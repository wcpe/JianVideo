package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/openapi"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func setupV2Router(t *testing.T) (*gin.Engine, *library.Service, *tasksvc.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(
		&models.Space{},
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.MediaExtension{},
		&models.Task{},
	); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	if err := gdb.Create(&models.Space{
		ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("创建默认 Space 失败: %v", err)
	}
	libSvc := library.NewService(gdb)
	taskSvc := tasksvc.NewService(gdb)
	h := NewHandler(libSvc).WithTasks(taskSvc)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, libSvc, taskSvc
}

func TestListMediaV2_ReturnsContractShape(t *testing.T) {
	r, svc, _ := setupV2Router(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "")
	if err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if _, err := svc.CreateMediaFile(lp.ID, filepath.Join(dir, "clip.mp4"), 2048); err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v2/media?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListMediaV2 期望 200, 实际 %d body=%s", w.Code, w.Body.String())
	}
	var page openapi.MediaPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("解析 MediaPage 失败: %v", err)
	}
	if page.Total < 1 || len(page.Items) < 1 {
		t.Fatalf("期望至少 1 条媒体, total=%d items=%d", page.Total, len(page.Items))
	}
	item := page.Items[0]
	if item.Id == "" || item.SpaceId == "" || item.Title == "" {
		t.Fatalf("契约必填字段缺失: %+v", item)
	}
	if item.Kind != openapi.Video && item.Kind != openapi.Image {
		t.Fatalf("kind 非法: %s", item.Kind)
	}
	if page.Page != 1 || page.PageSize != 10 {
		t.Fatalf("分页字段不符: page=%d page_size=%d", page.Page, page.PageSize)
	}
}

func TestGetMediaV2_NotFoundAndOK(t *testing.T) {
	r, svc, _ := setupV2Router(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "")
	if err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	mf, err := svc.CreateMediaFile(lp.ID, filepath.Join(dir, "a.mp4"), 100)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}

	// 不存在
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/media/999999", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在媒体期望 404, 实际 %d", w.Code)
	}
	var errBody openapi.Error
	_ = json.Unmarshal(w.Body.Bytes(), &errBody)
	if errBody.Code == "" {
		t.Fatalf("404 应返回契约 Error: %s", w.Body.String())
	}

	// 存在
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/media/"+strconv.FormatInt(mf.ID, 10), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GetMediaV2 期望 200, 实际 %d body=%s", w.Code, w.Body.String())
	}
	var item openapi.MediaItem
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatalf("解析 MediaItem 失败: %v", err)
	}
	if item.Id != strconv.FormatInt(mf.ID, 10) {
		t.Fatalf("id 不符: got %s want %d", item.Id, mf.ID)
	}
}

func TestGetTaskV2_ContractAndUnavailable(t *testing.T) {
	// 无 tasks 服务 → 503
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	_ = gdb.AutoMigrate(&models.Space{}, &models.LibraryPath{}, &models.MediaFile{}, &models.Task{})
	_ = gdb.Create(&models.Space{ID: models.DefaultSpaceID, Name: "默认", OwnerUserID: 1}).Error
	h := NewHandler(library.NewService(gdb)) // 不注入 tasks
	r := gin.New()
	RegisterRoutes(r, h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/tasks/1", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("无 tasks 期望 503, 实际 %d", w.Code)
	}

	// 有 tasks：入队后可读
	r2, _, taskSvc := setupV2Router(t)
	task, err := taskSvc.Enqueue(context.Background(), tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: models.DefaultSpaceID, Type: "library.scan", Priority: 1,
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	w = httptest.NewRecorder()
	r2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/tasks/"+strconv.FormatInt(task.ID, 10), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GetTaskV2 期望 200, 实际 %d body=%s", w.Code, w.Body.String())
	}
	var item openapi.TaskItem
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatalf("解析 TaskItem 失败: %v", err)
	}
	if item.Id != strconv.FormatInt(task.ID, 10) {
		t.Fatalf("task id 不符: %s", item.Id)
	}
	if item.Status != openapi.Pending {
		t.Fatalf("期望 pending, 实际 %s", item.Status)
	}
	if item.Type != "library.scan" {
		t.Fatalf("type 不符: %s", item.Type)
	}
}

func TestToOpenAPIMediaItem_ImageByExt(t *testing.T) {
	mf := &models.MediaFile{
		ID: 1, SpaceID: models.DefaultSpaceID, FileName: "a.jpg", FilePath: "/x/a.jpg",
		Format: "jpg", Duration: 0, AddedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	item := toOpenAPIMediaItem(mf, nil)
	if item.Kind != openapi.Image {
		t.Fatalf("jpg 期望 image, 实际 %s", item.Kind)
	}
	if item.Title != "a.jpg" {
		t.Fatalf("title 应回退 file_name: %s", item.Title)
	}
}
