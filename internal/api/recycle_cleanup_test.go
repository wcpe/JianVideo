package api

import (
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

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
)

// setupCleanupRouter 构建带 library + settings 服务的测试路由，并返回服务以便造数据。
func setupCleanupRouter(t *testing.T) (*gin.Engine, *library.Service, *settings.Service, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}, &models.Setting{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	libSvc := library.NewService(gdb)
	setSvc := settings.NewService(gdb)
	h := NewHandler(libSvc).WithSettings(setSvc)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, libSvc, setSvc, gdb
}

// softDeleteOne 造一条软删媒体（带真实源文件），返回源文件磁盘路径。
func softDeleteOne(t *testing.T, libSvc *library.Service, gdb *gorm.DB, dir, name string) string {
	t.Helper()
	src := filepath.Join(dir, name)
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	mf, err := libSvc.CreateMediaFile(1, filepath.ToSlash(src), 100)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", mf.ID).
		Update("deleted_at", time.Now()).Error; err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	return src
}

// TestRecycleCleanup_UnsetPathRejected 未配置盘符路径时返回 409 且不移动文件。
func TestRecycleCleanup_UnsetPathRejected(t *testing.T) {
	r, libSvc, setSvc, gdb := setupCleanupRouter(t)
	dir := t.TempDir()
	src := softDeleteOne(t, libSvc, gdb, dir, "a.mp4")
	// 故意配置一个不含该盘符的映射
	_ = setSvc.Set(settings.KeyRecycleBinPaths, `{"Z":"Z:/.recycle"}`)

	req := httptest.NewRequest("POST", "/api/library/recycle/cleanup", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("未配路径期望 409, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("拒绝后源文件应仍在, 实际: %v", err)
	}
	var deleted []models.MediaFile
	gdb.Where("deleted_at IS NOT NULL").Find(&deleted)
	if len(deleted) != 1 {
		t.Fatalf("拒绝后软删记录应仍在, 实际 %d 条", len(deleted))
	}
}

// TestRecycleCleanup_Success 配置好盘符后清理：文件移动、记录删除、返回统计。
func TestRecycleCleanup_Success(t *testing.T) {
	r, libSvc, setSvc, gdb := setupCleanupRouter(t)
	dir := t.TempDir()
	src := softDeleteOne(t, libSvc, gdb, dir, "b.mp4")

	drive := ""
	if len(src) >= 2 && src[1] == ':' {
		drive = string(src[0])
	}
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过成功用例")
	}
	recycleDir := filepath.Join(dir, ".recycle")
	_ = setSvc.Set(settings.KeyRecycleBinPaths, `{"`+drive+`":"`+filepath.ToSlash(recycleDir)+`"}`)

	req := httptest.NewRequest("POST", "/api/library/recycle/cleanup", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("清理期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Moved  int `json:"moved"`
		Failed int `json:"failed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if resp.Moved != 1 || resp.Failed != 0 {
		t.Fatalf("期望 moved=1 failed=0, 实际 %+v", resp)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("清理后源文件应已移走, 实际 stat err=%v", err)
	}
	var deleted []models.MediaFile
	gdb.Where("deleted_at IS NOT NULL").Find(&deleted)
	if len(deleted) != 0 {
		t.Fatalf("清理后回收站应为空, 实际 %d 条", len(deleted))
	}
}

// TestRecycleCleanup_SettingsUnavailable 未注入 settings 服务返回 503。
func TestRecycleCleanup_SettingsUnavailable(t *testing.T) {
	h := NewHandler(library.NewService(nil))
	r := gin.New()
	RegisterRoutes(r, h)

	req := httptest.NewRequest("POST", "/api/library/recycle/cleanup", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用设置服务期望 503, 实际 %d", w.Code)
	}
}
