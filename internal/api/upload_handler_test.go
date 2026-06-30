package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
)

// waitFor 轮询等待条件成立，最多约 2 秒；超时则失败。用于断言队列异步执行的副作用。
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待条件超时")
}

// setupUploadRouter 构造带设置与扫描队列的上传测试路由，并返回库目录绝对路径与扫描调用计数。
// scanCalls 记录扫描 exec 被触发的次数，用于断言「上传后触发扫描」。
func setupUploadRouter(t *testing.T) (*gin.Engine, *gorm.DB, string, *int64) {
	t.Helper()
	// 每个测试用独立命名的内存库（按测试名做 DSN），避免 cache=shared 跨测试串数据
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_busy_timeout=5000"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(
		&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{},
		&models.Setting{}, &models.ScanTask{},
	); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	libDir := t.TempDir()
	if err := gdb.Create(&models.LibraryPath{
		Path: filepath.ToSlash(libDir), Type: "local", Enabled: 1,
	}).Error; err != nil {
		t.Fatalf("创建库目录失败: %v", err)
	}

	var scanCalls int64
	// 扫描 exec 替身：仅计数，不实际遍历，避免测试触盘扫描
	exec := func(libraryID int64, path, dirType, mode string) (int, error) {
		atomic.AddInt64(&scanCalls, 1)
		return 0, nil
	}
	queue := library.NewTaskQueue(gdb, exec)
	queue.Start()
	t.Cleanup(queue.Stop)

	libSvc := library.NewService(gdb)
	h := NewHandler(libSvc).WithSettings(settings.NewService(gdb)).WithScanQueue(queue)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, gdb, libDir, &scanCalls
}

// buildMultipart 构造单文件 multipart 请求体，附带可选表单字段。
func buildMultipart(t *testing.T, fileName, content string, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("创建表单文件失败: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("写表单文件失败: %v", err)
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("写表单字段失败: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart writer 失败: %v", err)
	}
	return body, w.FormDataContentType()
}

// TestUpload_OriginalLandsOnDiskAndTriggersScan 上传图片落盘到目标目录并触发扫描入队。
func TestUpload_OriginalLandsOnDiskAndTriggersScan(t *testing.T) {
	r, gdb, libDir, scanCalls := setupUploadRouter(t)

	body, ct := buildMultipart(t, "photo.jpg", "image-bytes", map[string]string{
		"target_dir":  filepath.ToSlash(libDir),
		"naming_rule": library.UploadNamingOriginal,
	})
	req := httptest.NewRequest("POST", "/api/library/upload", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}

	// 文件应落在库目录内
	landed := filepath.Join(libDir, "photo.jpg")
	if _, err := os.Stat(landed); err != nil {
		t.Fatalf("上传文件未落盘: %v", err)
	}

	// 应入队一条扫描任务（FR-149 触发增量扫描入库）
	var taskCount int64
	gdb.Model(&models.ScanTask{}).Count(&taskCount)
	if taskCount != 1 {
		t.Fatalf("期望入队 1 条扫描任务，实际 %d", taskCount)
	}
	// 等待 worker 真正执行扫描 exec（队列异步），最多等待若干轮
	waitFor(t, func() bool { return atomic.LoadInt64(scanCalls) >= 1 })
}

// TestUpload_DateRuleArchivesByYearMonth date 规则按 YYYY/MM 整齐归档。
func TestUpload_DateRuleArchivesByYearMonth(t *testing.T) {
	r, _, libDir, _ := setupUploadRouter(t)

	body, ct := buildMultipart(t, "clip.mp4", "video-bytes", map[string]string{
		"target_dir":  filepath.ToSlash(libDir),
		"naming_rule": library.UploadNamingDate,
	})
	req := httptest.NewRequest("POST", "/api/library/upload", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	// 落盘路径应含 年/月 子目录，且文件确实存在
	if _, err := os.Stat(filepath.FromSlash(resp.FilePath)); err != nil {
		t.Fatalf("归档文件未落盘: path=%s, err=%v", resp.FilePath, err)
	}
	rel, _ := filepath.Rel(libDir, filepath.FromSlash(resp.FilePath))
	// date 规则应生成 年/月 两级子目录，故相对路径目录段非空
	if filepath.Dir(rel) == "." {
		t.Fatalf("date 规则未生成年/月子目录: %s", rel)
	}
}

// TestUpload_RejectsTargetOutsideLibrary 目标目录在库外被拒绝（防越权写库外）。
func TestUpload_RejectsTargetOutsideLibrary(t *testing.T) {
	r, _, _, _ := setupUploadRouter(t)
	outside := filepath.ToSlash(t.TempDir()) // 另一个未注册目录

	body, ct := buildMultipart(t, "photo.jpg", "x", map[string]string{"target_dir": outside})
	req := httptest.NewRequest("POST", "/api/library/upload", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("库外目标应 400，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

// TestUpload_RejectsUnsupportedType 非图片/视频被拒绝。
func TestUpload_RejectsUnsupportedType(t *testing.T) {
	r, _, libDir, _ := setupUploadRouter(t)

	body, ct := buildMultipart(t, "notes.txt", "plain", map[string]string{"target_dir": filepath.ToSlash(libDir)})
	req := httptest.NewRequest("POST", "/api/library/upload", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("非媒体类型应 400，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

// TestUpload_UsesDefaultTargetFromSettings 未传 target_dir 时回退设置默认上传目录。
func TestUpload_UsesDefaultTargetFromSettings(t *testing.T) {
	r, gdb, libDir, _ := setupUploadRouter(t)
	// 写入默认上传目录设置
	if err := settings.NewService(gdb).Set(settings.KeyUploadTargetDir, filepath.ToSlash(libDir)); err != nil {
		t.Fatalf("写默认上传目录失败: %v", err)
	}

	body, ct := buildMultipart(t, "photo.png", "img", nil)
	req := httptest.NewRequest("POST", "/api/library/upload", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("回退默认目录应 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(libDir, "photo.png")); err != nil {
		t.Fatalf("文件未落到默认目录: %v", err)
	}
}

// TestUpload_NoTargetNoDefault 既不传 target_dir 也无默认配置时返回 400。
func TestUpload_NoTargetNoDefault(t *testing.T) {
	r, _, _, _ := setupUploadRouter(t)

	body, ct := buildMultipart(t, "photo.png", "img", nil)
	req := httptest.NewRequest("POST", "/api/library/upload", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("无目标无默认应 400，实际 %d，body: %s", w.Code, w.Body.String())
	}
}
