package claude

import (
	"strings"
	"unicode"
)

// Claude Code 系统提示词常量
const ClaudeCodeSystemPrompt = "You are Claude Code, Anthropic's official CLI for Claude."

// 系统提示词模板列表（用于相似度匹配）
var systemPromptTemplates = []string{
	"You are Claude Code, Anthropic's official CLI for Claude.",
	"You are a Claude agent, built on Anthropic's Claude Agent SDK.",
	"You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.",
	"You are an interactive CLI tool that helps users",
	"You are a file search specialist for Claude Code, Anthropic's official CLI for Claude.",
	"You are an agent for Claude Code, Anthropic's official CLI for Claude.",
}

// 默认相似度阈值
const DefaultSystemPromptThreshold = 0.5

// normalizeText 规范化文本：去除多余空白，转小写
func normalizeText(s string) string {
	var builder strings.Builder
	lastSpace := true
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !lastSpace {
				_, _ = builder.WriteRune(' ')
				lastSpace = true
			}
		} else {
			_, _ = builder.WriteRune(unicode.ToLower(r))
			lastSpace = false
		}
	}
	return strings.TrimSpace(builder.String())
}

// stringSimilarity 计算两个字符串的相似度（Dice coefficient）
// 参考 claude-relay-service 使用的 string-similarity 库
func stringSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	if len(s1) < 2 || len(s2) < 2 {
		return 0.0
	}

	// 生成 bigrams
	bigrams1 := make(map[string]int)
	for i := 0; i < len(s1)-1; i++ {
		bigram := s1[i : i+2]
		bigrams1[bigram]++
	}

	bigrams2 := make(map[string]int)
	for i := 0; i < len(s2)-1; i++ {
		bigram := s2[i : i+2]
		bigrams2[bigram]++
	}

	// 计算交集
	intersection := 0
	for bigram, count1 := range bigrams1 {
		if count2, ok := bigrams2[bigram]; ok {
			if count1 < count2 {
				intersection += count1
			} else {
				intersection += count2
			}
		}
	}

	// Dice coefficient = 2 * |intersection| / (|s1 bigrams| + |s2 bigrams|)
	return float64(2*intersection) / float64(len(s1)-1+len(s2)-1)
}

// BestSimilarityByTemplates 计算文本与所有模板的最佳相似度
func BestSimilarityByTemplates(text string) (bestScore float64, matchedTemplate string) {
	normalizedText := normalizeText(text)
	bestScore = 0.0
	matchedTemplate = ""

	for _, template := range systemPromptTemplates {
		normalizedTemplate := normalizeText(template)
		score := stringSimilarity(normalizedText, normalizedTemplate)
		if score > bestScore {
			bestScore = score
			matchedTemplate = template
		}
	}

	return bestScore, matchedTemplate
}

// IsRealClaudeCodeRequest 判断请求是否是真实的 Claude Code 请求
// 通过检查 system 字段中是否包含 Claude Code 相关的系统提示词
func IsRealClaudeCodeRequest(system interface{}, threshold float64) bool {
	if threshold <= 0 {
		threshold = DefaultSystemPromptThreshold
	}

	if system == nil {
		return false
	}

	// 处理字符串格式
	if str, ok := system.(string); ok {
		score, _ := BestSimilarityByTemplates(str)
		return score >= threshold
	}

	// 处理数组格式
	if arr, ok := system.([]interface{}); ok {
		for _, item := range arr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if text, ok := itemMap["text"].(string); ok {
					score, _ := BestSimilarityByTemplates(text)
					if score >= threshold {
						return true
					}
				}
			}
		}
	}

	return false
}

// IncludesClaudeCodeSystemPrompt 检查 system 中是否存在 Claude Code 系统提示词
// 与 IsRealClaudeCodeRequest 类似，但只需要找到一个匹配即返回 true
func IncludesClaudeCodeSystemPrompt(system interface{}, threshold float64) bool {
	return IsRealClaudeCodeRequest(system, threshold)
}
