package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// setupPlayTestRouter 创建带播放路由的测试路由器。
func setupPlayTestRouter(t *testing.T) (*gin.Engine, *library.Service, *playback.Service) {
	t.Helper()
	db := setupTestDB(t)
	libSvc := library.NewService(db)
	pbSvc := playback.NewService()
	h := NewHandler(libSvc)

	r := gin.New()
	RegisterRoutes(r, h, pbSvc)
	return r, libSvc, pbSvc
}

// TestHLSRoute_RejectsMediaOutsideRequestedSpace 验证 HLS 直出不会跨 Space 读取媒体。
func TestHLSRoute_RejectsMediaOutsideRequestedSpace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	defaultMedia := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/default.mp4", FileName: "default.mp4"}
	otherMedia := models.MediaFile{SpaceID: "space-other", LibraryID: 2, FilePath: "D:/other.mp4", FileName: "other.mp4"}
	if err := db.Create(&defaultMedia).Error; err != nil {
		t.Fatalf("创建默认 Space 媒体失败: %v", err)
	}
	if err := db.Create(&otherMedia).Error; err != nil {
		t.Fatalf("创建其他 Space 媒体失败: %v", err)
	}

	hlsMgr := player.NewHLSManager(t.TempDir())
	if err := hlsMgr.SaveMasterM3U8(defaultMedia.ID, "#EXTM3U\n"); err != nil {
		t.Fatalf("创建 HLS master 失败: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("space_id", c.GetHeader("X-JianVideo-Space-Id"))
		c.Next()
	})
	RegisterHLSRoutes(router, hlsMgr, t.TempDir(), library.NewService(db), nil)

	deniedReq := httptest.NewRequest(http.MethodGet, "/api/play/hls/"+strconv.FormatInt(defaultMedia.ID, 10)+"/master.m3u8", nil)
	deniedReq.Header.Set("X-JianVideo-Space-Id", "space-other")
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, deniedReq)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("其他 Space 不得读取默认 Space HLS, 期望 404, 实际 %d", denied.Code)
	}

	allowedReq := httptest.NewRequest(http.MethodGet, "/api/play/hls/"+strconv.FormatInt(defaultMedia.ID, 10)+"/master.m3u8", nil)
	allowedReq.Header.Set("X-JianVideo-Space-Id", models.DefaultSpaceID)
	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, allowedReq)
	if allowed.Code != http.StatusOK || allowed.Body.String() != "#EXTM3U\n" {
		t.Fatalf("默认 Space 应可读取自身 HLS, code=%d body=%q", allowed.Code, allowed.Body.String())
	}
}

