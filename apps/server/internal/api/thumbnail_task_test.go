package api

import (
	"bytes"
	"context"
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
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	thumbsvc "github.com/wcpe/JianVideo/internal/thumbnail"
)

func setupThumbnailTaskRouter(t *testing.T) (*gin.Engine, *gorm.DB, *tasksvc.WorkerRegistry) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "api.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取底层测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Space{}, &models.LibraryPath{}, &models.MediaFile{}, &models.Task{}, &models.CacheAsset{}); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	if err := db.Create(&models.Space{ID: "space-a", Name: "空间 A", OwnerUserID: 1}).Error; err != nil {
		t.Fatalf("创建测试 Space 失败: %v", err)
	}
	libService := library.NewService(db)
	taskService := tasksvc.NewService(db)
	cacheService := storage.NewService(db, dataDir).WithTasks(taskService)
	thumbnailService := thumbsvc.NewService(libService, taskService, cacheService, dataDir)
	thumbnailService.SetGeneratorForTest(func(_ context.Context, _ models.MediaFile, _ int, outputPath string) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("task-jpeg"), 0o640)
	})
	registry := tasksvc.NewWorkerRegistry(taskService)
	if err := thumbnailService.RegisterWorkers(registry, 2); err != nil {
		t.Fatalf("注册缩略图 worker 失败: %v", err)
	}
	handler := NewHandler(libService).WithTasks(taskService).WithCache(cacheService).WithThumbnail(thumbnailService)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("space_id", "space-a")
		c.Next()
	})
	RegisterRoutes(router, handler)
	return router, db, registry
}

func createAPIMedia(t *testing.T, db *gorm.DB, name string) models.MediaFile {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("source"), 0o640); err != nil {
		t.Fatalf("写入媒体失败: %v", err)
	}
	libraryPath := models.LibraryPath{SpaceID: "space-a", Path: root, Type: "local", Enabled: 1}
	if err := db.Create(&libraryPath).Error; err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	media := models.MediaFile{SpaceID: "space-a", LibraryID: libraryPath.ID, FilePath: path, FileName: name, Format: filepath.Ext(name)[1:]}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	return media
}

func TestThumbnailAPI缺失时返回任务信息并生成后返回图片(t *testing.T) {
	router, db, registry := setupThumbnailTaskRouter(t)
	media := createAPIMedia(t, db, "image.jpg")
	url := "/api/library/thumbnail/" + strconv.FormatInt(media.ID, 10) + "?size=160&probe=1"

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodGet, url, nil)
	firstRequest.Header.Set("X-JianVideo-Space-Id", "space-a")
	router.ServeHTTP(first, firstRequest)
	if first.Code != http.StatusAccepted {
		t.Fatalf("缺失缩略图应返回 202，实际 %d body=%s", first.Code, first.Body.String())
	}
	var queued struct {
		Code   string `json:"code"`
		TaskID int64  `json:"task_id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &queued); err != nil {
		t.Fatalf("解析 202 响应失败: %v", err)
	}
	if queued.Code != "GENERATING" || queued.TaskID == 0 {
		t.Fatalf("202 响应缺少任务信息: %+v", queued)
	}

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, url, nil)
	secondRequest.Header.Set("X-JianVideo-Space-Id", "space-a")
	router.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusAccepted {
		t.Fatalf("活动任务重复探测仍应返回 202，实际 %d", second.Code)
	}
	var duplicate struct {
		TaskID int64 `json:"task_id"`
	}
	_ = json.Unmarshal(second.Body.Bytes(), &duplicate)
	if duplicate.TaskID != queued.TaskID {
		t.Fatalf("重复探测应复用任务: first=%d second=%d", queued.TaskID, duplicate.TaskID)
	}

	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("执行缩略图任务失败: %v", err)
	}
	ready := httptest.NewRecorder()
	readyRequest := httptest.NewRequest(http.MethodGet, url, nil)
	readyRequest.Header.Set("X-JianVideo-Space-Id", "space-a")
	router.ServeHTTP(ready, readyRequest)
	if ready.Code != http.StatusOK || ready.Body.String() != "task-jpeg" {
		t.Fatalf("生成后应返回真实图片: code=%d body=%q", ready.Code, ready.Body.String())
	}
}

func TestThumbnailBackfillAPI入队三档或指定尺寸(t *testing.T) {
	router, db, _ := setupThumbnailTaskRouter(t)
	createAPIMedia(t, db, "one.jpg")
	createAPIMedia(t, db, "two.mp4")

	body := bytes.NewBufferString(`{"sizes":[160,640]}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/library/thumbnails/backfill", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-JianVideo-Space-Id", "space-a")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("批量预生成应返回 202，实际 %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Status string `json:"status"`
		TaskID int64  `json:"task_id"`
		Sizes  []int  `json:"sizes"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析批量响应失败: %v", err)
	}
	if response.Status != "queued" || response.TaskID == 0 || len(response.Sizes) != 2 {
		t.Fatalf("批量响应无效: %+v", response)
	}
}
