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
	"sync"
	"time"

	"github.com/wcpe/JianVideo/internal/netproxy"
)

// 仓库与 API 默认值（公开仓库标识，非敏感）。
const (
	defaultBaseURL = "https://api.github.com"
	defaultOwner   = "wcpe"
	defaultRepo    = "JianVideo"
)

// cacheTTL 更新检测结果的缓存有效期：TTL 内重复检测直接走缓存，不再请求 GitHub，
// 缓解国内直连 GitHub 慢导致的前端超时。
const cacheTTL = 10 * time.Minute

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

// cachedCheck 一条按频道缓存的检测结果及其写入时间。
type cachedCheck struct {
	res *CheckResult
	at  time.Time
}

// Service 自更新服务。baseURL/owner/repo/client 可注入，便于单测以 httptest 替换 GitHub。
// mu/cache 实现按频道的 TTL 缓存，并发安全。
type Service struct {
	baseURL string
	owner   string
	repo    string
	client  *http.Client
	// downloadClient 专用于下载二进制产物：不设整体 Timeout，靠调用方 context 控制超时，
	// 避免慢网络下几十 MB 的产物被 client 的 30s 整体超时掐断（检测仍走 client 的 30s）。
	downloadClient *http.Client

	mu    sync.Mutex
	cache map[Channel]cachedCheck
}

// NewService 创建指向公开仓库的自更新服务。
// 两个 client 的 Transport 均接 netproxy.ProxyFunc（FR-80），使检测与下载都经可热更的出站代理：
// 代理为空时直连，与原行为一致；各自 Timeout 语义保持不变（检测 30s、下载无整体超时靠 context）。
func NewService() *Service {
	return &Service{
		baseURL: defaultBaseURL,
		owner:   defaultOwner,
		repo:    defaultRepo,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{Proxy: netproxy.ProxyFunc},
		},
		downloadClient: &http.Client{ // 无整体超时：下载大产物靠 context（handler 给 5min）控制
			Transport: &http.Transport{Proxy: netproxy.ProxyFunc},
		},
		cache: map[Channel]cachedCheck{},
	}
}

// dlClient 返回下载专用 client；未注入（如测试以字面量构造 Service）时回退到 client。
func (s *Service) dlClient() *http.Client {
	if s.downloadClient != nil {
		return s.downloadClient
	}
	return s.client
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
// force=false 时优先命中 TTL 缓存；缓存未过期直接返回副本，不请求 GitHub。
// fetch 出错时优雅降级：非 force 且存在历史缓存（即便已过期）则返回该缓存（nil err），
// 仅在无缓存或 force 时才把错误上抛。
func (s *Service) Check(ctx context.Context, current, channel string, force bool) (*CheckResult, error) {
	ch := normalizeChannel(channel)

	if !force {
		if res, ok := s.cachedFresh(ch); ok {
			return res, nil
		}
	}

	res, err := s.fetchCheck(ctx, current, ch)
	if err != nil {
		// 降级：非 force 且有历史缓存（含过期）→ 用上次结果，避免一次网络抖动就让用户看到失败
		if !force {
			if stale, ok := s.cachedAny(ch); ok {
				return stale, nil
			}
		}
		return nil, err
	}

	s.storeCache(ch, res)
	return res, nil
}

// fetchCheck 实际请求 GitHub 并组装检测结果（不读写缓存）。
func (s *Service) fetchCheck(ctx context.Context, current string, ch Channel) (*CheckResult, error) {
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

// cachedFresh 返回未过期的缓存副本（命中且在 TTL 内时 ok=true）。
func (s *Service) cachedFresh(ch Channel) (*CheckResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cache[ch]
	if !ok || time.Since(c.at) >= cacheTTL {
		return nil, false
	}
	return copyResult(c.res), true
}

// cachedAny 返回任意历史缓存副本（不论是否过期，命中即 ok=true），供降级使用。
func (s *Service) cachedAny(ch Channel) (*CheckResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cache[ch]
	if !ok {
		return nil, false
	}
	return copyResult(c.res), true
}

// storeCache 写入/刷新某频道的缓存。
func (s *Service) storeCache(ch Channel, res *CheckResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[ch] = cachedCheck{res: copyResult(res), at: time.Now()}
}

// copyResult 返回 CheckResult 的浅拷贝；CheckResult 全为值字段，浅拷贝即深拷贝，
// 用于让缓存对外只暴露副本，外部改动不污染缓存。
func copyResult(r *CheckResult) *CheckResult {
	if r == nil {
		return nil
	}
	c := *r
	return &c
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
	tmpBin, err := downloadToTemp(ctx, s.dlClient(), bin.URL, dir, bin.Name)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	sumsText, err := downloadText(ctx, s.dlClient(), sums.URL)
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
