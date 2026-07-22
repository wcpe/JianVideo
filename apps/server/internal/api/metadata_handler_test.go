package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func TestMetadataAPIGetRefreshAndBackfill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.MediaMetadata{}, &models.Task{}); err != nil {
		t.Fatalf("迁移元数据表失败: %v", err)
	}
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/media/clip.mp4", FileName: "clip.mp4", Format: "mp4", FileState: models.MediaFileStateAvailable, AddedAt: time.Now(), ModifiedAt: time.Now()}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	row := models.MediaMetadata{MediaID: media.ID, SpaceID: media.SpaceID, Source: library.MetadataSourceFFprobe, Tool: "ffprobe", ToolVersion: "7.1", RawJSON: `{}`, NormalizedJSON: `{"container":{"format_name":"mov,mp4"}}`, ParsedAt: time.Now(), Stale: false}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("创建元数据失败: %v", err)
	}

	tasks := tasksvc.NewService(db)
	h := NewHandler(library.NewService(db)).WithTasks(tasks)
	router := gin.New()
	RegisterRoutes(router, h)

	get := httptest.NewRequest(http.MethodGet, "/api/library/media/"+apiInt64(media.ID)+"/metadata", nil)
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, get)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("查询期望 200, 实际 %d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	var getResp struct {
		Items []models.MediaMetadata `json:"items"`
	}
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &getResp); err != nil || len(getResp.Items) != 1 {
		t.Fatalf("查询响应错误: err=%v body=%s", err, getRecorder.Body.String())
	}

	refresh := httptest.NewRequest(http.MethodPost, "/api/library/media/"+apiInt64(media.ID)+"/metadata/refresh", nil)
	refreshRecorder := httptest.NewRecorder()
	router.ServeHTTP(refreshRecorder, refresh)
	if refreshRecorder.Code != http.StatusAccepted {
		t.Fatalf("刷新期望 202, 实际 %d body=%s", refreshRecorder.Code, refreshRecorder.Body.String())
	}
	var parseTask models.Task
	if err := db.Where("type = ?", library.TaskTypeMetadataParse).First(&parseTask).Error; err != nil {
		t.Fatalf("应创建 metadata.parse 任务: %v", err)
	}
	if parseTask.MaxAttempts != 3 || parseTask.ResourceID != apiInt64(media.ID) {
		t.Fatalf("解析任务重试或资源字段错误: %+v", parseTask)
	}
	var stale models.MediaMetadata
	if err := db.First(&stale, row.ID).Error; err != nil || !stale.Stale {
		t.Fatalf("刷新入队前应标记 stale: err=%v row=%+v", err, stale)
	}

	backfill := httptest.NewRequest(http.MethodPost, "/api/library/metadata/backfill", nil)
	backfillRecorder := httptest.NewRecorder()
	router.ServeHTTP(backfillRecorder, backfill)
	if backfillRecorder.Code != http.StatusAccepted {
		t.Fatalf("回填期望 202, 实际 %d body=%s", backfillRecorder.Code, backfillRecorder.Body.String())
	}
	var backfillTask models.Task
	if err := db.Where("type = ?", library.TaskTypeMetadataBackfill).First(&backfillTask).Error; err != nil {
		t.Fatalf("应创建 metadata.backfill 任务: %v", err)
	}
}

func TestMetadataRefreshRejectsCrossSpaceMedia(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.MediaMetadata{}, &models.Task{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	media := models.MediaFile{SpaceID: "space-other", LibraryID: 1, FilePath: "D:/media/x.mp4", FileName: "x.mp4", Format: "mp4", FileState: models.MediaFileStateAvailable}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	h := NewHandler(library.NewService(db)).WithTasks(tasksvc.NewService(db))
	router := gin.New()
	RegisterRoutes(router, h)
	var beforeCount int64
	if err := db.Model(&models.Task{}).Count(&beforeCount).Error; err != nil {
		t.Fatalf("读取任务基线失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/library/media/"+apiInt64(media.ID)+"/metadata/refresh", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("跨 Space 刷新应 404, 实际 %d", recorder.Code)
	}
	var count int64
	if err := db.WithContext(context.Background()).Model(&models.Task{}).Count(&count).Error; err != nil || count != beforeCount {
		t.Fatalf("跨 Space 不应入队: before=%d after=%d err=%v", beforeCount, count, err)
	}
}

func TestMetadataBackfillRejectsInvalidInput(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	h := NewHandler(library.NewService(db)).WithTasks(tasksvc.NewService(db))
	router := gin.New()
	RegisterRoutes(router, h)
	var beforeCount int64
	if err := db.Model(&models.Task{}).Where("type = ?", library.TaskTypeMetadataBackfill).Count(&beforeCount).Error; err != nil {
		t.Fatalf("读取元数据回填任务基线失败: %v", err)
	}

	for _, body := range []string{`{"library_id":"bad"}`, `{"library_id":-1}`, `{`} {
		req := httptest.NewRequest(http.MethodPost, "/api/library/metadata/backfill", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("非法请求 %s 应返回 400，实际 %d body=%s", body, recorder.Code, recorder.Body.String())
		}
	}
	var count int64
	if err := db.Model(&models.Task{}).Where("type = ?", library.TaskTypeMetadataBackfill).Count(&count).Error; err != nil || count != beforeCount {
		t.Fatalf("非法回填请求不应入队: before=%d after=%d err=%v", beforeCount, count, err)
	}
}

func apiInt64(value int64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[value%10]
		value /= 10
	}
	return string(buf[i:])
}
