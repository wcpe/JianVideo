// Package update 实现服务器端二进制自更新（FR-46）：检测 GitHub Releases、
// 按频道选版、下载校验 sha256、替换运行中二进制并重启、回滚。决策见 ADR-0032。
package update

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// 仓库与 API 默认值（公开仓库标识，非敏感）。
const (
	defaultBaseURL = "https://api.github.com"
	defaultOwner   = "wcpe"
	defaultRepo    = "JianVideo"
)

// Channel 更新频道。
type Channel string

const (
	ChannelStable     Channel = "stable"     // 仅正式 Release
	ChannelPrerelease Channel = "prerelease" // 含滚动 dev 预发布
)

// includesPrerelease 该频道是否纳入预发布。
func (c Channel) includesPrerelease() bool { return c == ChannelPrerelease }

// normalizeChannel 归一非法频道值为 stable。
func normalizeChannel(s string) Channel {
	if Channel(s) == ChannelPrerelease {
		return ChannelPrerelease
	}
	return ChannelStable
}

// CheckResult 更新检测结果，直接序列化给前端。
type CheckResult struct {
	Current    string  `json:"current"`
	Latest     string  `json:"latest"`
	HasUpdate  bool    `json:"has_update"`
	Tag        string  `json:"tag"`
	Prerelease bool    `json:"prerelease"`
	Channel    Channel `json:"channel"`
	Notes      string  `json:"notes"`
	AssetName  string  `json:"asset_name"`
}

// Service 自更新服务。baseURL/owner/repo/client 可注入，便于单测以 httptest 替换 GitHub。
type Service struct {
	baseURL string
	owner   string
	repo    string
	client  *http.Client
}

// NewService 创建指向公开仓库的自更新服务。
func NewService() *Service {
	return &Service{
		baseURL: defaultBaseURL,
		owner:   defaultOwner,
		repo:    defaultRepo,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// resolveTarget 拉取并按频道选出目标 Release 及当前平台的二进制资产、校验和资产。
// rel 为 nil 表示该频道无可用发布。
func (s *Service) resolveTarget(ctx context.Context, ch Channel) (rel *Release, bin, sums *Asset, err error) {
	rels, err := fetchReleases(ctx, s.client, s.baseURL, s.owner, s.repo)
	if err != nil {
		return nil, nil, nil, err
	}
	rel = selectRelease(rels, ch.includesPrerelease())
	if rel == nil {
		return nil, nil, nil, nil
	}
	return rel, selectBinaryAsset(rel.Assets, runtime.GOOS, runtime.GOARCH), findChecksumsAsset(rel.Assets), nil
}

// Check 按频道检测是否有可应用的更新。
func (s *Service) Check(ctx context.Context, current, channel string) (*CheckResult, error) {
	ch := normalizeChannel(channel)
	rel, bin, _, err := s.resolveTarget(ctx, ch)
	if err != nil {
		return nil, err
	}
	res := &CheckResult{Current: current, Channel: ch}
	if rel == nil {
		return res, nil
	}
	latest := releaseVersion(rel)
	res.Latest, res.Tag, res.Prerelease, res.Notes = latest, rel.TagName, rel.Prerelease, rel.Body
	if bin != nil {
		res.AssetName = bin.Name
	}
	// 有更新需同时满足：按频道判定有更新 且 该平台有可下载产物
	res.HasUpdate = bin != nil && hasUpdate(latest, current, ch)
	return res, nil
}

// hasUpdate 判断目标版本相对当前版本是否应提示更新。
// 正式版（stable）：按语义版本比较，仅更高才提示。
// 测试版（prerelease，dev 滚动）：只要最新预发布版本与当前不同即提示——
// 既支持从正式版切到测试版，也支持 dev→更新的 dev；同一 dev 则不提示。
func hasUpdate(latest, current string, ch Channel) bool {
	latest = strings.TrimSpace(latest)
	if latest == "" {
		return false
	}
	if ch == ChannelPrerelease {
		return latest != strings.TrimSpace(current)
	}
	return isNewer(latest, current)
}

// Apply 下载目标频道最新版、校验 sha256 后替换二进制并重启。
// 校验失败或缺产物即拒绝替换并返回错误；成功后当前进程将在短延时后退出（由新进程接管）。
func (s *Service) Apply(ctx context.Context, current, channel string) error {
	ch := normalizeChannel(channel)
	rel, bin, sums, err := s.resolveTarget(ctx, ch)
	if err != nil {
		return err
	}
	if rel == nil {
		return fmt.Errorf("该频道无可用发布")
	}
	if !hasUpdate(releaseVersion(rel), current, ch) {
		return fmt.Errorf("已是最新版本")
	}
	if bin == nil {
		return fmt.Errorf("发布缺少当前平台（%s/%s）产物", runtime.GOOS, runtime.GOARCH)
	}
	if sums == nil {
		return fmt.Errorf("发布缺少校验和清单")
	}

	dir, err := os.MkdirTemp("", "jianvideo-update-")
	if err != nil {
		return err
	}
	tmpBin, err := downloadToTemp(ctx, s.client, bin.URL, dir, bin.Name)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	sumsText, err := downloadText(ctx, s.client, sums.URL)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	ok, err := verifyChecksum(tmpBin, bin.Name, sumsText)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	if !ok {
		os.RemoveAll(dir)
		return fmt.Errorf("校验和不匹配，拒绝替换")
	}
	// 替换成功将触发进程重启；临时二进制已移走，残余临时目录由系统回收。
	return replaceAndRestart(tmpBin)
}

// Rollback 回滚到上一版（.old 备份）并重启。
func (s *Service) Rollback() error {
	return rollback()
}