// TestStreamHandler_InvalidID 测试无效的媒体 ID 格式。
func TestHLSRoute_DefaultProfileAndLegacyFilesRemainCompatible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/video.mp4", FileName: "video.mp4"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	root := t.TempDir()
	profileDir := filepath.Join(root, models.DefaultSpaceID, strconv.FormatInt(media.ID, 10), "h264")
	if err := os.MkdirAll(profileDir, 0o750); err != nil {
		t.Fatalf("创建默认 profile 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "master.m3u8"), []byte("#EXTM3U\n720p.m3u8\n"), 0o640); err != nil {
		t.Fatalf("写默认 profile master 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "720p.m3u8"), []byte("#EXTM3U\n"), 0o640); err != nil {
		t.Fatalf("写默认 profile variant 失败: %v", err)
	}
	legacyDir := filepath.Join(root, strconv.FormatInt(media.ID, 10))
	if err := os.MkdirAll(legacyDir, 0o750); err != nil {
		t.Fatalf("创建旧 HLS 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "index.m3u8"), []byte("#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n"), 0o640); err != nil {
		t.Fatalf("写旧 fMP4 清单失败: %v", err)
	}

	router := gin.New()
	RegisterHLSRoutes(router, player.NewHLSManager(root), root, library.NewService(db), nil)
	for _, test := range []struct {
		path string
		body string
	}{
		{path: "/api/play/hls/" + strconv.FormatInt(media.ID, 10) + "/master", body: "720p.m3u8"},
		{path: "/api/play/hls/" + strconv.FormatInt(media.ID, 10) + "/master.m3u8", body: "720p.m3u8"},
		{path: "/api/play/hls/" + strconv.FormatInt(media.ID, 10) + "/720p.m3u8", body: "#EXTM3U"},
		{path: "/api/play/hls/" + strconv.FormatInt(media.ID, 10) + "/index.m3u8", body: "#EXT-X-MAP"},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK || !bytes.Contains(resp.Body.Bytes(), []byte(test.body)) {
			t.Fatalf("HLS 兼容路径失败: path=%s code=%d body=%q", test.path, resp.Code, resp.Body.String())
		}
	}
}

func TestHLSRoute_AudioReloadRequiresVerifiedSucceededTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/audio.mp4", FileName: "audio.mp4"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建音轨 HLS 媒体失败: %v", err)
	}
	root := t.TempDir()
	trackID := "audio-track"
	profileID := transcoder.AudioReloadProfileID(trackID)
	validPayload := transcoder.HLSPreviewPayload{
		SpaceID: models.DefaultSpaceID, MediaID: media.ID, ProfileID: profileID, Codec: transcoder.DefaultTargetCodec,
		AudioTrackID: trackID, AudioStreamIndex: hlsTestIntPointer(2), SourceFingerprint: "source-fingerprint", ForceRebuild: true,
	}
	spaceOther := "space-other"
	tests := []struct {
		name         string
		taskID       int64
		status       string
		spaceID      *string
		taskType     string
		resource     string
		resourceID   string
		payload      transcoder.HLSPreviewPayload
		missingAsset bool
		want         int
	}{
		{name: "目录存在但任务不存在", taskID: 70, want: http.StatusNotFound},
		{name: "任务属于其他 Space", taskID: 71, status: models.TaskStatusSucceeded, spaceID: &spaceOther, taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: withHLSRoutePayload(validPayload, func(payload *transcoder.HLSPreviewPayload) { payload.SpaceID = spaceOther }), want: http.StatusNotFound},
		{name: "任务绑定其他媒体", taskID: 72, status: models.TaskStatusSucceeded, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: "999", payload: withHLSRoutePayload(validPayload, func(payload *transcoder.HLSPreviewPayload) { payload.MediaID = 999 }), want: http.StatusNotFound},
		{name: "任务资源类型错误", taskID: 73, status: models.TaskStatusSucceeded, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "library", resourceID: strconv.FormatInt(media.ID, 10), payload: validPayload, want: http.StatusNotFound},
		{name: "任务类型错误", taskID: 74, status: models.TaskStatusSucceeded, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: "transcode.hls.abr", resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: validPayload, want: http.StatusNotFound},
		{name: "任务 profile 不匹配", taskID: 75, status: models.TaskStatusSucceeded, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: withHLSRoutePayload(validPayload, func(payload *transcoder.HLSPreviewPayload) {
			payload.ProfileID = transcoder.AudioReloadProfileID("other-track")
		}), want: http.StatusNotFound},
		{name: "任务音轨绑定不匹配", taskID: 76, status: models.TaskStatusSucceeded, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: withHLSRoutePayload(validPayload, func(payload *transcoder.HLSPreviewPayload) { payload.AudioTrackID = "other-track" }), want: http.StatusNotFound},
		{name: "任务 payload 媒体不匹配", taskID: 83, status: models.TaskStatusSucceeded, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: withHLSRoutePayload(validPayload, func(payload *transcoder.HLSPreviewPayload) { payload.MediaID = 999 }), want: http.StatusNotFound},
		{name: "任务 payload 音轨指纹缺失", taskID: 84, status: models.TaskStatusSucceeded, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: withHLSRoutePayload(validPayload, func(payload *transcoder.HLSPreviewPayload) { payload.SourceFingerprint = "" }), want: http.StatusNotFound},
		{name: "任务等待中", taskID: 77, status: models.TaskStatusPending, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: validPayload, want: http.StatusNotFound},
		{name: "任务执行中", taskID: 78, status: models.TaskStatusRunning, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: validPayload, want: http.StatusNotFound},
		{name: "任务失败", taskID: 79, status: models.TaskStatusFailed, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: validPayload, want: http.StatusNotFound},
		{name: "任务已取消", taskID: 80, status: models.TaskStatusCanceled, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: validPayload, want: http.StatusNotFound},
		{name: "成功任务但资产不存在", taskID: 82, status: models.TaskStatusSucceeded, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: validPayload, missingAsset: true, want: http.StatusNotFound},
		{name: "成功任务允许读取", taskID: 81, status: models.TaskStatusSucceeded, spaceID: hlsTestStringPointer(models.DefaultSpaceID), taskType: transcoder.TaskTypeHLSPreview, resource: "media", resourceID: strconv.FormatInt(media.ID, 10), payload: validPayload, want: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !test.missingAsset {
				mustWriteAudioHLSRouteAsset(t, root, media.ID, profileID, test.taskID, "master.m3u8", "#EXTM3U\ncanonical\n")
			}
			if test.spaceID != nil {
				mustCreateHLSRouteTask(t, db, test.taskID, test.status, test.spaceID, test.taskType, test.resource, test.resourceID, test.payload)
			}
			router := gin.New()
			RegisterHLSRoutes(router, player.NewHLSManager(root), root, library.NewService(db), tasksvc.NewService(db))
			request := httptest.NewRequest(http.MethodGet, hlsTaskRoute(media.ID, profileID, test.taskID, "master.m3u8"), nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("任务校验状态不符: code=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}

func TestHLSRoute_AudioReloadRequiresCanonicalTaskPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/audio.mp4", FileName: "audio.mp4"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建音轨 HLS 媒体失败: %v", err)
	}
	trackID := "audio-track"
	profileID := transcoder.AudioReloadProfileID(trackID)
	payload := transcoder.HLSPreviewPayload{
		SpaceID: models.DefaultSpaceID, MediaID: media.ID, ProfileID: profileID, Codec: transcoder.DefaultTargetCodec,
		AudioTrackID: trackID, AudioStreamIndex: hlsTestIntPointer(2), SourceFingerprint: "source-fingerprint", ForceRebuild: true,
	}
	root := t.TempDir()
	mustCreateHLSRouteTask(t, db, 90, models.TaskStatusSucceeded, hlsTestStringPointer(models.DefaultSpaceID), transcoder.TaskTypeHLSPreview, "media", strconv.FormatInt(media.ID, 10), payload)
	mustWriteAudioHLSRouteAsset(t, root, media.ID, profileID, 90, "master.m3u8", "#EXTM3U\n")
	router := gin.New()
	RegisterHLSRoutes(router, player.NewHLSManager(root), root, library.NewService(db), tasksvc.NewService(db))
	base := "/api/play/hls/" + strconv.FormatInt(media.ID, 10) + "/profiles/"
	for _, path := range []string{
		base + profileID + "/master.m3u8",
		base + strings.ToUpper(profileID) + "/tasks/90/master.m3u8",
		base + profileID + "/tasks/090/master.m3u8",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("非规范音轨 HLS 路径必须拒绝: path=%s code=%d body=%q", path, response.Code, response.Body.String())
		}
	}
}

