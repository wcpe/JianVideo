package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

func setupUnifiedHLSPreviewRouter(t *testing.T) (*gin.Engine, *tasksvc.Service, *tasksvc.WorkerRegistry, string, int64, *transcoder.HLSPreviewService) {
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
	preview := transcoder.NewHLSPreviewService(tasks, workers, hlsRoot, func(_ context.Context, taskID int64, payload transcoder.HLSPreviewPayload) error {
		dir := filepath.Join(hlsRoot, payload.SpaceID, strconv.FormatInt(payload.MediaID, 10), payload.ProfileID)
		if transcoder.IsAudioReloadProfileID(payload.ProfileID) {
			dir = filepath.Join(dir, "tasks", strconv.FormatInt(taskID, 10))
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
	return r, tasks, workers, hlsRoot, media.ID, preview
}

func TestLegacyTranscodeTasksAPIUsesUnifiedHLSPreviewTask(t *testing.T) {
	r, tasks, workers, _, mediaID, _ := setupUnifiedHLSPreviewRouter(t)
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
	r, tasks, workers, _, mediaID, _ := setupUnifiedHLSPreviewRouter(t)
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
	r, _, workers, _, mediaID, _ := setupUnifiedHLSPreviewRouter(t)
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
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if _, exists := raw["effective_track_id"]; exists {
		t.Fatalf("普通 profile 不得输出 effective_track_id: %s", w.Body.String())
	}
}

func TestHLSStatusTaskIDQueriesSameProfileTasksExactly(t *testing.T) {
	r, _, workers, _, mediaID, preview := setupUnifiedHLSPreviewRouter(t)
	request := transcoder.AudioReloadRequest{SpaceID: models.DefaultSpaceID, MediaID: mediaID, AudioTrackID: "audio-track", AudioStreamIndex: 2, SourceFingerprint: "fingerprint-1"}
	first, err := preview.EnqueueAudioReload(context.Background(), request)
	if err != nil {
		t.Fatalf("创建首个音轨任务失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行首个音轨任务失败: %v", err)
	}
	request.SourceFingerprint = "fingerprint-2"
	second, err := preview.EnqueueAudioReload(context.Background(), request)
	if err != nil {
		t.Fatalf("创建第二个音轨任务失败: %v", err)
	}
	profileID := transcoder.AudioReloadProfileID(request.AudioTrackID)
	baseURL := "/api/play/" + strconv.FormatInt(mediaID, 10) + "/hls-status?profile_id=" + profileID
	firstStatus := doJSON(t, r, http.MethodGet, baseURL+"&task_id="+strconv.FormatInt(first.ID, 10), "")
	if firstStatus.Code != http.StatusOK || !strings.Contains(firstStatus.Body.String(), `"effective_track_id":"audio-track"`) || !strings.Contains(firstStatus.Body.String(), `"id":"`+strconv.FormatInt(first.ID, 10)+`"`) {
		t.Fatalf("首个精确任务状态错误: code=%d body=%s", firstStatus.Code, firstStatus.Body.String())
	}
	secondStatus := doJSON(t, r, http.MethodGet, baseURL+"&task_id="+strconv.FormatInt(second.ID, 10), "")
	if secondStatus.Code != http.StatusOK || strings.Contains(secondStatus.Body.String(), "effective_track_id") || !strings.Contains(secondStatus.Body.String(), `"id":"`+strconv.FormatInt(second.ID, 10)+`"`) {
		t.Fatalf("第二个 pending 精确任务状态错误: code=%d body=%s", secondStatus.Code, secondStatus.Body.String())
	}
	latest := doJSON(t, r, http.MethodGet, baseURL, "")
	if latest.Code != http.StatusBadRequest || !strings.Contains(latest.Body.String(), `"code":"HLS_TASK_ID_REQUIRED"`) {
		t.Fatalf("音轨 profile 缺少 task_id 必须拒绝: code=%d body=%s", latest.Code, latest.Body.String())
	}
	crossSpaceRequest := doJSONRequest(t, http.MethodGet, baseURL+"&task_id="+strconv.FormatInt(first.ID, 10), "")
	crossSpaceRequest.Header.Set(spaceHeader, "space-other")
	crossSpace := serveRequest(r, crossSpaceRequest)
	if crossSpace.Code != http.StatusNotFound {
		t.Fatalf("跨 Space 精确任务必须拒绝: code=%d body=%s", crossSpace.Code, crossSpace.Body.String())
	}
}

func TestHLSStatusRequiresTaskIDForAudioProfile(t *testing.T) {
	r, _, _, _, mediaID, _ := setupUnifiedHLSPreviewRouter(t)
	profileID := transcoder.AudioReloadProfileID("audio-track")
	w := doJSON(t, r, http.MethodGet, "/api/play/"+strconv.FormatInt(mediaID, 10)+"/hls-status?profile_id="+profileID, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("音轨 HLS 状态缺少 task_id 期望 400, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var response struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析音轨 HLS 错误响应失败: %v", err)
	}
	if response.Code != "HLS_TASK_ID_REQUIRED" || response.Message == "" {
		t.Fatalf("音轨 HLS 缺少 task_id 错误响应不明确: %+v", response)
	}
}

func TestHLSStatusAudioProfileAliasStillRequiresTaskID(t *testing.T) {
	r, _, _, _, mediaID, _ := setupUnifiedHLSPreviewRouter(t)
	profileID := strings.ToUpper(transcoder.AudioReloadProfileID("audio-track"))
	w := doJSON(t, r, http.MethodGet, "/api/play/"+strconv.FormatInt(mediaID, 10)+"/hls-status?profile_id="+profileID, "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"HLS_TASK_ID_REQUIRED"`) {
		t.Fatalf("大小写别名不得绕过 task_id: code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHLSStatusInternalFailureUsesFixed500WithoutLeakingError(t *testing.T) {
	r, _, _, _, mediaID, preview := setupUnifiedHLSPreviewRouter(t)
	task, err := preview.EnqueueAudioReload(context.Background(), transcoder.AudioReloadRequest{
		SpaceID: models.DefaultSpaceID, MediaID: mediaID, AudioTrackID: "audio-track-a", AudioStreamIndex: 2,
		SourceFingerprint: "source-fingerprint",
	})
	if err != nil {
		t.Fatalf("创建状态错误测试任务失败: %v", err)
	}
	profileID := transcoder.AudioReloadProfileID("audio-track-b")
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })
	w := doJSON(t, r, http.MethodGet, "/api/play/"+strconv.FormatInt(mediaID, 10)+"/hls-status?profile_id="+profileID+"&task_id="+strconv.FormatInt(task.ID, 10), "")
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), `"code":"HLS_STATUS_FAILED"`) {
		t.Fatalf("内部状态错误必须固定返回 500: code=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "不匹配") || strings.Contains(w.Body.String(), "payload") {
		t.Fatalf("内部状态错误不得泄露实现细节: %s", w.Body.String())
	}
	if !strings.Contains(logs.String(), "查询 HLS 状态失败") {
		t.Fatalf("内部状态错误必须写入中文日志: %s", logs.String())
	}
}

func TestHLSStatusRejectsTaskIDForABRProfile(t *testing.T) {
	r, _, _, mediaID := setupABRRouter(t)
	w := doJSON(t, r, http.MethodGet, "/api/play/"+strconv.FormatInt(mediaID, 10)+"/hls-status?profile_id="+transcoder.ABRProfileID+"&task_id=1", "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"INVALID_HLS_TASK_ID"`) {
		t.Fatalf("ABR 状态必须继续拒绝 task_id: code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHLSStatusIncludesEffectiveTrackIDForAudioProfile(t *testing.T) {
	r, _, workers, _, mediaID, preview := setupUnifiedHLSPreviewRouter(t)
	trackID := "emb-stable-audio"
	task, err := preview.EnqueueAudioReload(context.Background(), transcoder.AudioReloadRequest{
		SpaceID: models.DefaultSpaceID, MediaID: mediaID, AudioTrackID: trackID, AudioStreamIndex: 2,
		Width: 1280, Height: 720, SourceFingerprint: "source-fingerprint",
	})
	if err != nil {
		t.Fatalf("创建音轨 reload 任务失败: %v", err)
	}
	profileID := transcoder.AudioReloadProfileID(trackID)
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行音轨 HLS worker 失败: %v", err)
	}
	w := doJSON(t, r, http.MethodGet, "/api/play/"+strconv.FormatInt(mediaID, 10)+"/hls-status?profile_id="+profileID+"&task_id="+strconv.FormatInt(task.ID, 10), "")
	if w.Code != http.StatusOK {
		t.Fatalf("音轨 HLS 状态期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var status struct {
		EffectiveTrackID string `json:"effective_track_id"`
		URL              string `json:"url"`
		Task             *struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("解析音轨 HLS 状态失败: %v", err)
	}
	expectedURL := "/api/play/hls/" + strconv.FormatInt(mediaID, 10) + "/profiles/" + profileID + "/tasks/" + strconv.FormatInt(task.ID, 10) + "/master.m3u8"
	if status.EffectiveTrackID != trackID || status.URL != expectedURL || status.Task == nil || status.Task.ID != strconv.FormatInt(task.ID, 10) {
		t.Fatalf("音轨 HLS 状态未绑定同一 task_id: %+v", status)
	}
}
