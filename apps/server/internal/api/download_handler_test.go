package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestDownloadMediaFile_ServesOriginal 下载端点回传原始字节，带附件头与真实（中文）文件名。
func TestDownloadMediaFile_ServesOriginal(t *testing.T) {
	router, svc := setupTestRouter(t)

	dir := t.TempDir()
	name := "下载测试.mp4"
	content := []byte("原始视频字节内容")
	fp := filepath.Join(dir, name)
	if err := os.WriteFile(fp, content, 0o644); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	mf, err := svc.CreateMediaFile(1, fp, int64(len(content)))
	if err != nil {
		t.Fatalf("创建媒体记录失败: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/download", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Fatalf("应为附件下载, Content-Disposition=%q", cd)
	}
	// 中文文件名按 RFC 5987 百分号编码
	if !strings.Contains(cd, url.PathEscape(name)) {
		t.Fatalf("Content-Disposition 应含编码后的文件名, 实际 %q", cd)
	}
	if w.Body.String() != string(content) {
		t.Fatalf("下载内容应与原文件一致, 实际 %q", w.Body.String())
	}
}

// TestDownloadMediaFile_InvalidID 非法 ID 返回 400。
func TestDownloadMediaFile_InvalidID(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest("GET", "/api/library/media/abc/download", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

// TestDownloadMediaFile_NotFound 记录不存在返回 404。
func TestDownloadMediaFile_NotFound(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest("GET", "/api/library/media/99999/download", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

// TestDownloadMediaFile_SMBRejected smb:// 路径暂不支持下载，返回 400。
func TestDownloadMediaFile_SMBRejected(t *testing.T) {
	router, svc := setupTestRouter(t)
	mf, err := svc.CreateMediaFile(1, "smb://server/share/a.mp4", 100)
	if err != nil {
		t.Fatalf("创建媒体记录失败: %v", err)
	}
	req := httptest.NewRequest("GET", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/download", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("smb 路径期望 400, 实际 %d", w.Code)
	}
}

// TestDownloadMediaFile_FileMissing 磁盘文件缺失返回 404。
func TestDownloadMediaFile_FileMissing(t *testing.T) {
	router, svc := setupTestRouter(t)
	mf, err := svc.CreateMediaFile(1, filepath.Join(t.TempDir(), "不存在.jpg"), 100)
	if err != nil {
		t.Fatalf("创建媒体记录失败: %v", err)
	}
	req := httptest.NewRequest("GET", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/download", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("文件缺失期望 404, 实际 %d", w.Code)
	}
}
