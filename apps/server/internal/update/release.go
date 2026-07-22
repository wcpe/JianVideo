package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

const (
	releasesPerPage = 100
	maxReleasePages = 10
)

// fetchReleases 按发布时间倒序分页拉取仓库发布列表，短页表示结束。
func fetchReleases(ctx context.Context, client *http.Client, baseURL, owner, repo string) ([]Release, error) {
	all := make([]Release, 0, releasesPerPage)
	for page := 1; page <= maxReleasePages; page++ {
		rels, err := fetchReleasePage(ctx, client, baseURL, owner, repo, page)
		if err != nil {
			return nil, err
		}
		all = append(all, rels...)
		if len(rels) < releasesPerPage {
			return all, nil
		}
	}
	return nil, fmt.Errorf("GitHub 发布列表已连续返回 %d 个满页，达到分页上限，拒绝使用不完整结果", maxReleasePages)
}

// fetchReleasePage 拉取指定页的 GitHub 发布列表。
func fetchReleasePage(ctx context.Context, client *http.Client, baseURL, owner, repo string, page int) ([]Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d&page=%d", baseURL, owner, repo, releasesPerPage, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 GitHub 发布列表请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub 发布列表失败: %w", err)
	}
	// 资源清理，关闭错误可忽略
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub 发布列表返回状态码 %d: %s", resp.StatusCode, string(body))
	}
	var rels []Release
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, fmt.Errorf("解析 GitHub 发布列表失败: %w", err)
	}
	return rels, nil
}

var prereleaseRequiredAssets = [...]string{
	"jianvideo-linux-amd64",
	"jianvideo-windows-amd64.exe",
	checksumsFileName,
}

// hasCompletePrereleaseAssets 检查候选发布是否包含新发布契约要求的三项非空资产。
func hasCompletePrereleaseAssets(assets []Asset) bool {
	for _, required := range prereleaseRequiredAssets {
		found := false
		for i := range assets {
			asset := assets[i]
			if asset.Name == required && asset.Size > 0 && strings.TrimSpace(asset.URL) != "" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// selectRelease 按频道选目标 Release（跳过 draft）。
// 正式频道保持选择列表中的最新正式版；候选版频道仅接受合法 vX.Y.Z-rc.N，
// 忽略旧 dev、其他预发布与资产不完整项，并按版本基线、RC 数字选最高版本。
func selectRelease(rels []Release, wantPrerelease bool) *Release {
	if !wantPrerelease {
		for i := range rels {
			if !rels[i].Draft && !rels[i].Prerelease {
				return &rels[i]
			}
		}
		return nil
	}

	var selected *Release
	var selectedVersion parsedVersion
	selectedRC := 0
	for i := range rels {
		rel := &rels[i]
		version, rc, ok := parseRCTag(rel.TagName)
		if rel.Draft || !rel.Prerelease || !ok || !hasCompletePrereleaseAssets(rel.Assets) {
			continue
		}
		if selected == nil || baselineCmp(version, selectedVersion) > 0 ||
			(baselineCmp(version, selectedVersion) == 0 && rc > selectedRC) {
			selected, selectedVersion, selectedRC = rel, version, rc
		}
	}
	return selected
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
