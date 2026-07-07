package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// countingService 起一个会统计 releases 请求次数、并可切换为返回错误的 httptest 服务，
// 返回指向它的 Service 与一个取调用计数的函数。用于验证 TTL 缓存命中 / 降级行为。
func countingService(t *testing.T, tag string) (*Service, func() int64, *atomic.Bool) {
	t.Helper()
	var calls atomic.Int64
	var fail atomic.Bool
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/wcpe/JianVideo/releases", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if fail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		rels := []Release{{
			TagName:    tag,
			Prerelease: false,
			Body:       "release notes",
			Assets: []Asset{
				{Name: platAsset(), URL: base + "/dl/" + platAsset()},
				{Name: checksumsFileName, URL: base + "/sums"},
			},
		}}
		_ = json.NewEncoder(w).Encode(rels)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	s := &Service{baseURL: srv.URL, owner: "wcpe", repo: "JianVideo", client: srv.Client(), cache: map[Channel]cachedCheck{}}
	return s, calls.Load, &fail
}

// TestCheck_CacheHitWithinTTL TTL 内第二次 Check 命中缓存，不再请求 GitHub。
func TestCheck_CacheHitWithinTTL(t *testing.T) {
	s, calls, _ := countingService(t, "v9.9.9")

	if _, err := s.Check(context.Background(), "0.6.2", "stable", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(context.Background(), "0.6.2", "stable", false); err != nil {
		t.Fatal(err)
	}
	if got := calls(); got != 1 {
		t.Errorf("TTL 内应只请求一次 GitHub，实际 %d 次", got)
	}
}

// TestCheck_ForceSkipsCache force=true 跳过缓存，强制重新请求。
func TestCheck_ForceSkipsCache(t *testing.T) {
	s, calls, _ := countingService(t, "v9.9.9")

	if _, err := s.Check(context.Background(), "0.6.2", "stable", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(context.Background(), "0.6.2", "stable", true); err != nil {
		t.Fatal(err)
	}
	if got := calls(); got != 2 {
		t.Errorf("force 应跳过缓存再请求一次，实际 %d 次", got)
	}
}

// TestCheck_ExpiredCacheRefetches 缓存过期后重新请求。
// 用「构造一条已过期缓存条目」的确定性手段，不依赖真实时钟睡眠。
func TestCheck_ExpiredCacheRefetches(t *testing.T) {
	s, calls, _ := countingService(t, "v9.9.9")

	// 首次填充缓存
	if _, err := s.Check(context.Background(), "0.6.2", "stable", false); err != nil {
		t.Fatal(err)
	}
	// 手动把缓存时间戳改成已过期（早于 now-cacheTTL）
	s.mu.Lock()
	entry := s.cache[ChannelStable]
	entry.at = time.Now().Add(-cacheTTL - time.Minute)
	s.cache[ChannelStable] = entry
	s.mu.Unlock()

	if _, err := s.Check(context.Background(), "0.6.2", "stable", false); err != nil {
		t.Fatal(err)
	}
	if got := calls(); got != 2 {
		t.Errorf("缓存过期应重新请求，实际 %d 次", got)
	}
}

// TestCheck_DegradeToStaleCacheOnError fetch 出错且有缓存（即便过期）时返回缓存且不报错。
func TestCheck_DegradeToStaleCacheOnError(t *testing.T) {
	s, calls, fail := countingService(t, "v9.9.9")

	// 首次成功，填充缓存
	first, err := s.Check(context.Background(), "0.6.2", "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	// 让缓存过期，并令后续 fetch 失败
	s.mu.Lock()
	entry := s.cache[ChannelStable]
	entry.at = time.Now().Add(-cacheTTL - time.Minute)
	s.cache[ChannelStable] = entry
	s.mu.Unlock()
	fail.Store(true)

	res, err := s.Check(context.Background(), "0.6.2", "stable", false)
	if err != nil {
		t.Fatalf("有缓存时 fetch 出错应降级返回缓存且 nil err，得到 err=%v", err)
	}
	if res == nil || res.Latest != first.Latest {
		t.Errorf("降级应返回上次缓存结果，得到 %+v", res)
	}
	if got := calls(); got != 2 {
		t.Errorf("降级路径应尝试请求一次（失败），实际请求 %d 次", got)
	}
}

// TestCheck_ErrorWithoutCache 无缓存且 fetch 出错时返回错误。
func TestCheck_ErrorWithoutCache(t *testing.T) {
	s, _, fail := countingService(t, "v9.9.9")
	fail.Store(true)

	if _, err := s.Check(context.Background(), "0.6.2", "stable", false); err == nil {
		t.Error("无缓存且 fetch 出错应返回错误，得到 nil")
	}
}

// TestCheck_ForceErrorWithCacheStillFails force=true 时即便有缓存，fetch 出错也不降级（强制要新鲜结果）。
func TestCheck_ForceErrorWithCacheStillFails(t *testing.T) {
	s, _, fail := countingService(t, "v9.9.9")

	if _, err := s.Check(context.Background(), "0.6.2", "stable", false); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if _, err := s.Check(context.Background(), "0.6.2", "stable", true); err == nil {
		t.Error("force 出错不应降级缓存，应返回错误，得到 nil")
	}
}

// TestCheck_CacheReturnsCopy 命中缓存返回副本，外部改动不污染缓存。
func TestCheck_CacheReturnsCopy(t *testing.T) {
	s, _, _ := countingService(t, "v9.9.9")

	first, err := s.Check(context.Background(), "0.6.2", "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	first.Latest = "被外部篡改"

	second, err := s.Check(context.Background(), "0.6.2", "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Latest == "被外部篡改" {
		t.Error("缓存应返回副本，外部改动不应污染后续结果")
	}
}
