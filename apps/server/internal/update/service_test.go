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
	mux.HandleFunc("/repos/wcpe/JianVideo/releases", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(releases(base))
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(binContent)) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(sumsContent)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return &Service{baseURL: srv.URL, owner: "wcpe", repo: "JianVideo", client: srv.Client(), cache: map[Channel]cachedCheck{}}
}

// TestCheck_PrereleaseFindsRCOnSecondPage 验证候选版会遍历分页全集，并按 GitHub 参数请求。
func TestCheck_PrereleaseFindsRCOnSecondPage(t *testing.T) {
	var requests []string
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/wcpe/JianVideo/releases", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		requests = append(requests, query.Get("per_page")+"/"+query.Get("page"))
		if query.Get("page") == "2" {
			_ = json.NewEncoder(w).Encode([]Release{{
				TagName:    "v9.9.9-rc.2",
				Prerelease: true,
				Assets:     completeRCAssets(base),
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(make([]Release, 100))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	s := &Service{baseURL: base, owner: "wcpe", repo: "JianVideo", client: srv.Client(), cache: map[Channel]cachedCheck{}}

	res, err := s.Check(context.Background(), "0.6.2", "prerelease", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Latest != "v9.9.9-rc.2" || !res.HasUpdate {
		t.Fatalf("应选择第二页的合法候选版，得到 %+v", res)
	}
	if len(requests) != 2 || requests[0] != "100/1" || requests[1] != "100/2" {
		t.Fatalf("分页请求参数错误，得到 %v", requests)
	}
}

// TestFetchReleases_ShortPageStopsWhenHandlerIgnoresPage 验证短页响应不会因处理器忽略页码而重复请求。
func TestFetchReleases_ShortPageStopsWhenHandlerIgnoresPage(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode([]Release{{TagName: "v1.0.0"}})
	}))
	t.Cleanup(srv.Close)

	rels, err := fetchReleases(context.Background(), srv.Client(), srv.URL, "wcpe", "JianVideo")
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || requests != 1 {
		t.Fatalf("短页应只请求一次，发布数=%d，请求数=%d", len(rels), requests)
	}
}

// TestFetchReleases_FailsClosedAtPageLimit 验证连续满页达到上限时拒绝返回部分结果。
func TestFetchReleases_FailsClosedAtPageLimit(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(make([]Release, 100))
	}))
	t.Cleanup(srv.Close)

	rels, err := fetchReleases(context.Background(), srv.Client(), srv.URL, "wcpe", "JianVideo")
	if err == nil || !strings.Contains(err.Error(), "分页上限") || !strings.Contains(err.Error(), "不完整结果") {
		t.Fatalf("达到分页上限应返回中文错误，得到 %v", err)
	}
	if rels != nil {
		t.Fatalf("达到分页上限不应返回部分结果，得到 %d 项", len(rels))
	}
	if requests != 10 {
		t.Fatalf("应在第 10 个满页后停止，实际请求 %d 次", requests)
	}
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
	res, err := s.Check(context.Background(), "0.6.2", "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasUpdate || res.Latest != "v9.9.9" || res.AssetName != platAsset() {
		t.Errorf("应检出更新到 v9.9.9，得到 %+v", res)
	}
}

