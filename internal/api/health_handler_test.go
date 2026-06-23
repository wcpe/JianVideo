package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

// setupHealthRouter 构造注入了健康巡检服务的测试路由。
func setupHealthRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}, &models.MediaHealthIssue{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	svc := library.NewService(gdb)
	healthSvc := library.NewDefaultHealthService(gdb)
	h := NewHandler(svc).WithHealthService(healthSvc)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, gdb
}

// TestHealthScanFlow 触发巡检 → 轮询状态至 completed → 问题清单含 0 字节问题。
// 用 0 字节媒体作样本（纯按 file_size 判定，不依赖真实 ffprobe / 文件系统），保证测试确定性。
func TestHealthScanFlow(t *testing.T) {
	r, gdb := setupHealthRouter(t)
	gdb.Create(&models.MediaFile{ID: 1, LibraryID: 1, FilePath: "D:/v/zero.mp4", FileName: "zero.mp4", FileSize: 0})

	// 触发巡检
	req := httptest.NewRequest("POST", "/api/library/health/scan", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("触发巡检期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var startResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &startResp)
	if startResp["status"] != "scanning" {
		t.Fatalf("触发响应 status 期望 scanning, 实际 %v", startResp["status"])
	}

	// 轮询状态至 completed
	deadline := time.Now().Add(2 * time.Second)
	var completed bool
	for time.Now().Before(deadline) {
		sw := httptest.NewRecorder()
		r.ServeHTTP(sw, httptest.NewRequest("GET", "/api/library/health/status", nil))
		var st map[string]any
		_ = json.Unmarshal(sw.Body.Bytes(), &st)
		if st["status"] == "completed" {
			completed = true
			if int(st["issue_count"].(float64)) != 1 {
				t.Fatalf("应发现 1 条问题, 实际 %v", st["issue_count"])
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !completed {
		t.Fatal("巡检最终应完成")
	}

	// 问题清单含该 0 字节媒体
	iw := httptest.NewRecorder()
	r.ServeHTTP(iw, httptest.NewRequest("GET", "/api/library/health/issues", nil))
	if iw.Code != http.StatusOK {
		t.Fatalf("问题清单期望 200, 实际 %d", iw.Code)
	}
	var issuesResp struct {
		Items []struct {
			MediaID   int64  `json:"media_id"`
			IssueType string `json:"issue_type"`
			FileName  string `json:"file_name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(iw.Body.Bytes(), &issuesResp); err != nil {
		t.Fatalf("解析问题清单失败: %v, body: %s", err, iw.Body.String())
	}
	if len(issuesResp.Items) != 1 {
		t.Fatalf("应有 1 条问题, 实际 %d", len(issuesResp.Items))
	}
	if issuesResp.Items[0].IssueType != models.HealthIssueZeroByte || issuesResp.Items[0].FileName != "zero.mp4" {
		t.Fatalf("问题项不符: %+v", issuesResp.Items[0])
	}
}

// TestHealthEndpointsUnavailable 未注入健康服务时端点返回 503。
func TestHealthEndpointsUnavailable(t *testing.T) {
	gdb, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	_ = gdb.AutoMigrate(&models.MediaFile{})
	h := NewHandler(library.NewService(gdb)) // 不注入 health
	r := gin.New()
	RegisterRoutes(r, h)

	for _, ep := range []struct {
		method, path string
	}{
		{"POST", "/api/library/health/scan"},
		{"GET", "/api/library/health/status"},
		{"GET", "/api/library/health/issues"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(ep.method, ep.path, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s 未注入服务应返回 503, 实际 %d", ep.method, ep.path, w.Code)
		}
	}
}
