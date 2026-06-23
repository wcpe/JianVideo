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
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// newCapabilityDB 创建内存库并迁移 CodecProbeCache，限单连接保证表可见。
func newCapabilityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.CodecProbeCache{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return db
}

func TestSystemInfo_ReturnsVersionAndFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil).WithVersion("9.9.9-test")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/system/info", nil)
	h.SystemInfo(c)

	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp["app_version"] != "9.9.9-test" {
		t.Errorf("app_version 应为注入值 9.9.9-test，实际 %v", resp["app_version"])
	}
	for _, k := range []string{"os", "arch", "num_cpu", "hostname", "go_version", "ffmpeg", "hwaccel"} {
		if _, ok := resp[k]; !ok {
			t.Errorf("响应缺少字段 %q", k)
		}
	}
}

// TestSystemInfo_RuntimeFields 验证运行环境补齐（FR-60）：响应含 runtime 对象，
// 其 pid/work_dir/executable/db_path/uptime_seconds/mem_alloc/mem_sys/num_gc/gomaxprocs 字段齐全且取值合理。
func TestSystemInfo_RuntimeFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 注入启动时刻（1 秒前）与数据库路径，断言运行时长与 db_path 经注入生效
	startedAt := time.Now().Add(-1 * time.Second)
	h := NewHandler(nil).WithStartTime(startedAt).WithDBPath("/data/test.db")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/system/info", nil)
	h.SystemInfo(c)

	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", w.Code)
	}
	var resp struct {
		Runtime struct {
			PID           int    `json:"pid"`
			WorkDir       string `json:"work_dir"`
			Executable    string `json:"executable"`
			DBPath        string `json:"db_path"`
			UptimeSeconds int64  `json:"uptime_seconds"`
			MemAlloc      uint64 `json:"mem_alloc"`
			MemSys        uint64 `json:"mem_sys"`
			NumGC         uint32 `json:"num_gc"`
			GOMAXPROCS    int    `json:"gomaxprocs"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp.Runtime.PID <= 0 {
		t.Errorf("pid 应为正整数，实际 %d", resp.Runtime.PID)
	}
	if resp.Runtime.DBPath != "/data/test.db" {
		t.Errorf("db_path 应为注入值 /data/test.db，实际 %q", resp.Runtime.DBPath)
	}
	if resp.Runtime.UptimeSeconds < 1 {
		t.Errorf("uptime_seconds 应 >=1（启动于 1 秒前），实际 %d", resp.Runtime.UptimeSeconds)
	}
	if resp.Runtime.GOMAXPROCS <= 0 {
		t.Errorf("gomaxprocs 应为正整数，实际 %d", resp.Runtime.GOMAXPROCS)
	}
	if resp.Runtime.MemSys == 0 {
		t.Errorf("mem_sys 应大于 0")
	}
	// work_dir 应非空（os.Getwd 正常返回）
	if resp.Runtime.WorkDir == "" {
		t.Errorf("work_dir 不应为空")
	}
}

// TestSystemInfo_RuntimeFields_NoInjection 未注入启动时刻/DB 路径时仍返回 runtime 对象，
// uptime_seconds 退化为 0、db_path 为空，不报错（保证 NewHandler(nil) 测试可用）。
func TestSystemInfo_RuntimeFields_NoInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/system/info", nil)
	h.SystemInfo(c)

	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	rt, ok := resp["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("响应应含 runtime 对象")
	}
	if v, _ := rt["uptime_seconds"].(float64); v != 0 {
		t.Errorf("未注入启动时刻时 uptime_seconds 应为 0，实际 %v", v)
	}
	if v, _ := rt["db_path"].(string); v != "" {
		t.Errorf("未注入 db_path 时应为空串，实际 %q", v)
	}
}

// TestCodecTest_FFmpegUnavailable 验证 ffmpeg 不可用时快速返回 ffmpeg_available:false，不跑试编码。
func TestCodecTest_FFmpegUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 临时把 ffmpeg 路径指向不存在的可执行文件，制造「不可用」，结束后恢复
	saved := transcoder.GetFFmpegPath()
	transcoder.SetFFmpegPath("jianvideo-nonexistent-ffmpeg-xyz")
	t.Cleanup(func() { transcoder.SetFFmpegPath(saved) })

	h := NewHandler(nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/system/codec-test", nil)
	h.CodecTest(c)

	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", w.Code)
	}
	var resp struct {
		FFmpegAvailable bool                            `json:"ffmpeg_available"`
		Results         []transcoder.EncoderProbeResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp.FFmpegAvailable {
		t.Errorf("ffmpeg 不可用时 ffmpeg_available 应为 false")
	}
	if len(resp.Results) != 0 {
		t.Errorf("ffmpeg 不可用时不应有试编码结果，实际 %d 条", len(resp.Results))
	}
}

// TestCodecTest_CacheHit_FromCacheTrue 注入能力服务且缓存命中时，CodecTest 返回 from_cache=true 且不重跑。
// 用哨兵编码器名证明结果来自缓存。
func TestCodecTest_CacheHit_FromCacheTrue(t *testing.T) {
	if !transcoder.IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过缓存命中端点测试")
	}
	gin.SetMode(gin.TestMode)

	db := newCapabilityDB(t)
	version := transcoder.FFmpegVersion(httptest.NewRequest(http.MethodGet, "/", nil).Context())
	raw, _ := json.Marshal([]transcoder.EncoderProbeResult{
		{Encoder: "sentinel-encoder", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
	})
	if err := db.Create(&models.CodecProbeCache{FFmpegVersion: version, Results: string(raw), TestedAt: time.Now()}).Error; err != nil {
		t.Fatalf("写入缓存失败: %v", err)
	}

	h := NewHandler(nil).WithCapabilityService(transcoder.NewCapabilityService(db))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/system/codec-test", nil)
	h.CodecTest(c)

	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", w.Code)
	}
	var resp struct {
		FFmpegAvailable bool                            `json:"ffmpeg_available"`
		Results         []transcoder.EncoderProbeResult `json:"results"`
		FromCache       bool                            `json:"from_cache"`
		FFmpegVersion   string                          `json:"ffmpeg_version"`
		TestedAt        string                          `json:"tested_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if !resp.FromCache {
		t.Errorf("命中缓存时 from_cache 应为 true")
	}
	if resp.FFmpegVersion != version {
		t.Errorf("ffmpeg_version 应为 %q，实际 %q", version, resp.FFmpegVersion)
	}
	if resp.TestedAt == "" {
		t.Errorf("命中缓存应有 tested_at")
	}
	if len(resp.Results) != 1 || resp.Results[0].Encoder != "sentinel-encoder" {
		t.Errorf("应返回缓存中的哨兵结果，证明未重跑，实际 %+v", resp.Results)
	}
}

