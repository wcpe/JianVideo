package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func setupWritebackAPIRouter(t *testing.T) (*gin.Engine, *gorm.DB, *library.Service, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	// 内存库避免 Windows 文件锁导致 t.TempDir 清理失败。
	dsn := "file:fr2033_" + strconv.FormatInt(int64(os.Getpid()), 10) + "_" + strconv.FormatInt(int64(len(t.Name())), 10) + "?mode=memory&cache=shared"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开库: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("底层连接: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.Task{}); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	// dataDir 用 TempDir；dbPath 仅用于 Handler 推导写回快照根目录。
	base := t.TempDir()
	dbPath := filepath.Join(base, "jv.db")
	libSvc := library.NewService(gdb)
	tasks := tasksvc.NewService(gdb)
	h := NewHandler(libSvc).WithTasks(tasks).WithDBPath(dbPath)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, gdb, libSvc, base
}

func seedAPIImage(t *testing.T, lib *library.Service, dir string) int64 {
	t.Helper()
	lp, err := lib.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "lib")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.jpg")
	if err := os.WriteFile(file, []byte("img"), 0o600); err != nil {
		t.Fatal(err)
	}
	mf, err := lib.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 3)
	if err != nil {
		t.Fatal(err)
	}
	// 通过 UpdateField 写入可写字段（测试包无法直接访问 mediaRepo）
	if _, err := lib.UpdateDisplayNameInSpace(models.DefaultSpaceID, mf.ID, "标题"); err != nil {
		t.Fatal(err)
	}
	return mf.ID
}

func TestWritebackAPI_RequiresConfirm(t *testing.T) {
	r, _, lib, _ := setupWritebackAPIRouter(t)
	id := seedAPIImage(t, lib, t.TempDir())

	// 无 body
	req := httptest.NewRequest(http.MethodPost, "/api/library/media/"+strconv.FormatInt(id, 10)+"/metadata/writeback", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("无 confirm 期望 400, 得到 %d body=%s", w.Code, w.Body.String())
	}

	// confirm=false
	body, _ := json.Marshal(map[string]bool{"confirm_writeback": false})
	req = httptest.NewRequest(http.MethodPost, "/api/library/media/"+strconv.FormatInt(id, 10)+"/metadata/writeback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("confirm=false 期望 400, 得到 %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != "CONFIRM_REQUIRED" {
		t.Fatalf("code=%v", resp["code"])
	}
}

func TestWritebackAPI_EnqueueWithConfirm(t *testing.T) {
	r, db, lib, _ := setupWritebackAPIRouter(t)
	id := seedAPIImage(t, lib, t.TempDir())

	body, _ := json.Marshal(map[string]bool{"confirm_writeback": true})
	req := httptest.NewRequest(http.MethodPost, "/api/library/media/"+strconv.FormatInt(id, 10)+"/metadata/writeback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("期望 202, 得到 %d body=%s", w.Code, w.Body.String())
	}
	var count int64
	if err := db.Model(&models.Task{}).Where("type = ?", library.TaskTypeMetadataWriteback).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("任务数: %d err=%v", count, err)
	}
}

func TestWritebackAPI_RejectsVideo(t *testing.T) {
	r, _, lib, _ := setupWritebackAPIRouter(t)
	dir := t.TempDir()
	lp, _ := lib.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "lib")
	file := filepath.Join(dir, "v.mp4")
	_ = os.WriteFile(file, []byte("vid"), 0o600)
	mf, err := lib.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 3)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]bool{"confirm_writeback": true})
	req := httptest.NewRequest(http.MethodPost, "/api/library/media/"+strconv.FormatInt(mf.ID, 10)+"/metadata/writeback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("视频期望 400, 得到 %d body=%s", w.Code, w.Body.String())
	}
}
