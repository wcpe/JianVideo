package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
)

// insertTestMediaFile 向数据库插入一条测试媒体文件记录。
func insertTestMediaFile(db *gorm.DB, libraryID int, filePath string) (int, error) {
	result := db.Exec(
		`INSERT INTO media_files (library_id, file_path, file_name, file_size, format, duration, width, height, subtitle_tracks, added_at, modified_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		libraryID, filePath, filepath.Base(filePath), 1024, "mkv", 120.0, 1920, 1080, "",
	)
	if result.Error != nil {
		return 0, result.Error
	}
	var id int
	db.Raw("SELECT last_insert_rowid()").Scan(&id)
	return id, nil
}

// setupSubtitleTestRouter 创建带字幕路由的测试路由器，并在临时目录创建测试字幕文件。
func setupSubtitleTestRouter(t *testing.T) (*gin.Engine, *playback.Service, int) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 创建临时目录和测试字幕文件
	tmpDir := t.TempDir()
	srtContent := "1\n00:00:01,000 --> 00:00:02,000\n测试字幕\n"
	assContent := "[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,ASS测试\n"

	_ = os.WriteFile(filepath.Join(tmpDir, "test video.srt"), []byte(srtContent), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "test video.ass"), []byte(assContent), 0o644)
	// 创建假视频文件（用于 FindSubtitleFiles 查找同目录字幕）
	_ = os.WriteFile(filepath.Join(tmpDir, "test video.mkv"), []byte("fake video"), 0o644)

	// 创建数据库和库路径
	db := setupTestDB(t)
	libSvc := library.NewService(db)

	// 插入媒体库目录
	libPath, err := libSvc.CreateLibraryPath(tmpDir, "local", "测试目录")
	if err != nil {
		t.Fatalf("创建测试目录失败: %v", err)
	}

	// 插入媒体文件记录
	mediaID, err := insertTestMediaFile(db, int(libPath.ID), filepath.Join(tmpDir, "test video.mkv"))
	if err != nil {
		t.Fatalf("插入媒体文件失败: %v", err)
	}

	pbSvc := playback.NewService()
	h := NewHandler(libSvc)

	r := gin.New()
	RegisterRoutes(r, h, pbSvc)
	return r, pbSvc, mediaID
}

// TestGetSubtitles_ReturnsTracks 测试获取字幕轨道列表。
func TestGetSubtitles_ReturnsTracks(t *testing.T) {
	router, _, mediaID := setupSubtitleTestRouter(t)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/play/%d/subtitles", mediaID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// 应包含 srt 和 ass 两个字幕轨道
	if !strContains(body, "test video.srt") {
		t.Errorf("应包含 srt 字幕文件, body: %s", body)
	}
	if !strContains(body, "test video.ass") {
		t.Errorf("应包含 ass 字幕文件, body: %s", body)
	}
	if !strContains(body, `"format":"srt"`) {
		t.Errorf("应包含 srt 格式, body: %s", body)
	}
	if !strContains(body, `"format":"ass"`) {
		t.Errorf("应包含 ass 格式, body: %s", body)
	}
}

// TestGetSubtitles_InvalidID 测试无效的媒体 ID。
func TestGetSubtitles_InvalidID(t *testing.T) {
	router, _, _ := setupSubtitleTestRouter(t)

	req := httptest.NewRequest("GET", "/api/play/invalid/subtitles", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

// TestGetSubtitles_NotFound 测试不存在的媒体文件。
func TestGetSubtitles_NotFound(t *testing.T) {
	router, _, _ := setupSubtitleTestRouter(t)

	req := httptest.NewRequest("GET", "/api/play/99999/subtitles", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

// TestGetSubtitleContent_ReturnsVTT 测试获取字幕 WebVTT 内容。
func TestGetSubtitleContent_ReturnsVTT(t *testing.T) {
	router, _, mediaID := setupSubtitleTestRouter(t)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/play/%d/subtitles/0", mediaID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// 应以 WEBVTT 开头
	if !strStartsWith(body, "WEBVTT") {
		t.Errorf("应以 WEBVTT 开头, body: %s", body[:minInt(len(body), 50)])
	}
	// 应包含字幕文本（srt 或 ass 转换后的文本）
	if !strContains(body, "测试字幕") && !strContains(body, "ASS测试") {
		t.Errorf("应包含字幕文本, body: %s", body)
	}
}

// TestGetSubtitleContent_InvalidIndex 测试无效的字幕索引。
func TestGetSubtitleContent_InvalidIndex(t *testing.T) {
	router, _, mediaID := setupSubtitleTestRouter(t)

	// 索引 99 超出范围
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/play/%d/subtitles/99", mediaID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

// TestGetSubtitleContent_NegativeIndex 测试负数索引。
func TestGetSubtitleContent_NegativeIndex(t *testing.T) {
	router, _, mediaID := setupSubtitleTestRouter(t)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/play/%d/subtitles/-1", mediaID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

// TestGetSubtitles_NoSubtitles 测试无字幕时返回空列表。
func TestGetSubtitles_NoSubtitles(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 创建临时目录但不创建字幕文件
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "nosubtitle.mkv"), []byte("fake"), 0o644)

	db := setupTestDB(t)
	libSvc := library.NewService(db)

	libPath, err := libSvc.CreateLibraryPath(tmpDir, "local", "无字幕目录")
	if err != nil {
		t.Fatalf("创建测试目录失败: %v", err)
	}

	mediaID, err := insertTestMediaFile(db, int(libPath.ID), filepath.Join(tmpDir, "nosubtitle.mkv"))
	if err != nil {
		t.Fatalf("插入媒体文件失败: %v", err)
	}

	pbSvc := playback.NewService()
	h := NewHandler(libSvc)

	r := gin.New()
	RegisterRoutes(r, h, pbSvc)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/play/%d/subtitles", mediaID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// 应返回空列表
	if !strContains(body, `"tracks":[]`) {
		t.Errorf("应返回空字幕列表, body: %s", body)
	}
}

// 辅助函数

func strContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func strStartsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
