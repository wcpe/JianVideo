package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *library.Service) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	svc := library.NewService(gdb)
	h := NewHandler(svc)

	r := gin.New()
	RegisterRoutes(r, h)
	return r, svc
}

func TestCreateLibraryPath_API(t *testing.T) {
	router, _ := setupTestRouter(t)
	dir := filepath.ToSlash(t.TempDir())

	body := `{"path":"` + dir + `","type":"local","label":"测试"}`
	req := httptest.NewRequest("POST", "/api/library/paths", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("期望 201, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["path"] == nil {
		t.Fatal("响应缺少 path 字段")
	}
}

func TestListLibraryPaths_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	_, _ = svc.CreateLibraryPath(t.TempDir(), "local", "目录1")

	req := httptest.NewRequest("GET", "/api/library/paths", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, ok := resp["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("期望 1 条记录, 实际响应: %s", w.Body.String())
	}
}

func TestListLibraryPaths_API_IncludesMediaCount(t *testing.T) {
	router, svc := setupTestRouter(t)
	lp, _ := svc.CreateLibraryPath(t.TempDir(), "local", "库")
	_, _ = svc.CreateMediaFile(lp.ID, "/tmp/x/a.mp4", 1)
	_, _ = svc.CreateMediaFile(lp.ID, "/tmp/x/b.mp4", 1)

	req := httptest.NewRequest("GET", "/api/library/paths", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var resp struct {
		Items []struct {
			ID         int64 `json:"id"`
			MediaCount int64 `json:"media_count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body: %s", err, w.Body.String())
	}
	if len(resp.Items) != 1 {
		t.Fatalf("期望 1 条记录, 实际响应: %s", w.Body.String())
	}
	if resp.Items[0].MediaCount != 2 {
		t.Fatalf("期望 media_count=2, 实际 %d", resp.Items[0].MediaCount)
	}
}

func TestDeleteLibraryPath_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	lp, _ := svc.CreateLibraryPath(t.TempDir(), "local", "")

	req := httptest.NewRequest("DELETE", "/api/library/paths/"+strconv.FormatInt(lp.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("期望 204, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

func TestCreateMediaFileAndList_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "")
	if err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	_, _ = svc.CreateMediaFile(lp.ID, filepath.Join(dir, "video.mp4"), 1024)

	req := httptest.NewRequest("GET", "/api/library/media?library_id="+strconv.FormatInt(lp.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"].(float64) != 1 {
		t.Fatalf("期望 total=1, 实际 %v", resp["total"])
	}
}

func TestGetMediaFile_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	mf, _ := svc.CreateMediaFile(1, "/tmp/single.mp4", 2048)

	req := httptest.NewRequest("GET", "/api/library/media/"+strconv.FormatInt(mf.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["file_name"] != "single.mp4" {
		t.Fatalf("期望 file_name=single.mp4, 实际 %v", resp["file_name"])
	}
}

// TestGetMediaFile_SoftDeleted_API 软删项经详情端点应返回 404（FR-25 访问隔离）。
func TestGetMediaFile_SoftDeleted_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	mf, _ := svc.CreateMediaFile(1, "/tmp/soft_deleted.mp4", 2048)
	if err := svc.DeleteMediaFile(mf.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/library/media/"+strconv.FormatInt(mf.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("软删项详情应 404, 实际 %d", w.Code)
	}
}

func TestDeleteMediaFile_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	mf, _ := svc.CreateMediaFile(1, "/tmp/to_del.mp4", 512)

	req := httptest.NewRequest("DELETE", "/api/library/media/"+strconv.FormatInt(mf.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("期望 204, 实际 %d", w.Code)
	}
}

func TestListRecycleMediaFiles_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	keep, _ := svc.CreateMediaFile(1, "/tmp/keep.mp4", 512)
	del, _ := svc.CreateMediaFile(1, "/tmp/gone.mp4", 512)
	if err := svc.DeleteMediaFile(del.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/library/recycle", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []models.MediaFile `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0].ID != del.ID {
		t.Fatalf("回收站应仅含软删项 %d, 实得 %d 条", del.ID, len(resp.Items))
	}
	if resp.Items[0].ID == keep.ID {
		t.Fatal("未软删项不应出现在回收站")
	}
}

func TestRestoreMediaFile_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	mf, _ := svc.CreateMediaFile(1, "/tmp/restore.mp4", 512)
	if err := svc.DeleteMediaFile(mf.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/restore", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("期望 204, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	// 还原后应回到常规列表
	items, _, _ := svc.ListMediaFilesFiltered(library.MediaFilter{LibraryID: 1}, 1, 20)
	if len(items) != 1 || items[0].ID != mf.ID {
		t.Fatalf("还原后媒体应回到常规列表, 实得 %d 条", len(items))
	}
}

func TestRestoreMediaFile_NotFound_API(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/api/library/media/99999/restore", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

func TestScanLibrary_API(t *testing.T) {
	router, svc := setupTestRouter(t)

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "scan_test.mp4"), []byte("fake"), 0o644)

	lp, _ := svc.CreateLibraryPath(dir, "local", "扫描测试")

	req := httptest.NewRequest("POST", "/api/library/scan/"+strconv.FormatInt(lp.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "scanning" {
		t.Fatalf("期望 status=scanning, 实际 %v", resp["status"])
	}

	// 等待异步扫描完成
	time.Sleep(500 * time.Millisecond)
	status := library.GetScanStatus()
	if status.Status != "completed" {
		t.Fatalf("期望扫描完成后 status=completed, 实际 %s", status.Status)
	}
}

// ─── 错误路径测试 ──────────────────────────────────────

func TestCreateLibraryPath_InvalidJSON(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/api/library/paths", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestDeleteLibraryPath_InvalidID(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("DELETE", "/api/library/paths/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestGetMediaFile_InvalidID(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/library/media/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestGetMediaFile_NotFound(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/library/media/99999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

func TestDeleteMediaFile_InvalidID(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("DELETE", "/api/library/media/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestScanLibrary_InvalidID(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/api/library/scan/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestCreateLibraryPath_InaccessibleLocalPathReturns400(t *testing.T) {
	router, _ := setupTestRouter(t)
	missing := filepath.ToSlash(filepath.Join(t.TempDir(), "missing"))
	body := `{"path":"` + missing + `","type":"local","label":"不可访问"}`

	req := httptest.NewRequest("POST", "/api/library/paths", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("不可访问路径期望 400, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

func TestGetRawImage_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "图片预览")
	if err != nil {
		t.Fatalf("创建媒体库目录失败: %v", err)
	}
	imagePath := filepath.Join(dir, "cover.png")
	if err := os.WriteFile(imagePath, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatalf("写入测试图片失败: %v", err)
	}
	mf, err := svc.CreateMediaFile(lp.ID, imagePath, 4)
	if err != nil {
		t.Fatalf("创建图片记录失败: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/raw", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("图片 raw 期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("Content-Type 期望 image/png, 实际 %s", w.Header().Get("Content-Type"))
	}
	if !bytes.Equal(w.Body.Bytes(), []byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatal("raw 响应内容不匹配")
	}
}

func TestGetRawImage_RejectsVideo(t *testing.T) {
	router, svc := setupTestRouter(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "视频")
	if err != nil {
		t.Fatalf("创建媒体库目录失败: %v", err)
	}
	mf, err := svc.CreateMediaFile(lp.ID, filepath.Join(dir, "movie.mp4"), 4)
	if err != nil {
		t.Fatalf("创建视频记录失败: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/raw", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("视频 raw 期望 400, 实际 %d", w.Code)
	}
}

func TestMediaExtensionsAPI(t *testing.T) {
	router, svc := setupTestRouter(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "自定义后缀")
	if err != nil {
		t.Fatalf("创建媒体库目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clip.foo"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("写入自定义媒体失败: %v", err)
	}

	postBody := `{"library_id":` + strconv.FormatInt(lp.ID, 10) + `,"extension":".foo","type":"video"}`
	req := httptest.NewRequest("POST", "/api/library/extensions", bytes.NewBufferString(postBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("添加后缀期望 201, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/library/extensions?library_id="+strconv.FormatInt(lp.ID, 10), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("查询后缀期望 200, 实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"extension":"foo"`) {
		t.Fatalf("后缀列表应包含 foo, body: %s", w.Body.String())
	}

	svc.ScanLibrary(lp.ID, dir)
	// 等待异步扫描完成
	var status library.ScanStatus
	for i := 0; i < 50; i++ {
		status = library.GetScanStatus()
		if status.Status == "completed" || status.Status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status.ScannedFiles != 1 {
		t.Fatalf("自定义后缀扫描期望 1, 实际 %d", status.ScannedFiles)
	}
}

func TestRenameMediaFile_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.mp4")
	if err := os.WriteFile(oldPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	mf, _ := svc.CreateMediaFile(1, oldPath, 4)

	// 正常重命名 → 200
	req := httptest.NewRequest("PUT", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/rename",
		bytes.NewBufferString(`{"new_name":"new.mkv"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["file_name"] != "new.mkv" {
		t.Fatalf("响应 file_name 期望 new.mkv, 实际 %v", resp["file_name"])
	}

	// 非法新名 → 400
	req = httptest.NewRequest("PUT", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/rename",
		bytes.NewBufferString(`{"new_name":"sub/x.mp4"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法新名期望 400, 实际 %d", w.Code)
	}

	// 不存在的记录 → 404
	req = httptest.NewRequest("PUT", "/api/library/media/9999/rename",
		bytes.NewBufferString(`{"new_name":"x.mp4"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在记录期望 404, 实际 %d", w.Code)
	}
}

func TestUpdateDisplayName_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	dir := t.TempDir()
	diskPath := filepath.Join(dir, "real.mp4")
	if err := os.WriteFile(diskPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	mf, _ := svc.CreateMediaFile(1, diskPath, 4)

	// 设置显示名 → 200，仅 display_name 变更，磁盘文件名不动
	req := httptest.NewRequest("PUT", "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/display-name",
		bytes.NewBufferString(`{"display_name":"我的影片"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["display_name"] != "我的影片" {
		t.Fatalf("响应 display_name 期望 我的影片, 实际 %v", resp["display_name"])
	}
	if resp["file_name"] != "real.mp4" {
		t.Fatalf("改显示名不应改动 file_name, 实际 %v", resp["file_name"])
	}
	// 磁盘文件应保持原名
	if _, err := os.Stat(diskPath); err != nil {
		t.Fatalf("磁盘文件应保持原名: %v", err)
	}

	// 不存在的记录 → 404
	req = httptest.NewRequest("PUT", "/api/library/media/9999/display-name",
		bytes.NewBufferString(`{"display_name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在记录期望 404, 实际 %d", w.Code)
	}
}

// 复现 Bug B：扫描完成后扫描状态恒为 completed，SSE 端点不应在 completed 后
// 立即关闭连接（否则浏览器 EventSource 会每 ~3s 重连成风暴）。连接应保持打开，
// 仅在客户端断开时关闭。
func TestScanProgressSSE_StaysOpenAfterCompleted(t *testing.T) {
	router, svc := setupTestRouter(t)

	// 触发一次扫描并等待完成，使全局扫描状态变为 completed
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}
	lp, err := svc.CreateLibraryPath(dir, "local", "t")
	if err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	svc.ScanLibrary(lp.ID, dir)
	for i := 0; i < 60; i++ {
		if s := library.GetScanStatus(); s.Status == "completed" || s.Status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if s := library.GetScanStatus(); s.Status != "completed" {
		t.Fatalf("扫描未完成，状态=%s", s.Status)
	}

	ts := httptest.NewServer(router)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/library/scan/progress", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("连接 SSE 失败: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	gotProgress := false
	var readErr error
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 && strings.Contains(string(buf[:n]), "progress") {
			gotProgress = true
		}
		if rerr != nil {
			readErr = rerr
			break
		}
	}
	if !gotProgress {
		t.Fatal("未收到 progress 事件")
	}
	// 修复前：completed 后服务端立即 return 关闭连接 → 客户端读到 io.EOF。
	// 修复后：连接保持打开，直到客户端 ctx 超时取消（非 EOF）。
	if errors.Is(readErr, io.EOF) {
		t.Fatal("SSE 在 completed 后被服务端提前关闭，应保持连接打开以接收后续扫描进度")
	}
}

// 复现 G1：硬件加速能力端点 GET /api/transcode/hwaccel 必须已挂载并返回 JSON。
// 修复前路由未注册 → 测试路由得 404；真实部署中会被前端 SPA catch-all 接住返回 HTML。
func TestHWAccelEndpoint_ReturnsJSON(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/transcode/hwaccel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d（路由未挂载会得 404）", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("期望 application/json，实际 Content-Type=%q", ct)
	}
	var info map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("响应非 JSON: %v, body=%s", err, w.Body.String())
	}
	for _, k := range []string{"available", "preferred", "software_fallback", "h264_supported", "h265_supported"} {
		if _, ok := info[k]; !ok {
			t.Fatalf("硬件能力响应缺字段 %q，body=%s", k, w.Body.String())
		}
	}
}