// TestHWAccelAndSystemInfo_SameSource 验证 HWAccel 端点与 SystemInfo 的 hwaccel 同源（命中同一缓存）。
func TestHWAccelAndSystemInfo_SameSource(t *testing.T) {
	if !transcoder.IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过同源端点测试")
	}
	gin.SetMode(gin.TestMode)

	db := newCapabilityDB(t)
	version := transcoder.FFmpegVersion(httptest.NewRequest(http.MethodGet, "/", nil).Context())
	raw, _ := json.Marshal([]transcoder.EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", Compiled: true, TestedOK: true},
	})
	if err := db.Create(&models.CodecProbeCache{FFmpegVersion: version, Results: string(raw), TestedAt: time.Now()}).Error; err != nil {
		t.Fatalf("写入缓存失败: %v", err)
	}

	h := NewHandler(nil).WithCapabilityService(transcoder.NewCapabilityService(db))

	// HWAccel 端点
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodGet, "/api/transcode/hwaccel", nil)
	h.HWAccel(c1)
	var hw transcoder.HWAccelInfo
	if err := json.Unmarshal(w1.Body.Bytes(), &hw); err != nil {
		t.Fatalf("HWAccel 响应非 JSON: %v", err)
	}

	// SystemInfo 的 hwaccel
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/system/info", nil)
	h.SystemInfo(c2)
	var sys struct {
		HWAccel transcoder.HWAccelInfo `json:"hwaccel"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &sys); err != nil {
		t.Fatalf("SystemInfo 响应非 JSON: %v", err)
	}

	if hw.Preferred != "h264_amf" || sys.HWAccel.Preferred != "h264_amf" {
		t.Errorf("两端点 preferred 应同为 h264_amf，HWAccel=%q SystemInfo=%q", hw.Preferred, sys.HWAccel.Preferred)
	}
	if !hw.FromCache || !sys.HWAccel.FromCache {
		t.Errorf("两端点均应标记 from_cache=true")
	}
}
