package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// platAsset 当前平台的二进制资产名（与 selectBinaryAsset 匹配规则一致）。
func platAsset() string {
	return "jianvideo-" + runtime.GOOS + "-" + runtime.GOARCH
}

// mockService 起一个模拟 GitHub 的 httptest 服务并返回指向它的 Service。
// releases 按运行时 base 构造 release 列表（资产 URL 指向本服务）；binContent/sumsContent 为下载内容。
func mockService(t *testing.T, releases func(base string) []Release, binContent, sumsContent string) *Service {
	t.Helper()
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/wcpe/JianVideo/releases", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releases(base))
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(binContent)) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(sumsContent)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return &Service{baseURL: srv.URL, owner: "wcpe", repo: "JianVideo", client: srv.Client()}
}

// oneStableRelease 构造单个含当前平台产物 + 校验和的正式 release。
func oneStableRelease(tag string) func(base string) []Release {
	return func(base string) []Release {
		return []Release{{
			TagName:    tag,
			Prerelease: false,
			Body:       "release notes",
			Assets: []Asset{
				{Name: platAsset(), URL: base + "/dl/" + platAsset()},
				{Name: checksumsFileName, URL: base + "/sums"},
			},
		}}
	}
}

func TestCheck_StableHasUpdate(t *testing.T) {
	s := mockService(t, oneStableRelease("v9.9.9"), "", "")
	res, err := s.Check(context.Background(), "0.6.2", "stable")
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasUpdate || res.Latest != "v9.9.9" || res.AssetName != platAsset() {
		t.Errorf("应检出更新到 v9.9.9，得到 %+v", res)
	}
}

func TestCheck_NoUpdateWhenSameVersion(t *testing.T) {
	s := mockService(t, oneStableRelease("v0.6.2"), "", "")
	res, err := s.Check(context.Background(), "0.6.2", "stable")
	if err != nil {
		t.Fatal(err)
	}
	if res.HasUpdate {
		t.Errorf("同版本不应有更新，得到 %+v", res)
	}
}

func TestCheck_StableSkipsPrerelease(t *testing.T) {
	releases := func(base string) []Release {
		mk := func(tag string, pre bool) Release {
			return Release{TagName: tag, Prerelease: pre, Assets: []Asset{
				{Name: platAsset(), URL: base + "/dl/" + platAsset()},
				{Name: checksumsFileName, URL: base + "/sums"},
			}}
		}
		return []Release{mk("v9.9.9", true), mk("v0.6.1", false)} // 最新是预发布，正式版较旧
	}
	s := mockService(t, releases, "", "")

	// 稳定频道跳过预发布，选中较旧的 v0.6.1 → 无更新
	if res, _ := s.Check(context.Background(), "0.6.2", "stable"); res.HasUpdate || res.Latest != "v0.6.1" {
		t.Errorf("稳定频道应选 v0.6.1 且无更新，得到 %+v", res)
	}
	// 预发布频道取最新 v9.9.9 → 有更新
	if res, _ := s.Check(context.Background(), "0.6.2", "prerelease"); !res.HasUpdate || res.Latest != "v9.9.9" {
		t.Errorf("预发布频道应选 v9.9.9 且有更新，得到 %+v", res)
	}
}

func TestApply_RejectsChecksumMismatch(t *testing.T) {
	// 校验和清单给出错误摘要 → Apply 下载校验后应拒绝替换（不触及二进制替换）
	s := mockService(t, oneStableRelease("v9.9.9"), "BINARY", "deadbeef  "+platAsset()+"\n")
	err := s.Apply(context.Background(), "0.6.2", "stable")
	if err == nil || !strings.Contains(err.Error(), "校验和不匹配") {
		t.Errorf("应因校验和不匹配拒绝替换，得到 err=%v", err)
	}
}

func TestApply_RejectsWhenNotNewer(t *testing.T) {
	s := mockService(t, oneStableRelease("v0.6.1"), "BINARY", "")
	err := s.Apply(context.Background(), "0.6.2", "stable")
	if err == nil || !strings.Contains(err.Error(), "已是最新版本") {
		t.Errorf("非更新版本应拒绝，得到 err=%v", err)
	}
}

// devReleaseList 构造 CI 风格列表：正式版 v0.7.0 在前、滚动 dev 预发布在后
// （模拟 GitHub 把正式版排在预发布之前——用户实际遇到的顺序），dev 名内嵌完整版本。
func devReleaseList(stableTag, devEmbedded string) func(base string) []Release {
	return func(b string) []Release {
		mk := func(tag string, pre bool, name string) Release {
			return Release{TagName: tag, Prerelease: pre, Name: name, Assets: []Asset{
				{Name: platAsset(), URL: b + "/dl/" + platAsset()},
				{Name: checksumsFileName, URL: b + "/sums"},
			}}
		}
		return []Release{
			mk(stableTag, false, stableTag),
			mk("dev", true, "开发预览（dev · "+devEmbedded+"）"),
		}
	}
}

// TestCheck_PrereleaseSelectsDevNotNewestStable 回归核心 bug：
// 即便正式版排在列表最前，测试版频道也要选「最新预发布」（dev）而非最新整体。
func TestCheck_PrereleaseSelectsDevNotNewestStable(t *testing.T) {
	s := mockService(t, devReleaseList("v0.7.0", "0.7.0-dev.abc1234"), "", "")

	// 测试版：选 dev，latest 取内嵌版本，相对正式版 0.7.0 视为有更新（可切换）
	res, err := s.Check(context.Background(), "0.7.0", "prerelease")
	if err != nil {
		t.Fatal(err)
	}
	if res.Latest != "0.7.0-dev.abc1234" || !res.Prerelease || !res.HasUpdate {
		t.Fatalf("测试版应选 dev 内嵌版本且有更新，得到 %+v", res)
	}
	// 正式版：仍选 v0.7.0，当前即 0.7.0 → 无更新
	st, _ := s.Check(context.Background(), "0.7.0", "stable")
	if st.Latest != "v0.7.0" || st.HasUpdate {
		t.Errorf("正式版应选 v0.7.0 且无更新，得到 %+v", st)
	}
}

// TestCheck_PrereleaseSameDevNoUpdate 已在最新 dev 时测试版不应再提示更新。
func TestCheck_PrereleaseSameDevNoUpdate(t *testing.T) {
	s := mockService(t, devReleaseList("v0.7.0", "0.7.0-dev.abc1234"), "", "")
	res, _ := s.Check(context.Background(), "0.7.0-dev.abc1234", "prerelease")
	if res.HasUpdate {
		t.Errorf("已在最新 dev 不应提示更新，得到 %+v", res)
	}
}

// TestReleaseVersion 校验：语义 tag 原样用；非语义 tag（dev）从名提取内嵌版本。
func TestReleaseVersion(t *testing.T) {
	if v := releaseVersion(&Release{TagName: "v0.7.0"}); v != "v0.7.0" {
		t.Errorf("语义 tag 应原样返回，得到 %q", v)
	}
	if v := releaseVersion(&Release{TagName: "dev", Name: "开发预览（dev · 0.7.0-dev.abc1234）"}); v != "0.7.0-dev.abc1234" {
		t.Errorf("dev 应从名提取内嵌版本，得到 %q", v)
	}
}
