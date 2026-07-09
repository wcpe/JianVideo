package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func setupCacheRouter(t *testing.T) (*gin.Engine, *gorm.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "jianvideo.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("读取底层数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := gdb.AutoMigrate(
		&models.Space{},
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.CacheAsset{},
		&models.AuditEvent{},
		&models.Task{},
	); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	if err := gdb.Create(&models.Space{ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("创建 Space 失败: %v", err)
	}
	auditSvc := audit.NewRecorder(gdb)
	taskSvc := tasksvc.NewService(gdb)
	cacheSvc := storage.NewService(gdb, dataDir).WithAudit(auditSvc).WithTasks(taskSvc)
	h := NewHandler(library.NewService(gdb)).WithAudit(auditSvc).WithTasks(taskSvc).WithCache(cacheSvc)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, gdb, dataDir
}

func TestStorageCacheAPI_SummaryInventoryDryRunAndClean(t *testing.T) {
	r, db, dataDir := setupCacheRouter(t)
	thumb := filepath.Join(dataDir, "thumbnails", "a.jpg")
	hlsDir := filepath.Join(dataDir, "hls", "88")
	mustWriteAPIFile(t, thumb, "12345")
	mustWriteAPIFile(t, filepath.Join(hlsDir, "master.m3u8"), "m")
	mustWriteAPIFile(t, filepath.Join(hlsDir, "480p_segment_000.ts"), "segment")

	w := serveCacheRequest(r, http.MethodPost, "/api/storage/cache/inventory", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("盘点期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}

	w = serveCacheRequest(r, http.MethodGet, "/api/storage/cache/summary", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("统计期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var summary storage.Summary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("解析统计失败: %v", err)
	}
	if summary.ByKind[storage.CacheKindThumbnail].SizeBytes != 5 || summary.ByKind[storage.CacheKindHLS].FileCount != 2 {
		t.Fatalf("统计内容不符: %+v", summary)
	}

	body := bytes.NewBufferString(`{"dry_run":true,"kinds":["thumbnail"]}`)
	w = serveCacheRequest(r, http.MethodPost, "/api/storage/cache/clean", body)
	if w.Code != http.StatusOK {
		t.Fatalf("dry-run 期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(thumb); err != nil {
		t.Fatalf("dry-run 不应删除文件: %v", err)
	}

	body = bytes.NewBufferString(`{"dry_run":false,"kinds":["thumbnail"]}`)
	w = serveCacheRequest(r, http.MethodPost, "/api/storage/cache/clean", body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("执行清理期望 202, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(thumb); !os.IsNotExist(err) {
		t.Fatalf("执行清理应删除缩略图, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(hlsDir, "master.m3u8")); err != nil {
		t.Fatalf("HLS 未被选中，不应删除: %v", err)
	}
	var taskCount int64
	if err := db.Model(&models.Task{}).Where("type = ?", storage.TaskTypeCacheClean).Count(&taskCount).Error; err != nil {
		t.Fatalf("统计清理任务失败: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("执行清理应写入任务中心，实际 %d", taskCount)
	}
	var task models.Task
	if err := db.Where("type = ?", storage.TaskTypeCacheClean).First(&task).Error; err != nil {
		t.Fatalf("读取清理任务失败: %v", err)
	}
	if task.Status != models.TaskStatusSucceeded || task.Progress != 100 || task.FinishedAt == nil {
		t.Fatalf("清理任务状态不符: %+v", task)
	}
	var auditCount int64
	if err := db.Model(&models.AuditEvent{}).Where("action IN ?", []string{"cache.clean.preview", "cache.clean.executed"}).Count(&auditCount).Error; err != nil {
		t.Fatalf("统计审计失败: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("dry-run 与执行清理应写审计，实际 %d", auditCount)
	}
}

func TestStorageCacheAPI_RejectsUnsafeCleanKind(t *testing.T) {
	r, _, _ := setupCacheRouter(t)
	body := bytes.NewBufferString(`{"dry_run":true,"kinds":["database"]}`)
	w := serveCacheRequest(r, http.MethodPost, "/api/storage/cache/clean", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 kind 期望 400, 实际 %d, body=%s", w.Code, w.Body.String())
	}
}

func serveCacheRequest(r *gin.Engine, method, path string, body *bytes.Buffer) *httptest.ResponseRecorder {
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func mustWriteAPIFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}
