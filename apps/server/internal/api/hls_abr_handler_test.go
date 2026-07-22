package api

import (
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
	"github.com/wcpe/JianVideo/internal/settings"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

func setupABRRouter(t *testing.T) (*gin.Engine, *tasksvc.Service, *tasksvc.WorkerRegistry, int64) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "abr-api.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 ABR API 测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取 ABR API 测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.MediaFile{}, &models.Task{}, &models.Setting{}); err != nil {
		t.Fatalf("迁移 ABR API 测试表失败: %v", err)
	}
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: filepath.Join(t.TempDir(), "video.mp4"), FileName: "video.mp4", Width: 1280, Height: 720}
	if err := os.WriteFile(media.FilePath, []byte("video"), 0o640); err != nil {
		t.Fatalf("创建 ABR API 媒体失败: %v", err)
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建 ABR API 媒体记录失败: %v", err)
	}
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	root := filepath.Join(t.TempDir(), "hls")
	abr := transcoder.NewABRService(tasks, workers, root, func(_ context.Context, _ int64, payload transcoder.ABRPayload) error {
		dir, err := transcoder.HLSProfileDir(root, payload.SpaceID, payload.MediaID, payload.ProfileID)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "master.m3u8"), []byte("#EXTM3U\n"), 0o640)
	})
	if err := abr.RegisterWorker(1); err != nil {
		t.Fatalf("注册 ABR API worker 失败: %v", err)
	}
	settingSvc := settings.NewService(db)
	if err := settingSvc.Set(settings.KeyTranscodeABRLadder, `["1080p","720p","480p"]`); err != nil {
		t.Fatalf("写入 ABR ladder 设置失败: %v", err)
	}
	handler := NewHandler(library.NewService(db)).WithSettings(settingSvc).WithTasks(tasks).WithHLSABR(abr)
	router := gin.New()
	RegisterRoutes(router, handler)
	return router, tasks, workers, media.ID
}

func TestCreateHLSABRIsExplicitAndReturnsUnifiedTask(t *testing.T) {
	router, tasks, workers, mediaID := setupABRRouter(t)
	response := doJSON(t, router, http.MethodPost, "/api/play/"+strconv.FormatInt(mediaID, 10)+"/hls-abr", `{"priority":9,"force_rebuild":true}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("ABR 显式入队期望 202, 实际 %d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		TaskID    int64  `json:"task_id"`
		ProfileID string `json:"profile_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.TaskID == 0 || body.ProfileID != transcoder.ABRProfileID {
		t.Fatalf("ABR 入队响应异常: body=%s err=%v", response.Body.String(), err)
	}
	task, err := tasks.Get(context.Background(), body.TaskID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
	if err != nil || task.Type != transcoder.TaskTypeHLSABR || task.Priority != 9 {
		t.Fatalf("ABR 统一任务异常: task=%+v err=%v", task, err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行 ABR API worker 失败: %v", err)
	}
	finished, err := tasks.Get(context.Background(), body.TaskID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
	if err != nil || finished.Status != models.TaskStatusSucceeded {
		t.Fatalf("ABR API 任务未成功: task=%+v err=%v", finished, err)
	}
	status := doJSON(t, router, http.MethodGet, "/api/play/"+strconv.FormatInt(mediaID, 10)+"/hls-status?profile_id="+transcoder.ABRProfileID, "")
	if status.Code != http.StatusOK || !json.Valid(status.Body.Bytes()) {
		t.Fatalf("ABR 状态查询失败: code=%d body=%s", status.Code, status.Body.String())
	}
	var statusBody struct {
		Available bool   `json:"available"`
		ProfileID string `json:"profile_id"`
		URL       string `json:"url"`
	}
	_ = json.Unmarshal(status.Body.Bytes(), &statusBody)
	if !statusBody.Available || statusBody.ProfileID != transcoder.ABRProfileID || statusBody.URL != "/api/play/hls/"+strconv.FormatInt(mediaID, 10)+"/profiles/abr-h264/master.m3u8" {
		t.Fatalf("ABR 状态响应异常: %+v", statusBody)
	}
}

func TestCreateHLSABRRejectsMediaFromAnotherSpace(t *testing.T) {
	router, _, _, mediaID := setupABRRouter(t)
	request, err := http.NewRequest(http.MethodPost, "/api/play/"+strconv.FormatInt(mediaID, 10)+"/hls-abr", nil)
	if err != nil {
		t.Fatalf("创建跨 Space 请求失败: %v", err)
	}
	request.Header.Set("X-JianVideo-Space-Id", "space-other")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("跨 Space ABR 入队应返回 404, 实际 %d body=%s", response.Code, response.Body.String())
	}
}
