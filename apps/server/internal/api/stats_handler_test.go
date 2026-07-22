package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wcpe/JianVideo/internal/library"
)

// TestWatchStats_API 验证统计端点返回各聚合维度。
func TestWatchStats_API(t *testing.T) {
	router, svc := setupWatchRouter(t)
	idA := seedMedia(t, svc, 1, "a.mp4")
	idB := seedMedia(t, svc, 1, "b.mp4")
	seedMedia(t, svc, 1, "c.mp4") // 未看

	if _, err := svc.MarkWatched(idA); err != nil {
		t.Fatalf("标记 A 失败: %v", err)
	}
	if _, err := svc.MarkWatched(idB); err != nil {
		t.Fatalf("标记 B 失败: %v", err)
	}

	w := doJSON(t, router, "GET", "/api/library/stats", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp library.WatchStats
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body: %s", err, w.Body.String())
	}
	if resp.Total != 3 {
		t.Fatalf("期望总数 3, 实际 %d", resp.Total)
	}
	if resp.Watched != 2 {
		t.Fatalf("期望已看 2, 实际 %d", resp.Watched)
	}
	if resp.Unwatched != 1 {
		t.Fatalf("期望未看 1, 实际 %d", resp.Unwatched)
	}
	if len(resp.PositionHeatmap) != 10 {
		t.Fatalf("期望热力 10 档, 实际 %d", len(resp.PositionHeatmap))
	}
}
