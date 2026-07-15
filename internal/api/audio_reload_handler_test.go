package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/subtitle"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

type audioReloadFixture struct {
	router  *gin.Engine
	tasks   *tasksvc.Service
	workers *tasksvc.WorkerRegistry
}

func TestAudioReloadCapabilityDecision(t *testing.T) {
	index := 2
	base := subtitle.Track{
		Kind: subtitle.KindAudio, Source: subtitle.SourceEmbedded, Available: true,
		StreamIndex: &index,
	}
	tests := []struct {
		name             string
		track            subtitle.Track
		filePath         string
		previewAvailable bool
		ffmpegAvailable  bool
		capability       string
		reason           string
	}{
		{name: "全部可用", track: base, filePath: "movie.mkv", previewAvailable: true, ffmpegAvailable: true, capability: subtitle.CapabilityReload},
		{name: "预览服务未注入", track: base, filePath: "movie.mkv", ffmpegAvailable: true, capability: subtitle.CapabilityUnsupported, reason: subtitle.ReasonAudioSwitchUnsupported},
		{name: "FFmpeg不可用", track: base, filePath: "movie.mkv", previewAvailable: true, capability: subtitle.CapabilityUnsupported, reason: subtitle.ReasonAudioReloadFFmpegUnavailable},
		{name: "SMB媒体", track: base, filePath: "smb://server/share/movie.mkv", previewAvailable: true, ffmpegAvailable: true, capability: subtitle.CapabilityUnsupported, reason: subtitle.ReasonSMBAudioReloadUnsupported},
		{name: "缺少流索引", track: audioTrackWithoutIndex(base), filePath: "movie.mkv", previewAvailable: true, ffmpegAvailable: true, capability: subtitle.CapabilityUnsupported, reason: subtitle.ReasonAudioStreamIndexUnavailable},
		{name: "负流索引", track: audioTrackWithIndex(base, -1), filePath: "movie.mkv", previewAvailable: true, ffmpegAvailable: true, capability: subtitle.CapabilityUnsupported, reason: subtitle.ReasonAudioStreamIndexUnavailable},
		{name: "非内嵌音轨", track: audioTrackWithSource(base, subtitle.SourceUploaded), filePath: "movie.mkv", previewAvailable: true, ffmpegAvailable: true, capability: subtitle.CapabilityUnsupported, reason: subtitle.ReasonAudioSwitchUnsupported},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			capability := audioReloadCapability(item.track, item.filePath, item.previewAvailable, item.ffmpegAvailable, transcoder.DefaultHardwarePolicy())
			if capability.Capability != item.capability || capability.UnsupportedReason != item.reason || capability.Available != (item.capability == subtitle.CapabilityReload) {
				t.Fatalf("音轨 reload 能力判定错误: %#v", capability)
			}
		})
	}
}

func TestGetTracksDecoratesAudioReloadCapability(t *testing.T) {
	if !transcoder.IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过动态 reload 能力集成测试")
	}
	fixture := setupSubtitleTrackAPI(t)
	createAudioReloadMetadata(t, fixture, []map[string]any{
		{"index": 1, "codec_name": "aac", "language": "zh", "default": true},
		{"index": 3, "codec_name": "aac", "language": "ja"},
	})
	reload := setupAudioReloadFixture(t, fixture, false)
	response := performSubtitleRequest(reload.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("读取动态音轨能力失败: code=%d body=%s", response.Code, response.Body.String())
	}
	var manifest subtitle.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("解析动态音轨能力失败: %v", err)
	}
	for _, track := range manifest.Tracks {
		if track.Kind == subtitle.KindAudio && (track.Capability != subtitle.CapabilityReload || track.UnsupportedReason != "") {
			t.Fatalf("本地可用音轨必须装饰为 reload: %#v", track)
		}
	}
	backend := manifest.Backend[subtitle.KindAudio]
	if !backend.Available || backend.Capability != subtitle.CapabilityReload || backend.UnsupportedReason != "" {
		t.Fatalf("音频后端能力必须装饰为 reload: %#v", backend)
	}
}

