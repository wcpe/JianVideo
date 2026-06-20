package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"jianvideo/internal/transcoder"
)

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
