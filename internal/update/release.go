package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Asset GitHub Release 的下载资产。
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release GitHub Release（仅取所需字段）。
type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Body       string  `json:"body"`
	Assets     []Asset `json:"assets"`
}

// fetchReleases 拉取仓库 releases 列表（GitHub 默认按发布时间最新在前）。
func fetchReleases(ctx context.Context, client *http.Client, baseURL, owner, repo string) ([]Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=20", baseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub releases 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub releases 返回 %d: %s", resp.StatusCode, string(body))
	}
	var rels []Release
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, fmt.Errorf("解析 GitHub releases 失败: %w", err)
	}
	return rels, nil
}

// selectRelease 按频道选目标 Release：始终跳过 draft；
// includePrerelease=true 取最新（含预发布），否则取最新正式版。无匹配返回 nil。
func selectRelease(rels []Release, includePrerelease bool) *Release {
	for i := range rels {
		if rels[i].Draft {
			continue
		}
		if includePrerelease || !rels[i].Prerelease {
			return &rels[i]
		}
	}
	return nil
}