func TestGetTracksKeepsSingleAudioUnsupported(t *testing.T) {
	if !transcoder.IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过单音轨动态能力测试")
	}
	fixture := setupSubtitleTrackAPI(t)
	createAudioReloadMetadata(t, fixture, []map[string]any{
		{"index": 1, "codec_name": "aac", "language": "zh", "default": true},
	})
	reload := setupAudioReloadFixture(t, fixture, false)
	manifest := getTrackManifest(t, reload.router, fixture.media.ID)
	track := findAPITrack(manifest.Tracks, subtitle.KindAudio, "aac")
	if track == nil || track.Capability != subtitle.CapabilityUnsupported || track.UnsupportedReason != subtitle.ReasonAudioSwitchUnsupported {
		t.Fatalf("单音轨媒体必须保持 unsupported: %#v", track)
	}
	backend := manifest.Backend[subtitle.KindAudio]
	if backend.Available || backend.Capability != subtitle.CapabilityUnsupported || backend.UnsupportedReason != subtitle.ReasonAudioSwitchUnsupported {
		t.Fatalf("单音轨后端能力必须保持 unsupported: %#v", backend)
	}
	response := doJSON(t, reload.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), `{"track_id":"`+track.ID+`"}`)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), subtitle.ReasonAudioSwitchUnsupported) {
		t.Fatalf("单音轨 reload 必须被拒绝: code=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAudioReloadReadinessRejectsUnavailableForcedHardware(t *testing.T) {
	if !transcoder.IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过硬件策略 readiness 测试")
	}
	fixture := setupSubtitleTrackAPI(t)
	createAudioReloadMetadata(t, fixture, []map[string]any{{"index": 1, "codec_name": "aac", "default": true}})
	if err := fixture.db.AutoMigrate(&models.Task{}, &models.Setting{}); err != nil {
		t.Fatalf("迁移 readiness 测试表失败: %v", err)
	}
	settingsSvc := settings.NewService(fixture.db)
	if err := settingsSvc.Set(settings.KeyTranscodeHWAccelMode, "qsv"); err != nil {
		t.Fatalf("设置强制硬件策略失败: %v", err)
	}
	if err := settingsSvc.Set(settings.KeyTranscodeHWAccelFallback, "0"); err != nil {
		t.Fatalf("关闭软件回退失败: %v", err)
	}
	ffmpegPath := transcoder.GetFFmpegPath()
	transcoder.SetFFmpegPath(ffmpegPath)
	t.Cleanup(func() { transcoder.SetFFmpegPath(ffmpegPath) })
	tasks := tasksvc.NewService(fixture.db)
	preview := transcoder.NewHLSPreviewService(tasks, tasksvc.NewWorkerRegistry(tasks), filepath.Join(t.TempDir(), "hls"), func(context.Context, int64, transcoder.HLSPreviewPayload) error { return nil })
	handler := NewHandler(library.NewService(fixture.db)).WithSubtitle(fixture.service).WithHLSPreview(preview).WithSettings(settingsSvc)
	router := gin.New()
	RegisterRoutes(router, handler)
	tracks := performSubtitleRequest(router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	assertAudioTrackReason(t, tracks, subtitle.ReasonAudioHardwareUnavailable)
	manifest := getTrackManifest(t, router, fixture.media.ID)
	track := findAPITrack(manifest.Tracks, subtitle.KindAudio, "aac")
	response := doJSON(t, router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), `{"track_id":"`+track.ID+`"}`)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), subtitle.ReasonAudioHardwareUnavailable) {
		t.Fatalf("POST 必须返回结构化硬件不可用: code=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGetTracksAudioReloadStructuredDowngrade(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	createAudioReloadMetadata(t, fixture, []map[string]any{{"index": -1, "codec_name": "aac", "default": true}})
	reload := setupAudioReloadFixture(t, fixture, false)
	response := performSubtitleRequest(reload.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	assertAudioTrackReason(t, response, subtitle.ReasonAudioStreamIndexUnavailable)

	fixture.media.FilePath = "smb://server/share/movie.mkv"
	if err := fixture.db.Save(&fixture.media).Error; err != nil {
		t.Fatalf("更新 SMB 媒体失败: %v", err)
	}
	createAudioReloadMetadata(t, fixture, []map[string]any{{"index": 1, "codec_name": "aac", "default": true}})
	response = performSubtitleRequest(reload.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	assertAudioTrackReason(t, response, subtitle.ReasonSMBAudioReloadUnsupported)
}

func TestCreateAudioReloadValidatesStrictBodyAndAvailability(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	createAudioReloadMetadata(t, fixture, []map[string]any{{"index": 1, "codec_name": "aac", "default": true}})
	manifest := getTrackManifest(t, fixture.router, fixture.media.ID)
	track := findAPITrack(manifest.Tracks, subtitle.KindAudio, "aac")
	if track == nil {
		t.Fatal("缺少测试音轨")
	}
	cases := []struct {
		name, body string
	}{
		{name: "非法JSON", body: `{"track_id":`},
		{name: "空轨道", body: `{"track_id":" "}`},
		{name: "额外profile", body: `{"track_id":"` + track.ID + `","profile_id":"forged"}`},
		{name: "额外index", body: `{"track_id":"` + track.ID + `","stream_index":99}`},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			response := doJSON(t, fixture.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), item.body)
			assertAudioReloadError(t, response, http.StatusBadRequest, "INVALID_AUDIO_RELOAD_REQUEST")
		})
	}
	response := doJSON(t, fixture.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), `{"track_id":"`+track.ID+`"}`)
	assertAudioReloadError(t, response, http.StatusServiceUnavailable, "HLS_PREVIEW_UNAVAILABLE")
}

