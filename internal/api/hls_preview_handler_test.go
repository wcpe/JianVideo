package api

import (
	"context"
	"encoding/json"
	"net/http"
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
	"github.com/wcpe/JianVideo/internal/transcoder"
)

func setupUnifiedHLSPreviewRouter(t *testing.T) (*gin.Engine, *tasksvc.Service, *tasksvc.WorkerRegistry, string, int64) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "hls-preview-test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 HLS preview 测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取 HLS preview 测试连接失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.TranscodePreset{}, &models.Task{}); err != nil {
		t.Fatalf("迁移 HLS preview 测试表失败: %v", err)
	}
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: filepath.Join(t.TempDir(), "video.mp4"), FileName: "video.mp4"}
	if err := os.WriteFile(media.FilePath, []byte("video"), 0o640); err != nil {
		t.Fatalf("创建媒体文件失败: %v", err)
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体记录失败: %v", err)
	}
	preset := models.TranscodePreset{Name: "H.264 预览", Codec: "h264", Width: 1280, Height: 720}
	if err := db.Create(&preset).Error; err != nil {
		t.Fatalf("创建预设失败: %v", err)
	}
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	hlsRoot := filepath.Join(t.TempDir(), "hls")
	preview := transcoder.NewHLSPreviewService(tasks, workers, hlsRoot, func(_ context.Context, _ int64, payload transcoder.HLSPreviewPayload) error {
		dir, err := transcoder.HLSProfileDir(hlsRoot, payload.SpaceID, payload.MediaID, payload.ProfileID)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "master.m3u8"), []byte("#EXTM3U\n"), 0o640)
	})
	if err := preview.RegisterWorker(); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}
	h := NewHandler(library.NewService(db)).WithTranscodePresets(transcoder.NewPresetStore(db), nil).WithTasks(tasks).WithHLSPreview(preview)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, tasks, workers, hlsRoot, media.ID
}

func TestLegacyTranscodeTasksAPIUsesUnifiedHLSPreviewTask(t *testing.T) {
	r, tasks, workers, _, mediaID := setupUnifiedHLSPreviewRouter(t)
	w := doJSON(t, r, http.MethodPost, "/api/transcode/tasks", `{"media_id":`+strconv.FormatInt(mediaID, 10)+`,"preset_id":1,"priority":8,"force_rebuild":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("兼容入队期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var response struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.TaskID == 0 {
		t.Fatalf("解析统一任务 ID 失败: body=%s err=%v", w.Body.String(), err)
	}
	task, err := tasks.Get(context.Background(), response.TaskID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("读取统一任务失败: %v", err)
	}
	if task.Type != transcoder.TaskTypeHLSPreview || task.Priority != 8 {
		t.Fatalf("兼容接口未映射统一任务: %+v", task)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行 HLS worker 失败: %v", err)
	}
	w = doJSON(t, r, http.MethodGet, "/api/transcode/tasks", "")
	if w.Code != http.StatusOK || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("兼容列表失败: code=%d body=%s", w.Code, w.Body.String())
	}
	var list struct {
		Tasks []struct {
			ID       int64   `json:"id"`
			Status   string  `json:"status"`
			Progress float64 `json:"progress"`
			Width    int     `json:"width"`
			Height   int     `json:"height"`
		} `json:"tasks"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Tasks) != 1 || list.Tasks[0].ID != response.TaskID || list.Tasks[0].Status != "completed" || list.Tasks[0].Progress != 1 || list.Tasks[0].Width != 1280 || list.Tasks[0].Height != 720 {
		t.Fatalf("兼容任务列表映射异常: %+v", list.Tasks)
	}
}

func TestHLSPreviewTaskCanCancelAndRetryThroughUnifiedAPI(t *testing.T) {
	r, tasks, workers, _, mediaID := setupUnifiedHLSPreviewRouter(t)
	w := doJSON(t, r, http.MethodPost, "/api/transcode/tasks", `{"media_id":`+strconv.FormatInt(mediaID, 10)+`,"preset_id":1}`)
	var queued struct {
		TaskID int64 `json:"task_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &queued)
	w = doJSON(t, r, http.MethodPost, "/api/tasks/"+strconv.FormatInt(queued.TaskID, 10)+"/cancel", "")
	if w.Code != http.StatusOK {
		t.Fatalf("取消 HLS preview 期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	canceled, err := tasks.Get(context.Background(), queued.TaskID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
	if err != nil || canceled.Status != models.TaskStatusCanceled {
		t.Fatalf("取消 HLS preview 终态异常: task=%+v err=%v", canceled, err)
	}
	w = doJSON(t, r, http.MethodPost, "/api/tasks/"+strconv.FormatInt(queued.TaskID, 10)+"/retry", "")
	if w.Code != http.StatusOK {
		t.Fatalf("重试 HLS preview 期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行重试 HLS preview 失败: %v", err)
	}
	done, err := tasks.Get(context.Background(), queued.TaskID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
	if err != nil || done.Status != models.TaskStatusSucceeded {
		t.Fatalf("重试 HLS preview 终态异常: task=%+v err=%v", done, err)
	}
}

func TestHLSStatusReturnsAvailabilityTaskAndProfileURL(t *testing.T) {
	r, _, workers, _, mediaID := setupUnifiedHLSPreviewRouter(t)
	w := doJSON(t, r, http.MethodPost, "/api/transcode/tasks", `{"media_id":`+strconv.FormatInt(mediaID, 10)+`,"preset_id":1}`)
	var queued struct {
		TaskID int64 `json:"task_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &queued)
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行 HLS worker 失败: %v", err)
	}
	w = doJSON(t, r, http.MethodGet, "/api/play/"+strconv.FormatInt(mediaID, 10)+"/hls-status?profile_id=h264", "")
	if w.Code != http.StatusOK {
		t.Fatalf("HLS 状态期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var status struct {
		Available bool   `json:"available"`
		ProfileID string `json:"profile_id"`
		URL       string `json:"url"`
		Task      *struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	if !status.Available || status.ProfileID != "h264" || status.URL != "/api/play/hls/"+strconv.FormatInt(mediaID, 10)+"/master.m3u8" || status.Task == nil || status.Task.ID != strconv.FormatInt(queued.TaskID, 10) {
		t.Fatalf("HLS 状态响应异常: %+v", status)
	}
}
