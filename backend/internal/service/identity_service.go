package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// 预编译正则表达式（避免每次调用重新编译）
var (
	// 匹配 user_id 格式: user_{64位hex}_account__session_{uuid}
	userIDRegex = regexp.MustCompile(`^user_[a-f0-9]{64}_account__session_([a-f0-9-]{36})$`)
	// 匹配 Claude Code User-Agent 格式: claude-cli/x.y.z
	claudeCodeUARegex = regexp.MustCompile(`^claude-cli/(\d+)\.(\d+)\.(\d+)`)
)

// 默认指纹值（Claude Code 客户端特征）
var defaultFingerprint = Fingerprint{
	UserAgent:               "claude-cli/2.0.62 (external, cli)",
	StainlessLang:           "js",
	StainlessPackageVersion: "0.52.0",
	StainlessOS:             "Linux",
	StainlessArch:           "x64",
	StainlessRuntime:        "node",
	StainlessRuntimeVersion: "v22.14.0",
}

// Fingerprint represents account fingerprint data
type Fingerprint struct {
	ClientID                string
	UserAgent               string
	StainlessLang           string
	StainlessPackageVersion string
	StainlessOS             string
	StainlessArch           string
	StainlessRuntime        string
	StainlessRuntimeVersion string
}

// IdentityCache defines cache operations for identity service
type IdentityCache interface {
	GetFingerprint(ctx context.Context, accountID int64) (*Fingerprint, error)
	SetFingerprint(ctx context.Context, accountID int64, fp *Fingerprint) error
}

// IdentityService 管理OAuth账号的请求身份指纹
type IdentityService struct {
	cache IdentityCache
}

// NewIdentityService 创建新的IdentityService
func NewIdentityService(cache IdentityCache) *IdentityService {
	return &IdentityService{cache: cache}
}

// GetOrCreateFingerprint 获取或创建账号的指纹
// 策略：
// 1. 如果客户端是真正的 Claude Code（User-Agent 匹配 claude-cli/x.y.z），用它更新缓存
// 2. 如果不是（如 SillyTavern），使用缓存的 Claude Code User-Agent
// 3. 这样真正的 Claude Code 客户端可以自动升级版本，其他客户端也能正常工作
func (s *IdentityService) GetOrCreateFingerprint(ctx context.Context, accountID int64, headers http.Header) (*Fingerprint, error) {
	clientUA := headers.Get("User-Agent")
	isRealClaudeCode := isClaudeCodeUserAgent(clientUA)

	// 尝试从缓存获取指纹
	cached, err := s.cache.GetFingerprint(ctx, accountID)
	if err == nil && cached != nil {
		if isRealClaudeCode {
			// 真正的 Claude Code 客户端：检查是否需要更新版本
			if isNewerClaudeCodeVersion(clientUA, cached.UserAgent) {
				cached.UserAgent = clientUA
				_ = s.cache.SetFingerprint(ctx, accountID, cached)
				log.Printf("Updated fingerprint User-Agent for account %d: %s", accountID, clientUA)
			}
		} else {
			// 非 Claude Code 客户端：确保使用 Claude Code User-Agent
			// 如果缓存的不是 Claude Code 格式，强制使用默认值
			if !isClaudeCodeUserAgent(cached.UserAgent) {
				cached.UserAgent = defaultFingerprint.UserAgent
				_ = s.cache.SetFingerprint(ctx, accountID, cached)
				log.Printf("Fixed fingerprint User-Agent to default for account %d", accountID)
			}
		}
		return cached, nil
	}

	// 缓存不存在或解析失败，创建新指纹
	fp := &Fingerprint{
		StainlessLang:           defaultFingerprint.StainlessLang,
		StainlessPackageVersion: defaultFingerprint.StainlessPackageVersion,
		StainlessOS:             defaultFingerprint.StainlessOS,
		StainlessArch:           defaultFingerprint.StainlessArch,
		StainlessRuntime:        defaultFingerprint.StainlessRuntime,
		StainlessRuntimeVersion: defaultFingerprint.StainlessRuntimeVersion,
	}

	// 如果是真正的 Claude Code 客户端，使用它的 User-Agent；否则使用默认值
	if isRealClaudeCode {
		fp.UserAgent = clientUA
	} else {
		fp.UserAgent = defaultFingerprint.UserAgent
	}

	// 生成随机ClientID
	fp.ClientID = generateClientID()

	// 保存到缓存（永不过期）
	if err := s.cache.SetFingerprint(ctx, accountID, fp); err != nil {
		log.Printf("Warning: failed to cache fingerprint for account %d: %v", accountID, err)
	}

	log.Printf("Created new fingerprint for account %d with client_id: %s, user_agent: %s", accountID, fp.ClientID, fp.UserAgent)
	return fp, nil
}