func TestCreateAudioReloadUsesStableTrackAndSpace(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	createAudioReloadMetadata(t, fixture, []map[string]any{{"index": 1, "codec_name": "aac", "default": true}})
	reload := setupAudioReloadFixture(t, fixture, false)
	uploaded := models.MediaSubtitleTrack{
		ID: "upl-existing", SpaceID: fixture.media.SpaceID, MediaID: fixture.media.ID,
		Source: subtitle.SourceUploaded, SourceRef: "existing.vtt",
		StorageRelativePath: filepath.ToSlash(filepath.Join("subtitles", fixture.media.SpaceID, strconv.FormatInt(fixture.media.ID, 10), "upl-existing.vtt")),
		StreamIndex:         -1, Format: "vtt",
	}
	if err := fixture.db.Create(&uploaded).Error; err != nil {
		t.Fatalf("创建现有非音轨失败: %v", err)
	}
	response := doJSON(t, reload.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), `{"track_id":"`+uploaded.ID+`"}`)
	assertAudioReloadError(t, response, http.StatusUnprocessableEntity, "AUDIO_RELOAD_UNSUPPORTED")
	response = doJSON(t, reload.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), `{"track_id":"emb-missing"}`)
	assertAudioReloadError(t, response, http.StatusNotFound, "NOT_FOUND")

	request := doJSONRequest(t, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), `{"track_id":"emb-missing"}`)
	request.Header.Set(spaceHeader, "space-other")
	response = serveRequest(reload.router, request)
	assertAudioReloadError(t, response, http.StatusNotFound, "NOT_FOUND")
}

