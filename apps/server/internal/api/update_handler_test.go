package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/library"
)

// TestUpdateProgress_IdleByDefault 校验进度端点（FR-90）在未发生更新时返回 idle、percent=0，
// 且 JSON 含 state/downloaded/total/percent 四字段，端点经路由可达。
func TestUpdateProgress_IdleByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gdb := setupTestDB(t)
	h := NewHandler(library.NewService(gdb))

	r := gin.New()
	r.GET("/api/system/update/progress", h.UpdateProgress)

	req := httptest.NewRequest(http.MethodGet, "/api/system/update/progress", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("进度端点应返回 200，实际 %d", w.Code)
	}
	var body struct {
		State      string `json:"state"`
		Downloaded int64  `json:"downloaded"`
		Total      int64  `json:"total"`
		Percent    int    `json:"percent"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应非合法 JSON: %v，原文 %s", err, w.Body.String())
	}
	if body.State != "idle" {
		t.Errorf("初始进度状态应为 idle，实际 %q", body.State)
	}
	if body.Percent != 0 || body.Downloaded != 0 || body.Total != 0 {
		t.Errorf("初始进度应全 0，实际 downloaded=%d total=%d percent=%d", body.Downloaded, body.Total, body.Percent)
	}
}