// ApplyFingerprint 将指纹应用到请求头（覆盖原有的x-stainless-*头）
func (s *IdentityService) ApplyFingerprint(req *http.Request, fp *Fingerprint) {
	if fp == nil {
		return
	}

	// 设置user-agent
	if fp.UserAgent != "" {
		req.Header.Set("user-agent", fp.UserAgent)
	}

	// 设置x-stainless-*头
	if fp.StainlessLang != "" {
		req.Header.Set("X-Stainless-Lang", fp.StainlessLang)
	}
	if fp.StainlessPackageVersion != "" {
		req.Header.Set("X-Stainless-Package-Version", fp.StainlessPackageVersion)
	}
	if fp.StainlessOS != "" {
		req.Header.Set("X-Stainless-OS", fp.StainlessOS)
	}
	if fp.StainlessArch != "" {
		req.Header.Set("X-Stainless-Arch", fp.StainlessArch)
	}
	if fp.StainlessRuntime != "" {
		req.Header.Set("X-Stainless-Runtime", fp.StainlessRuntime)
	}
	if fp.StainlessRuntimeVersion != "" {
		req.Header.Set("X-Stainless-Runtime-Version", fp.StainlessRuntimeVersion)
	}

	// Claude Code 客户端必需的额外头
	req.Header.Set("X-Stainless-Retry-Count", "0")
	req.Header.Set("X-App", "cli")
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
}

// RewriteUserID 重写body中的metadata.user_id
// 输入格式：user_{clientId}_account__session_{sessionUUID}
// 输出格式：user_{cachedClientID}_account_{accountUUID}_session_{newHash}
func (s *IdentityService) RewriteUserID(body []byte, accountID int64, accountUUID, cachedClientID string) ([]byte, error) {
	if len(body) == 0 || accountUUID == "" || cachedClientID == "" {
		return body, nil
	}

	// 解析JSON
	var reqMap map[string]any
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return body, nil
	}

	metadata, ok := reqMap["metadata"].(map[string]any)
	if !ok {
		return body, nil
	}

	userID, ok := metadata["user_id"].(string)
	if !ok || userID == "" {
		return body, nil
	}

	// 匹配格式: user_{64位hex}_account__session_{uuid}
	matches := userIDRegex.FindStringSubmatch(userID)
	if matches == nil {
		return body, nil
	}

	sessionTail := matches[1] // 原始session UUID

	// 生成新的session hash: SHA256(accountID::sessionTail) -> UUID格式
	seed := fmt.Sprintf("%d::%s", accountID, sessionTail)
	newSessionHash := generateUUIDFromSeed(seed)

	// 构建新的user_id
	// 格式: user_{cachedClientID}_account_{account_uuid}_session_{newSessionHash}
	newUserID := fmt.Sprintf("user_%s_account_%s_session_%s", cachedClientID, accountUUID, newSessionHash)

	metadata["user_id"] = newUserID
	reqMap["metadata"] = metadata

	return json.Marshal(reqMap)
}

// generateClientID 生成64位十六进制客户端ID（32字节随机数）
func generateClientID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// 极罕见的情况，使用时间戳+固定值作为fallback
		log.Printf("Warning: crypto/rand.Read failed: %v, using fallback", err)
		// 使用SHA256(当前纳秒时间)作为fallback
		h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(h[:])
	}
	return hex.EncodeToString(b)
}

// generateUUIDFromSeed 从种子生成确定性UUID v4格式字符串
func generateUUIDFromSeed(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	bytes := hash[:16]

	// 设置UUID v4版本和变体位
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

// isClaudeCodeUserAgent 检查User-Agent是否为Claude Code客户端格式
func isClaudeCodeUserAgent(ua string) bool {
	return claudeCodeUARegex.MatchString(ua)
}

// isNewerClaudeCodeVersion 比较两个Claude Code User-Agent版本
// 返回true如果newUA版本比oldUA更新
func isNewerClaudeCodeVersion(newUA, oldUA string) bool {
	newMatches := claudeCodeUARegex.FindStringSubmatch(newUA)
	oldMatches := claudeCodeUARegex.FindStringSubmatch(oldUA)

	if newMatches == nil || oldMatches == nil {
		return false
	}

	// 解析版本号 (major.minor.patch)
	newMajor, _ := strconv.Atoi(newMatches[1])
	newMinor, _ := strconv.Atoi(newMatches[2])
	newPatch, _ := strconv.Atoi(newMatches[3])

	oldMajor, _ := strconv.Atoi(oldMatches[1])
	oldMinor, _ := strconv.Atoi(oldMatches[2])
	oldPatch, _ := strconv.Atoi(oldMatches[3])

	// 比较版本号
	if newMajor != oldMajor {
		return newMajor > oldMajor
	}
	if newMinor != oldMinor {
		return newMinor > oldMinor
	}
	return newPatch > oldPatch
}