func TestCreateAudioReloadRejectsUnsupportedTrack(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	fixture.media.FilePath = "smb://server/share/movie.mkv"
	if err := fixture.db.Save(&fixture.media).Error; err != nil {
		t.Fatalf("更新 SMB 媒体失败: %v", err)
	}
	createAudioReloadMetadata(t, fixture, []map[string]any{{"index": 1, "codec_name": "aac", "default": true}})
	reload := setupAudioReloadFixture(t, fixture, false)
	manifest := getTrackManifest(t, reload.router, fixture.media.ID)
	track := findAPITrack(manifest.Tracks, subtitle.KindAudio, "aac")
	response := doJSON(t, reload.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), `{"track_id":"`+track.ID+`"}`)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"AUDIO_RELOAD_UNSUPPORTED"`) || !strings.Contains(response.Body.String(), subtitle.ReasonSMBAudioReloadUnsupported) {
		t.Fatalf("不支持音轨响应错误: code=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateAudioReloadReturnsIdempotentTaskAndServerDerivedPayload(t *testing.T) {
	if !transcoder.IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过 audio-reload 成功路径测试")
	}
	fixture := setupSubtitleTrackAPI(t)
	fixture.media.Width = 1920
	fixture.media.Height = 1080
	fixture.media.FileSize = 999
	fixture.media.ModifiedAt = time.Unix(1, 0)
	if err := fixture.db.Save(&fixture.media).Error; err != nil {
		t.Fatalf("更新媒体尺寸失败: %v", err)
	}
	createAudioReloadMetadata(t, fixture, []map[string]any{
		{"index": 1, "codec_name": "aac", "language": "zh", "default": true},
		{"index": 4, "codec_name": "aac", "language": "ja"},
	})
	reload := setupAudioReloadFixture(t, fixture, false)
	manifest := getTrackManifest(t, reload.router, fixture.media.ID)
	track := findAudioTrackByLanguage(manifest.Tracks, "ja")
	body := `{"track_id":"` + track.ID + `"}`
	first := doJSON(t, reload.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), body)
	second := doJSON(t, reload.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), body)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("audio-reload 应返回 202: first=%d/%s second=%d/%s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	var firstResult, secondResult struct {
		TaskID           string `json:"task_id"`
		ProfileID        string `json:"profile_id"`
		URL              string `json:"url"`
		RequestedTrackID string `json:"requested_track_id"`
		SpaceID          string `json:"space_id"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &firstResult)
	_ = json.Unmarshal(second.Body.Bytes(), &secondResult)
	expectedProfile := transcoder.AudioReloadProfileID(track.ID)
	if firstResult.TaskID == "" || secondResult.TaskID != firstResult.TaskID || firstResult.ProfileID != expectedProfile || firstResult.RequestedTrackID != track.ID || firstResult.SpaceID != fixture.media.SpaceID {
		t.Fatalf("audio-reload 响应或幂等性错误: first=%+v second=%+v", firstResult, secondResult)
	}
	expectedURL := "/api/play/hls/" + strconv.FormatInt(fixture.media.ID, 10) + "/profiles/" + expectedProfile + "/tasks/" + firstResult.TaskID + "/master.m3u8"
	if firstResult.URL != expectedURL {
		t.Fatalf("audio-reload URL 错误: %s", firstResult.URL)
	}
	taskID, err := strconv.ParseInt(firstResult.TaskID, 10, 64)
	if err != nil {
		t.Fatalf("task_id 必须为十进制字符串: %v", err)
	}
	task, err := reload.tasks.Get(context.Background(), taskID, tasksvc.Query{SpaceID: fixture.media.SpaceID})
	if err != nil {
		t.Fatalf("读取 audio-reload 任务失败: %v", err)
	}
	var payload transcoder.HLSPreviewPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		t.Fatalf("解析 audio-reload payload 失败: %v", err)
	}
	if payload.AudioTrackID != track.ID || payload.AudioStreamIndex == nil || *payload.AudioStreamIndex != 4 || payload.Width != 1920 || payload.Height != 1080 {
		t.Fatalf("audio-reload 未使用服务端轨道与媒体字段: %+v", payload)
	}
	fileInfo, err := os.Stat(fixture.media.FilePath)
	if err != nil {
		t.Fatalf("读取真实音轨源身份失败: %v", err)
	}
	expectedFingerprint := transcoder.AudioReloadSourceFingerprint(transcoder.MediaIdentity{
		SpaceID: fixture.media.SpaceID, MediaID: fixture.media.ID, Path: fixture.media.FilePath,
		Size: fileInfo.Size(), ModifiedAt: fileInfo.ModTime(), ContentHash: fixture.media.ContentHash, ContentHashStale: fixture.media.ContentHashStale,
	}, transcoder.AudioTrackIdentity{
		ID: track.ID, Index: *track.StreamIndex, Codec: track.Codec, Language: track.Language,
		Title: track.Title, Channels: track.Channels, ChannelLayout: track.ChannelLayout,
	})
	if payload.SourceFingerprint != expectedFingerprint {
		t.Fatalf("audio-reload 必须以真实文件身份入队: got=%s want=%s", payload.SourceFingerprint, expectedFingerprint)
	}
}

func TestCreateAudioReloadEnqueueFailureReturns500(t *testing.T) {
	if !transcoder.IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过 audio-reload 入队失败测试")
	}
	fixture := setupSubtitleTrackAPI(t)
	createAudioReloadMetadata(t, fixture, []map[string]any{
		{"index": 1, "codec_name": "aac", "language": "zh", "default": true},
		{"index": 2, "codec_name": "aac", "language": "ja"},
	})
	manifest := getTrackManifest(t, fixture.router, fixture.media.ID)
	track := findAudioTrackByLanguage(manifest.Tracks, "ja")
	taskDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "closed-tasks.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开独立任务库失败: %v", err)
	}
	if err := taskDB.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移独立任务库失败: %v", err)
	}
	tasks := tasksvc.NewService(taskDB)
	workers := tasksvc.NewWorkerRegistry(tasks)
	preview := transcoder.NewHLSPreviewService(tasks, workers, filepath.Join(t.TempDir(), "hls"), func(context.Context, int64, transcoder.HLSPreviewPayload) error { return nil })
	sqlDB, err := taskDB.DB()
	if err != nil {
		t.Fatalf("读取独立任务库连接失败: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关闭独立任务库失败: %v", err)
	}
	router := gin.New()
	RegisterRoutes(router, NewHandler(library.NewService(fixture.db)).WithSubtitle(fixture.service).WithHLSPreview(preview))
	response := doJSON(t, router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/audio-reload"), `{"track_id":"`+track.ID+`"}`)
	assertAudioReloadError(t, response, http.StatusInternalServerError, "AUDIO_RELOAD_ENQUEUE_FAILED")
}

