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
	"gorm.io/gorm/logger"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/metrics"
)

// setupMetricsRouter 构造注入了指标采样器的测试路由（含内存库与 metric_samples 表）。
func setupMetricsRouter(t *testing.T) (*gin.Engine, *gorm.DB, *metrics.Sampler) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.MetricSample{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	sampler := metrics.NewSampler(gdb, t.TempDir(), func() int { return 0 })
	h := NewHandler(nil).WithMetrics(sampler)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, gdb, sampler
}

// metricsResponse 端点响应的解析结构，仅取断言所需字段。
type metricsResponse struct {
	Range  string `json:"range"`
	Points []struct {
		T               string  `json:"t"`
		CPUPercent      float64 `json:"cpu_percent"`
		TranscodeActive int     `json:"transcode_active"`
	} `json:"points"`
	Current *struct {
		CPUPercent      float64 `json:"cpu_percent"`
		TranscodeActive int     `json:"transcode_active"`
	} `json:"current"`
}

// TestSystemMetrics_Unavailable 未注入采样器时端点返回 503。
func TestSystemMetrics_Unavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil) // 不注入 metrics
	r := gin.New()
	RegisterRoutes(r, h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/system/metrics", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未注入采样器期望 503，实际 %d", w.Code)
	}
}

// TestSystemMetrics_EmptyReturns200 刚启动无样本时返回 200，points 为空、current 为 null。
func TestSystemMetrics_EmptyReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, _, _ := setupMetricsRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/system/metrics?range=1h", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("空样本期望 200，实际 %d，body=%s", w.Code, w.Body.String())
	}

	var resp metricsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Range != "1h" {
		t.Errorf("range 期望 1h，实际 %q", resp.Range)
	}
	if len(resp.Points) != 0 {
		t.Errorf("空样本期望 points 为空，实际 %d", len(resp.Points))
	}
	if resp.Current != nil {
		t.Errorf("空样本期望 current 为 null，实际 %+v", resp.Current)
	}
}

// TestSystemMetrics_WithData 有样本时返回下采样 points 与 current 快照。
func TestSystemMetrics_WithData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, gdb, _ := setupMetricsRouter(t)

	now := time.Now().UTC()
	// 两个不同分钟的样本（1h 桶 60s → 2 桶）
	if err := gdb.Create(&models.MetricSample{SampledAt: now.Add(-5 * time.Minute), CPUPercent: 12, TranscodeActive: 1}).Error; err != nil {
		t.Fatalf("写入样本失败: %v", err)
	}
	if err := gdb.Create(&models.MetricSample{SampledAt: now.Add(-1 * time.Minute), CPUPercent: 88, TranscodeActive: 2}).Error; err != nil {
		t.Fatalf("写入样本失败: %v", err)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/system/metrics?range=1h", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body=%s", w.Code, w.Body.String())
	}

	var resp metricsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(resp.Points) != 2 {
		t.Fatalf("两不同分钟样本期望 2 点，实际 %d", len(resp.Points))
	}
	if resp.Current == nil {
		t.Fatalf("有样本时 current 不应为 null")
	}
	if resp.Current.CPUPercent != 88 {
		t.Errorf("current 应取最新样本 CPU=88，实际 %v", resp.Current.CPUPercent)
	}
	if resp.Current.TranscodeActive != 2 {
		t.Errorf("current 转码并发应为 2，实际 %d", resp.Current.TranscodeActive)
	}
}

// TestSystemMetrics_DefaultRange 缺省 range 归一化为 24h。
func TestSystemMetrics_DefaultRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, _, _ := setupMetricsRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/system/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	var resp metricsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Range != "24h" {
		t.Errorf("缺省 range 期望归一化为 24h，实际 %q", resp.Range)
	}
}