func TestHLSRoute_AudioReloadPreservesRangeAndContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/audio.mp4", FileName: "audio.mp4"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建音轨 HLS 媒体失败: %v", err)
	}
	trackID := "audio-track"
	profileID := transcoder.AudioReloadProfileID(trackID)
	payload := transcoder.HLSPreviewPayload{
		SpaceID: models.DefaultSpaceID, MediaID: media.ID, ProfileID: profileID, Codec: transcoder.DefaultTargetCodec,
		AudioTrackID: trackID, AudioStreamIndex: hlsTestIntPointer(2), SourceFingerprint: "source-fingerprint", ForceRebuild: true,
	}
	root := t.TempDir()
	mustCreateHLSRouteTask(t, db, 91, models.TaskStatusSucceeded, hlsTestStringPointer(models.DefaultSpaceID), transcoder.TaskTypeHLSPreview, "media", strconv.FormatInt(media.ID, 10), payload)
	mustWriteAudioHLSRouteAsset(t, root, media.ID, profileID, 91, "segment.ts", "0123456789")

	router := gin.New()
	RegisterHLSRoutes(router, player.NewHLSManager(root), root, library.NewService(db), tasksvc.NewService(db))
	request := httptest.NewRequest(http.MethodGet, hlsTaskRoute(media.ID, profileID, 91, "segment.ts"), nil)
	request.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "2345" {
		t.Fatalf("Range 响应不符: code=%d body=%q", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "video/mp2t" {
		t.Fatalf("HLS Content-Type 不符: %q", contentType)
	}
}

func TestHLSRoute_AudioReloadRejectsSymlinkEscape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/audio.mp4", FileName: "audio.mp4"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建音轨 HLS 媒体失败: %v", err)
	}
	trackID := "audio-track"
	profileID := transcoder.AudioReloadProfileID(trackID)
	payload := transcoder.HLSPreviewPayload{
		SpaceID: models.DefaultSpaceID, MediaID: media.ID, ProfileID: profileID, Codec: transcoder.DefaultTargetCodec,
		AudioTrackID: trackID, AudioStreamIndex: hlsTestIntPointer(2), SourceFingerprint: "source-fingerprint", ForceRebuild: true,
	}
	root := t.TempDir()
	mustCreateHLSRouteTask(t, db, 92, models.TaskStatusSucceeded, hlsTestStringPointer(models.DefaultSpaceID), transcoder.TaskTypeHLSPreview, "media", strconv.FormatInt(media.ID, 10), payload)
	outside := filepath.Join(t.TempDir(), "secret.ts")
	mustWriteHLSRouteFile(t, outside, "secret")
	link := filepath.Join(root, models.DefaultSpaceID, strconv.FormatInt(media.ID, 10), profileID, "tasks", "92", "segment.ts")
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatalf("创建 HLS 目录失败: %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境无法创建符号链接: %v", err)
	}

	router := gin.New()
	RegisterHLSRoutes(router, player.NewHLSManager(root), root, library.NewService(db), tasksvc.NewService(db))
	request := httptest.NewRequest(http.MethodGet, hlsTaskRoute(media.ID, profileID, 92, "segment.ts"), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("越界符号链接必须被拒绝: code=%d body=%q", response.Code, response.Body.String())
	}
}

