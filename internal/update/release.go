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

// selectRelease 按频道选目标 Release（跳过 draft）：
// wantPrerelease=false（正式版）选最新正式版（!prerelease）；
// wantPrerelease=true（测试版）选最新预发布（prerelease==true，即 CI 的滚动 dev）。
// 注意：不能取「最新整体」——GitHub 列表里正式版可能排在预发布之前，会让测试版误取正式版。
func selectRelease(rels []Release, wantPrerelease bool) *Release {
	for i := range rels {
		if rels[i].Draft {
			continue
		}
		if rels[i].Prerelease == wantPrerelease {
			return &rels[i]
		}
	}
	return nil
}

// releaseVersion 返回用于比较/展示的版本号：tag 本身是语义版本则用 tag；
// 否则（如滚动预发布的 "dev" tag）从 release 名提取内嵌语义版本（CI 在名中写了完整版本，
// 如「开发预览（dev · 0.7.0-dev.abc1234）」）。
func releaseVersion(rel *Release) string {
	if parseVersion(rel.TagName).ok {
		return rel.TagName
	}
	if v := extractSemverish(rel.Name); v != "" {
		return v
	}
	return rel.TagName
}