func TestCheck_NoUpdateWhenSameVersion(t *testing.T) {
	s := mockService(t, oneStableRelease("v0.6.2"), "", "")
	res, err := s.Check(context.Background(), "0.6.2", "stable", false)
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
			return Release{TagName: tag, Prerelease: pre, Assets: completeRCAssets(base)}
		}
		return []Release{mk("v9.9.9-rc.1", true), mk("v0.6.1", false)} // 最新是候选版，正式版较旧
	}
	s := mockService(t, releases, "", "")

	// 稳定频道跳过预发布，选中较旧的 v0.6.1 → 无更新
	if res, _ := s.Check(context.Background(), "0.6.2", "stable", false); res.HasUpdate || res.Latest != "v0.6.1" {
		t.Errorf("稳定频道应选 v0.6.1 且无更新，得到 %+v", res)
	}
	// 候选版频道取 v9.9.9-rc.1 → 有更新
	if res, _ := s.Check(context.Background(), "0.6.2", "prerelease", false); !res.HasUpdate || res.Latest != "v9.9.9-rc.1" {
		t.Errorf("候选版频道应选 v9.9.9-rc.1 且有更新，得到 %+v", res)
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

// completeRCAssets 构造候选发布契约要求的三项非空资产。
func completeRCAssets(base string) []Asset {
	return []Asset{
		{Name: "jianvideo-linux-amd64", URL: base + "/dl/jianvideo-linux-amd64", Size: 1},
		{Name: "jianvideo-windows-amd64.exe", URL: base + "/dl/jianvideo-windows-amd64.exe", Size: 1},
		{Name: checksumsFileName, URL: base + "/sums", Size: 1},
	}
}

// rcReleaseList 构造包含正式版、旧 dev、非法候选版与多个合法 RC 的发布列表。
func rcReleaseList(base string) []Release {
	mk := func(tag string, prerelease, draft bool) Release {
		return Release{TagName: tag, Prerelease: prerelease, Draft: draft, Assets: completeRCAssets(base)}
	}
	return []Release{
		mk("dev", true, false),
		mk("0.19.0-rc.1", true, false),
		mk("v0.19.0-rc.01", true, false),
		mk("v0.18.0-rc.x", true, false),
		mk("v0.18.0-rc.20", true, true),
		mk("v0.17.1-rc.9", true, false),
		mk("v0.17.1-rc.10", true, false),
		mk("v0.17.1", false, false),
	}
}

// TestSelectRelease_PrereleaseOnlyHighestRC 验证候选版频道忽略旧 dev/非法/draft，且按数字选择最高 RC。
func TestSelectRelease_PrereleaseOnlyHighestRC(t *testing.T) {
	rel := selectRelease(rcReleaseList(""), true)
	if rel == nil || rel.TagName != "v0.17.1-rc.10" {
		t.Fatalf("候选版频道应选择 v0.17.1-rc.10，得到 %+v", rel)
	}
}

// TestSelectRelease_PrereleaseFallsBackFromIncompleteRC 验证高版本候选发布资产不完整时回退。
func TestSelectRelease_PrereleaseFallsBackFromIncompleteRC(t *testing.T) {
	zeroSizeAssets := completeRCAssets("")
	zeroSizeAssets[1].Size = 0
	emptyURLAssets := completeRCAssets("")
	emptyURLAssets[0].URL = " "
	rels := []Release{
		{TagName: "v0.19.0-rc.4", Prerelease: true, Assets: completeRCAssets("")[:2]},
		{TagName: "v0.19.0-rc.3", Prerelease: true, Assets: zeroSizeAssets},
		{TagName: "v0.19.0-rc.2", Prerelease: true, Assets: emptyURLAssets},
		{TagName: "v0.19.0-rc.1", Prerelease: true, Assets: completeRCAssets("")},
	}
	rel := selectRelease(rels, true)
	if rel == nil || rel.TagName != "v0.19.0-rc.1" {
		t.Fatalf("应跳过资产不完整的高版本 RC 并回退到 v0.19.0-rc.1，得到 %+v", rel)
	}
}

// TestSelectRelease_StableKeepsHistoricalAssetCompatibility 确保正式频道不新增候选发布资产门槛。
func TestSelectRelease_StableKeepsHistoricalAssetCompatibility(t *testing.T) {
	rels := []Release{{
		TagName:    "v0.18.0",
		Prerelease: false,
		Assets:     []Asset{{Name: platAsset(), URL: "/dl/" + platAsset()}},
	}}
	if rel := selectRelease(rels, false); rel == nil || rel.TagName != "v0.18.0" {
		t.Fatalf("正式频道应保持历史选择兼容，得到 %+v", rel)
	}
}

// TestSelectRelease_PrereleaseBaselineBeforeRC 验证候选版先比较版本基线，再比较 RC 数字。
func TestSelectRelease_PrereleaseBaselineBeforeRC(t *testing.T) {
	rels := []Release{
		{TagName: "v0.17.1-rc.99", Prerelease: true, Assets: completeRCAssets("")},
		{TagName: "v0.18.0-rc.1", Prerelease: true, Assets: completeRCAssets("")},
	}
	rel := selectRelease(rels, true)
	if rel == nil || rel.TagName != "v0.18.0-rc.1" {
		t.Fatalf("更高基线应优先于更大的低基线 RC，得到 %+v", rel)
	}
}

// TestCheck_PrereleaseRCMigration 覆盖旧 dev 迁移 RC、低 RC 与同基线正式版不降级。
func TestCheck_PrereleaseRCMigration(t *testing.T) {
	s := mockService(t, rcReleaseList, "", "")

	cases := []struct {
		name    string
		current string
		want    bool
	}{
		{"旧开发版升级同基线候选版", "0.17.1-dev.3.gabc1234", true},
		{"较低候选版升级", "0.17.1-rc.9", true},
		{"相同候选版不更新", "0.17.1-rc.10", false},
		{"较高候选版不降级", "0.17.1-rc.11", false},
		{"同基线正式版不降级", "0.17.1", false},
	}
	for _, tc := range cases {
		res, err := s.Check(context.Background(), tc.current, "prerelease", true)
		if err != nil {
			t.Fatal(err)
		}
		if res.Latest != "v0.17.1-rc.10" || res.HasUpdate != tc.want {
			t.Errorf("%s: 得到 %+v", tc.name, res)
		}
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
