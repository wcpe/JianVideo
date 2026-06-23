package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// TestUpdateSettings_AppliesFFmpegPathToRuntime 保存 ffmpeg_path / ffprobe_path 设置后，
// 应即时应用到 transcoder 运行期全局路径（GetFFmpegPath / GetFFprobePath 变更）。
// 复用 settings_handler_test.go 的 setupSettingsRouter。
func TestUpdateSettings_AppliesFFmpegPathToRuntime(t *testing.T) {
	// 保存并在结束后恢复全局路径，避免污染其他测试
	savedFFmpeg := transcoder.GetFFmpegPath()
	savedFFprobe := transcoder.GetFFprobePath()
	t.Cleanup(func() {
		transcoder.SetFFmpegPath(savedFFmpeg)
		transcoder.SetFFprobePath(savedFFprobe)
	})

	r := setupSettingsRouter(t)

	const newFFmpeg = "D:/custom/ffmpeg.exe"
	const newFFprobe = "D:/custom/ffprobe.exe"
	body := `{"settings":{"` + settings.KeyFFmpegPath + `":"` + newFFmpeg + `","` + settings.KeyFFprobePath + `":"` + newFFprobe + `"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("保存设置应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if got := transcoder.GetFFmpegPath(); got != newFFmpeg {
		t.Errorf("保存后 GetFFmpegPath 应为 %q，实际 %q", newFFmpeg, got)
	}
	if got := transcoder.GetFFprobePath(); got != newFFprobe {
		t.Errorf("保存后 GetFFprobePath 应为 %q，实际 %q", newFFprobe, got)
	}
}

// TestDetectFFmpeg_ResponseShape 检测端点响应应含 ffmpeg_available 与 ffmpeg_version 字段。
// 用不存在的路径，断言结构与 available=false（不依赖真机 ffmpeg）。
func TestDetectFFmpeg_ResponseShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"path":"jianvideo-nonexistent-ffmpeg-xyz"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/system/ffmpeg/detect", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.DetectFFmpeg(c)

	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", w.Code)
	}
	var resp struct {
		FFmpegAvailable bool   `json:"ffmpeg_available"`
		FFmpegVersion   string `json:"ffmpeg_version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp.FFmpegAvailable {
		t.Errorf("不存在的路径应返回 ffmpeg_available=false")
	}
	if resp.FFmpegVersion != "" {
		t.Errorf("不可用时 ffmpeg_version 应为空串，实际 %q", resp.FFmpegVersion)
	}
}