func setupAudioReloadFixture(t *testing.T, fixture subtitleAPIFixture, wakeWorkers bool) audioReloadFixture {
	t.Helper()
	if err := fixture.db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移 audio-reload 任务表失败: %v", err)
	}
	tasks := tasksvc.NewService(fixture.db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	preview := transcoder.NewHLSPreviewService(tasks, workers, filepath.Join(t.TempDir(), "hls"), func(context.Context, int64, transcoder.HLSPreviewPayload) error { return nil })
	handler := NewHandler(library.NewService(fixture.db)).WithSubtitle(fixture.service).WithHLSPreview(preview).WithTasks(tasks)
	if wakeWorkers {
		if err := preview.RegisterWorker(); err != nil {
			t.Fatalf("注册 audio-reload worker 失败: %v", err)
		}
		handler.WithTaskWorkers(workers)
	}
	router := gin.New()
	RegisterRoutes(router, handler)
	return audioReloadFixture{router: router, tasks: tasks, workers: workers}
}

func createAudioReloadMetadata(t *testing.T, fixture subtitleAPIFixture, audioStreams []map[string]any) {
	t.Helper()
	if err := fixture.db.Where("space_id = ? AND media_id = ?", fixture.media.SpaceID, fixture.media.ID).Delete(&models.MediaMetadata{}).Error; err != nil {
		t.Fatalf("清理音轨元数据失败: %v", err)
	}
	encoded, err := json.Marshal(map[string]any{"audio_streams": audioStreams})
	if err != nil {
		t.Fatalf("编码音轨元数据失败: %v", err)
	}
	metadata := models.MediaMetadata{
		MediaID: fixture.media.ID, SpaceID: fixture.media.SpaceID, Source: "ffprobe", Tool: "ffprobe", ToolVersion: "7",
		RawJSON: `{}`, NormalizedJSON: string(encoded), ParsedAt: time.Now(), Stale: false,
	}
	if err := fixture.db.Create(&metadata).Error; err != nil {
		t.Fatalf("创建音轨元数据失败: %v", err)
	}
}

func getTrackManifest(t *testing.T, router *gin.Engine, mediaID int64) subtitle.ListResponse {
	t.Helper()
	response := doJSON(t, router, http.MethodGet, subtitleAPIPath(mediaID, "/tracks"), "")
	if response.Code != http.StatusOK {
		t.Fatalf("读取轨道清单失败: code=%d body=%s", response.Code, response.Body.String())
	}
	var manifest subtitle.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("解析轨道清单失败: %v", err)
	}
	return manifest
}

func assertAudioTrackReason(t *testing.T, response *httptest.ResponseRecorder, reason string) {
	t.Helper()
	var manifest subtitle.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("解析降级轨道清单失败: %v", err)
	}
	track := findAPITrack(manifest.Tracks, subtitle.KindAudio, "aac")
	if track == nil || track.Capability != subtitle.CapabilityUnsupported || track.UnsupportedReason != reason {
		t.Fatalf("音轨降级原因错误: %#v", track)
	}
}

func assertAudioReloadError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	if response.Code != status || body.Code != code {
		t.Fatalf("audio-reload 错误响应不符: want=%d/%s got=%d/%s", status, code, response.Code, body.Code)
	}
}

func findAudioTrackByLanguage(tracks []subtitle.Track, language string) *subtitle.Track {
	for index := range tracks {
		if tracks[index].Kind == subtitle.KindAudio && tracks[index].Language == language {
			return &tracks[index]
		}
	}
	return nil
}

func audioTrackWithoutIndex(track subtitle.Track) subtitle.Track {
	track.StreamIndex = nil
	return track
}

func audioTrackWithIndex(track subtitle.Track, index int) subtitle.Track {
	track.StreamIndex = &index
	return track
}

func audioTrackWithSource(track subtitle.Track, source string) subtitle.Track {
	track.Source = source
	return track
}

func doJSONRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("创建 JSON 请求失败: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return request
}

func serveRequest(router *gin.Engine, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