func TestHLSRoute_LegacyMasterFailureIsControlled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/video.mp4", FileName: "video.mp4"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	root := t.TempDir()
	router := gin.New()
	RegisterHLSRoutes(router, player.NewHLSManager(root), root, library.NewService(db), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/play/hls/"+strconv.FormatInt(media.ID, 10)+"/master.m3u8", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || response.Body.String() != `{"code":"NOT_FOUND","message":"HLS 主清单不存在"}` {
		t.Fatalf("legacy master 错误响应不受控: code=%d body=%q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), root) || strings.Contains(response.Body.String(), "open ") {
		t.Fatalf("legacy master 错误泄露底层信息: %q", response.Body.String())
	}
}

func mustCreateHLSRouteTask(t *testing.T, db *gorm.DB, taskID int64, status string, spaceID *string, taskType, resourceType, resourceID string, payload transcoder.HLSPreviewPayload) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("编码 HLS 测试任务失败: %v", err)
	}
	task := models.Task{
		ID: taskID, Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: taskType, Status: status,
		MaxAttempts: 1, PayloadJSON: string(data), ResourceType: resourceType, ResourceID: resourceID,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("创建 HLS 测试任务失败: %v", err)
	}
}

func mustWriteAudioHLSRouteAsset(t *testing.T, root string, mediaID int64, profileID string, taskID int64, name, content string) {
	t.Helper()
	path := filepath.Join(root, models.DefaultSpaceID, strconv.FormatInt(mediaID, 10), profileID, "tasks", strconv.FormatInt(taskID, 10), name)
	mustWriteHLSRouteFile(t, path, content)
}

func hlsTaskRoute(mediaID int64, profileID string, taskID int64, name string) string {
	return "/api/play/hls/" + strconv.FormatInt(mediaID, 10) + "/profiles/" + profileID + "/tasks/" + strconv.FormatInt(taskID, 10) + "/" + name
}

func withHLSRoutePayload(payload transcoder.HLSPreviewPayload, mutate func(*transcoder.HLSPreviewPayload)) transcoder.HLSPreviewPayload {
	mutate(&payload)
	return payload
}

func hlsTestStringPointer(value string) *string {
	return &value
}

func hlsTestIntPointer(value int) *int {
	return &value
}

func mustWriteHLSRouteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("创建 HLS 路径失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatalf("写入 HLS 路由文件失败: %v", err)
	}
}

func TestStreamHandler_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	req := httptest.NewRequest("GET", "/api/play/invalid/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 无效 ID 格式应返回 400
	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

// TestGetProgress 测试获取播放进度。
func TestGetProgress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, pbSvc := setupPlayTestRouter(t)

	pbSvc.HandleBufferReport(1, playback.BufferReport{
		CurrentPosition: 10.0,
		FileSize:        4096,
		BufferedRanges:  [][2]int64{{0, 4096}},
	})

	req := httptest.NewRequest("GET", "/api/play/1/progress", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 已上报进度，应返回 200
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}
}

// TestReportBuffer 测试缓冲区间上报。
func TestReportBuffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	body, _ := json.Marshal(map[string]interface{}{
		"current_position": 15.0,
		"file_size":        4096,
		"buffered_ranges":  [][]int{{0, 2048}, {2048, 4096}},
	})
	req := httptest.NewRequest("POST", "/api/play/1/buffer", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

// TestStreamFileNotFound 测试媒体文件不存在时返回 404。
func TestStreamFileNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	req := httptest.NewRequest("GET", "/api/play/9999/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

// TestStreamInvalidIDFormat 测试无效 ID 格式。
func TestStreamInvalidIDFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	req := httptest.NewRequest("GET", "/api/play/abc/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

// TestSeekInvalidJSON 测试无效 JSON。
func TestSeekInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	body := `invalid json`
	req := httptest.NewRequest("POST", "/api/play/1/seek", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}
