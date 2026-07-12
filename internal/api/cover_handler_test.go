package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	thumbsvc "github.com/wcpe/JianVideo/internal/thumbnail"
)

func setupCoverRouter(t *testing.T) (*gin.Engine, *gorm.DB, *tasksvc.WorkerRegistry) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "cover-api.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取底层数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&models.Space{}, &models.LibraryPath{}, &models.MediaFile{}, &models.Task{},
		&models.CacheAsset{}, &models.MediaCover{}, &models.CoverCandidate{}, &models.AuditEvent{},
	); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	for _, spaceID := range []string{models.DefaultSpaceID, "space-b"} {
		if err := db.Create(&models.Space{ID: spaceID, Name: spaceID, OwnerUserID: 1}).Error; err != nil {
			t.Fatalf("创建 Space 失败: %v", err)
		}
	}
	recorder := audit.NewRecorder(db)
	lib := library.NewService(db)
	tasks := tasksvc.NewService(db)
	cache := storage.NewService(db, dataDir).WithTasks(tasks)
	thumbnail := thumbsvc.NewService(lib, tasks, cache, dataDir).WithAudit(recorder)
	thumbnail.SetGeneratorForTest(func(_ context.Context, _ models.MediaFile, _ int, outputPath string) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("ordinary-thumbnail"), 0o640)
	})
	thumbnail.SetCoverGeneratorForTest(func(_ context.Context, _ models.MediaFile, timestamp float64, outputPath string) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte(fmt.Sprintf("cover-%.1f", timestamp)), 0o640)
	})
	workers := tasksvc.NewWorkerRegistry(tasks)
	if err := thumbnail.RegisterWorkers(workers, 2); err != nil {
		t.Fatalf("注册缩略图与封面 worker 失败: %v", err)
	}
	handler := NewHandler(lib).WithTasks(tasks).WithCache(cache).WithThumbnail(thumbnail)
	router := gin.New()
	RegisterRoutes(router, handler)
	return router, db, workers
}

func createCoverAPIMedia(t *testing.T, db *gorm.DB, spaceID string) models.MediaFile {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "cover-api.mp4")
	if err := os.WriteFile(path, []byte("source"), 0o640); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}
	libraryPath := models.LibraryPath{SpaceID: spaceID, Path: root, Type: "local", Enabled: 1}
	if err := db.Create(&libraryPath).Error; err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	media := models.MediaFile{
		SpaceID: spaceID, LibraryID: libraryPath.ID, FilePath: path, FileName: filepath.Base(path),
		Format: "mp4", Duration: 10, FileSize: 6, ModifiedAt: time.Now().UTC(), FileState: models.MediaFileStateAvailable,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	return media
}

func TestCoverAPI生成查询选择与封面优先缩略图(t *testing.T) {
	router, db, workers := setupCoverRouter(t)
	media := createCoverAPIMedia(t, db, models.DefaultSpaceID)

	generate := httptest.NewRecorder()
	generateRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/library/media/%d/covers/generate", media.ID), bytes.NewBufferString(`{"refresh":false}`))
	generateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(generate, generateRequest)
	if generate.Code != http.StatusAccepted {
		t.Fatalf("生成封面应返回 202: code=%d body=%s", generate.Code, generate.Body.String())
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行封面任务失败: %v", err)
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/library/media/%d/covers", media.ID), nil))
	if list.Code != http.StatusOK {
		t.Fatalf("查询封面失败: code=%d body=%s", list.Code, list.Body.String())
	}
	var response struct {
		Cover struct {
			Manual bool `json:"manual"`
		} `json:"cover"`
		Candidates []struct {
			ID       int64  `json:"id"`
			ImageURL string `json:"image_url"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析封面响应失败: %v", err)
	}
	if len(response.Candidates) != 5 || response.Candidates[0].ImageURL == "" {
		t.Fatalf("封面候选响应不完整: %+v", response)
	}
	chosen := response.Candidates[4]
	selectRecorder := httptest.NewRecorder()
	selectRequest := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/library/media/%d/cover", media.ID), bytes.NewBufferString(fmt.Sprintf(`{"candidate_id":%d}`, chosen.ID)))
	selectRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(selectRecorder, selectRequest)
	if selectRecorder.Code != http.StatusOK {
		t.Fatalf("人工选择失败: code=%d body=%s", selectRecorder.Code, selectRecorder.Body.String())
	}

	candidate := httptest.NewRecorder()
	router.ServeHTTP(candidate, httptest.NewRequest(http.MethodGet, response.Candidates[0].ImageURL, nil))
	if candidate.Code != http.StatusOK || !bytes.HasPrefix(candidate.Body.Bytes(), []byte("cover-")) {
		t.Fatalf("候选图片资源不可读: code=%d body=%q", candidate.Code, candidate.Body.String())
	}

	thumbnail := httptest.NewRecorder()
	router.ServeHTTP(thumbnail, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/library/thumbnail/%d?size=160", media.ID), nil))
	if thumbnail.Code != http.StatusOK || thumbnail.Body.String() != "cover-9.0" {
		t.Fatalf("统一缩略图路由应优先当前人工封面: code=%d body=%q", thumbnail.Code, thumbnail.Body.String())
	}
}

func TestCoverAPI隔离Space并校验候选归属(t *testing.T) {
	router, db, workers := setupCoverRouter(t)
	mediaA := createCoverAPIMedia(t, db, models.DefaultSpaceID)
	mediaB := createCoverAPIMedia(t, db, "space-b")

	generate := httptest.NewRecorder()
	router.ServeHTTP(generate, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/library/media/%d/covers/generate", mediaA.ID), nil))
	if generate.Code != http.StatusAccepted {
		t.Fatalf("生成失败: %s", generate.Body.String())
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行生成失败: %v", err)
	}
	var candidate models.CoverCandidate
	if err := db.Where("space_id = ? AND media_id = ?", models.DefaultSpaceID, mediaA.ID).First(&candidate).Error; err != nil {
		t.Fatalf("读取候选失败: %v", err)
	}

	crossSpace := httptest.NewRecorder()
	crossSpaceRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/library/media/%d/covers", mediaA.ID), nil)
	crossSpaceRequest.Header.Set("X-JianVideo-Space-Id", "space-b")
	router.ServeHTTP(crossSpace, crossSpaceRequest)
	if crossSpace.Code != http.StatusNotFound {
		t.Fatalf("跨 Space 查询必须返回 404，实际 %d", crossSpace.Code)
	}

	crossMedia := httptest.NewRecorder()
	crossMediaRequest := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/library/media/%d/cover", mediaB.ID), bytes.NewBufferString(fmt.Sprintf(`{"candidate_id":%d}`, candidate.ID)))
	crossMediaRequest.Header.Set("Content-Type", "application/json")
	crossMediaRequest.Header.Set("X-JianVideo-Space-Id", "space-b")
	router.ServeHTTP(crossMedia, crossMediaRequest)
	if crossMedia.Code != http.StatusNotFound {
		t.Fatalf("跨媒体候选选择必须返回 404，实际 %d body=%s", crossMedia.Code, crossMedia.Body.String())
	}
}
