package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type githubReleaseClient struct {
	proxyRepo service.ProxyRepository
}

func NewGitHubReleaseClient(proxyRepo service.ProxyRepository) service.GitHubReleaseClient {
	return &githubReleaseClient{
		proxyRepo: proxyRepo,
	}
}

// findUpdateProxy 查找名称包含"更新"的代理
func (c *githubReleaseClient) findUpdateProxy(ctx context.Context) *service.Proxy {
	proxies, err := c.proxyRepo.ListActive(ctx)
	if err != nil {
		return nil
	}

	for i := range proxies {
		if strings.Contains(proxies[i].Name, "更新") {
			return &proxies[i]
		}
	}
	return nil
}

// getHTTPClient 获取 HTTP 客户端，优先使用更新代理
func (c *githubReleaseClient) getHTTPClient(ctx context.Context, timeout time.Duration) *http.Client {
	opts := httpclient.Options{
		Timeout: timeout,
	}

	// 查找更新代理
	if proxy := c.findUpdateProxy(ctx); proxy != nil {
		opts.ProxyURL = proxy.URL()
		log.Printf("[UpdateService] Using proxy '%s' for update check", proxy.Name)
	}

	client, err := httpclient.GetClient(opts)
	if err != nil {
		log.Printf("[UpdateService] Failed to create HTTP client with proxy: %v, falling back to direct connection", err)
		client, _ = httpclient.GetClient(httpclient.Options{Timeout: timeout})
	}
	return client
}

func (c *githubReleaseClient) FetchLatestRelease(ctx context.Context, repo string) (*service.GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Sub2API-Updater")

	httpClient := c.getHTTPClient(ctx, 30*time.Second)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release service.GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func (c *githubReleaseClient) DownloadFile(ctx context.Context, url, dest string, maxSize int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	downloadClient := c.getHTTPClient(ctx, 10*time.Minute)
	resp, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// SECURITY: Check Content-Length if available
	if resp.ContentLength > maxSize {
		return fmt.Errorf("file too large: %d bytes (max %d)", resp.ContentLength, maxSize)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	// SECURITY: Use LimitReader to enforce max download size even if Content-Length is missing/wrong
	limited := io.LimitReader(resp.Body, maxSize+1)
	written, err := io.Copy(out, limited)
	if err != nil {
		return err
	}

	// Check if we hit the limit (downloaded more than maxSize)
	if written > maxSize {
		_ = os.Remove(dest) // Clean up partial file (best-effort)
		return fmt.Errorf("download exceeded maximum size of %d bytes", maxSize)
	}

	return nil
}

func (c *githubReleaseClient) FetchChecksumFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	httpClient := c.getHTTPClient(ctx, 30*time.Second)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
