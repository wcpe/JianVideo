package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

type fakeTimelinePreviewGateway struct {
	status       TimelinePreviewStatus
	enqueued     TimelinePreviewStatus
	rebuilt      TimelinePreviewStatus
	resource     TimelinePreviewResource
	statusCalls  []TimelinePreviewIdentity
	enqueueCalls []TimelinePreviewIdentity
	rebuildCalls []TimelinePreviewIdentity
	openCalls    []TimelinePreviewResourceIdentity
}

func (f *fakeTimelinePreviewGateway) Status(_ context.Context, identity TimelinePreviewIdentity) (TimelinePreviewStatus, error) {
	f.statusCalls = append(f.statusCalls, identity)
	return f.status, nil
}

func (f *fakeTimelinePreviewGateway) Enqueue(_ context.Context, identity TimelinePreviewIdentity) (TimelinePreviewStatus, error) {
	f.enqueueCalls = append(f.enqueueCalls, identity)
	return f.enqueued, nil
}

func (f *fakeTimelinePreviewGateway) Rebuild(_ context.Context, identity TimelinePreviewIdentity) (TimelinePreviewStatus, error) {
	f.rebuildCalls = append(f.rebuildCalls, identity)
	return f.rebuilt, nil
}

func (f *fakeTimelinePreviewGateway) OpenResource(_ context.Context, identity TimelinePreviewResourceIdentity) (TimelinePreviewResource, error) {
	f.openCalls = append(f.openCalls, identity)
	return f.resource, nil
}

func setupTimelinePreviewRouter(t *testing.T, gateway TimelinePreviewGateway) (*gin.Engine, int64) {
	t.Helper()
	_, libraryService, _ := setupSpaceTestRouter(t)
	_, media := createSpaceMedia(t, libraryService, models.DefaultSpaceID, t.TempDir(), t.TempDir()+"/video.mp4")
	router := gin.New()
	RegisterRoutes(router, NewHandler(libraryService).WithTimelinePreview(gateway))
	return router, media.ID
}

func TestTimelinePreview缺失时幂等入队并返回202(t *testing.T) {
	gateway := &fakeTimelinePreviewGateway{
		enqueued: TimelinePreviewStatus{ProfileID: "desktop", Duration: 20, Version: 1, TaskID: 42, State: TimelinePreviewPending},
	}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)

	response := getJSON(t, router, timelineStatusURL(mediaID, "desktop"), models.DefaultSpaceID)

	if response.Code != http.StatusAccepted {
		t.Fatalf("期望 202，实际 %d: %s", response.Code, response.Body.String())
	}
	assertJSONField(t, response.Body.Bytes(), "task_id", float64(42))
	assertJSONField(t, response.Body.Bytes(), "duration", float64(20))
	assertJSONField(t, response.Body.Bytes(), "version", float64(1))
	if len(gateway.statusCalls) != 1 || len(gateway.enqueueCalls) != 1 {
		t.Fatalf("缺失预览应先查状态再入队: status=%v enqueue=%v", gateway.statusCalls, gateway.enqueueCalls)
	}
}

func TestTimelinePreview查询缺省Profile返回202及实际Profile(t *testing.T) {
	gateway := &fakeTimelinePreviewGateway{
		enqueued: TimelinePreviewStatus{ProfileID: "timeline-default", TaskID: 42, State: TimelinePreviewPending},
	}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)

	response := getJSON(t, router, timelineStatusURLWithoutProfile(mediaID), models.DefaultSpaceID)

	if response.Code != http.StatusAccepted {
		t.Fatalf("期望 202，实际 %d: %s", response.Code, response.Body.String())
	}
	assertJSONField(t, response.Body.Bytes(), "profile_id", "timeline-default")
	if len(gateway.statusCalls) != 1 || gateway.statusCalls[0].ProfileID != "" {
		t.Fatalf("缺省 profile 应原样传给 gateway: %+v", gateway.statusCalls)
	}
}

func TestTimelinePreview查询缺省Profile可返回Available(t *testing.T) {
	status := availableTimelineStatus()
	status.ProfileID = "timeline-default"
	gateway := &fakeTimelinePreviewGateway{status: status}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)

	response := getJSON(t, router, timelineStatusURLWithoutProfile(mediaID), models.DefaultSpaceID)

	if response.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", response.Code, response.Body.String())
	}
	assertJSONField(t, response.Body.Bytes(), "profile_id", "timeline-default")
	if len(gateway.statusCalls) != 1 || gateway.statusCalls[0].ProfileID != "" {
		t.Fatalf("缺省 profile 应原样传给 gateway: %+v", gateway.statusCalls)
	}
}

func TestTimelinePreview查询非空非法Profile仍返回400(t *testing.T) {
	gateway := &fakeTimelinePreviewGateway{}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)

	response := getJSON(t, router, timelineStatusURL(mediaID, "bad%20profile"), models.DefaultSpaceID)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d: %s", response.Code, response.Body.String())
	}
	if len(gateway.statusCalls) != 0 {
		t.Fatalf("非法 profile 不得传给 gateway: %+v", gateway.statusCalls)
	}
}

func TestTimelinePreview可用时返回受控资源URL(t *testing.T) {
	gateway := &fakeTimelinePreviewGateway{status: availableTimelineStatus()}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)

	response := getJSON(t, router, timelineStatusURL(mediaID, "desktop"), models.DefaultSpaceID)

	if response.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	vttURL, _ := body["vtt_url"].(string)
	if !strings.HasPrefix(vttURL, "/api/play/") || strings.Contains(vttURL, `:\`) {
		t.Fatalf("不得暴露绝对路径，实际 vtt_url=%q", vttURL)
	}
	assertJSONField(t, response.Body.Bytes(), "duration", float64(20))
	assertJSONField(t, response.Body.Bytes(), "version", float64(1))
	sprites, ok := body["sprite_urls"].(map[string]any)
	if !ok || len(sprites) != 2 {
		t.Fatalf("应返回 sprite 名称到受控 URL 的映射: %#v", body["sprite_urls"])
	}
	for name, value := range sprites {
		url, _ := value.(string)
		if !strings.HasPrefix(url, "/api/play/") || strings.Contains(url, `:\`) || !strings.HasSuffix(url, name) {
			t.Fatalf("sprite 映射不得暴露绝对路径且须保留名称: %q=%q", name, url)
		}
	}
	if len(gateway.enqueueCalls) != 0 {
		t.Fatalf("已有可用预览时不得重复入队")
	}
}

func TestTimelinePreviewRebuild仅入队新代次(t *testing.T) {
	gateway := &fakeTimelinePreviewGateway{
		rebuilt: TimelinePreviewStatus{GenerationID: "generation-new", ProfileID: "desktop", TaskID: 77, State: TimelinePreviewPending},
	}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)
	body := `{"profile_id":"desktop"}`

	response := requestWithSpace(t, router, http.MethodPost, timelineRebuildURL(mediaID), models.DefaultSpaceID, body)

	if response.Code != http.StatusAccepted {
		t.Fatalf("期望 202，实际 %d: %s", response.Code, response.Body.String())
	}
	assertJSONField(t, response.Body.Bytes(), "generation_id", "generation-new")
	if len(gateway.rebuildCalls) != 1 {
		t.Fatalf("重建必须且仅调用一次服务入队，实际 %d", len(gateway.rebuildCalls))
	}
}

func TestTimelinePreviewRebuild缺省Profile返回实际Profile(t *testing.T) {
	gateway := &fakeTimelinePreviewGateway{
		rebuilt: TimelinePreviewStatus{GenerationID: "generation-new", ProfileID: "timeline-default", TaskID: 77, State: TimelinePreviewPending},
	}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)

	response := requestWithSpace(t, router, http.MethodPost, timelineRebuildURL(mediaID), models.DefaultSpaceID, `{}`)

	if response.Code != http.StatusAccepted {
		t.Fatalf("期望 202，实际 %d: %s", response.Code, response.Body.String())
	}
	assertJSONField(t, response.Body.Bytes(), "profile_id", "timeline-default")
	if len(gateway.rebuildCalls) != 1 || gateway.rebuildCalls[0].ProfileID != "" {
		t.Fatalf("缺省 profile 应原样传给 gateway: %+v", gateway.rebuildCalls)
	}
}

func TestTimelinePreviewResource按完整身份流式返回(t *testing.T) {
	gateway := &fakeTimelinePreviewGateway{resource: TimelinePreviewResource{
		Body: io.NopCloser(bytes.NewBufferString("WEBVTT\n")), ContentType: "text/vtt; charset=utf-8", Size: 7,
	}}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)
	path := timelineResourceURL(mediaID, "desktop", "source-a", "generation-a", "index.vtt")

	response := getJSON(t, router, path, models.DefaultSpaceID)

	if response.Code != http.StatusOK || response.Body.String() != "WEBVTT\n" {
		t.Fatalf("资源响应不正确: code=%d body=%q", response.Code, response.Body.String())
	}
	if len(gateway.openCalls) != 1 || gateway.openCalls[0].GenerationID != "generation-a" {
		t.Fatalf("资源服务未收到完整身份: %+v", gateway.openCalls)
	}
}

func TestTimelinePreview严格隔离Space与媒体归属(t *testing.T) {
	gateway := &fakeTimelinePreviewGateway{status: availableTimelineStatus()}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)

	response := getJSON(t, router, timelineStatusURL(mediaID, "desktop"), "space-alt")

	if response.Code != http.StatusNotFound {
		t.Fatalf("跨 Space 媒体应返回 404，实际 %d", response.Code)
	}
	if len(gateway.statusCalls) != 0 {
		t.Fatalf("媒体归属校验失败时不得触碰预览服务")
	}
}

func TestTimelinePreviewResource拒绝遍历与非白名单文件(t *testing.T) {
	gateway := &fakeTimelinePreviewGateway{}
	router, mediaID := setupTimelinePreviewRouter(t, gateway)
	base := timelineResourceURL(mediaID, "desktop", "source-a", "generation-a", "")
	paths := []string{base + "%2e%2e%2fsecret", base + "sprite.svg", base + "nested%2fsprite.webp"}

	for _, path := range paths {
		response := getJSON(t, router, path, models.DefaultSpaceID)
		if response.Code != http.StatusBadRequest && response.Code != http.StatusNotFound {
			t.Fatalf("非法资源 %q 应在路由或 handler 层拒绝，实际 %d", path, response.Code)
		}
	}
	if len(gateway.openCalls) != 0 {
		t.Fatalf("非法资源不得触碰预览服务")
	}
}

func TestTimelinePreview真实Service经Adapter完成生成与资源读取(t *testing.T) {
	router, mediaID, workers, profileID := setupRealTimelinePreviewRouter(t)
	pending := getJSON(t, router, timelineStatusURL(mediaID, profileID), models.DefaultSpaceID)
	if pending.Code != http.StatusAccepted {
		t.Fatalf("首次查询应返回 202: %d %s", pending.Code, pending.Body.String())
	}
	assertJSONField(t, pending.Body.Bytes(), "duration", float64(20))
	assertJSONField(t, pending.Body.Bytes(), "version", float64(1))
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("运行时间轴预览 worker 失败: %v", err)
	}
	available := getJSON(t, router, timelineStatusURL(mediaID, profileID), models.DefaultSpaceID)
	status := decodeTimelineAvailableStatus(t, available)
	assertTimelineHTTPResource(t, router, status.VTTURL, "WEBVTT\n\n")
	assertTimelineHTTPResource(t, router, status.SpriteURLs["sprite-001.jpg"], "jpeg")
}

func setupRealTimelinePreviewRouter(t *testing.T) (*gin.Engine, int64, *tasksvc.WorkerRegistry, string) {
	t.Helper()
	_, libraryService, db := setupSpaceTestRouter(t)
	root, media := createRealTimelineMedia(t, libraryService, db)
	if err := db.AutoMigrate(&models.Task{}, &models.MediaTimelinePreview{}, &models.CacheAsset{}); err != nil {
		t.Fatalf("迁移时间轴预览测试表失败: %v", err)
	}
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	service := transcoder.NewTimelinePreviewService(db, tasks, workers, storage.NewService(db, root), root, apiTimelineGenerator{})
	if err := service.RegisterWorker(); err != nil {
		t.Fatalf("注册时间轴预览 worker 失败: %v", err)
	}
	router := gin.New()
	RegisterRoutes(router, NewHandler(libraryService).WithTimelinePreview(NewTimelinePreviewGateway(service)))
	return router, media.ID, workers, transcoder.DefaultTimelinePreviewProfile().ID
}

func createRealTimelineMedia(t *testing.T, libraryService *library.Service, db *gorm.DB) (string, models.MediaFile) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "video source.mp4")
	libraryDir := filepath.Join(root, "library")
	if err := os.MkdirAll(libraryDir, 0o750); err != nil {
		t.Fatalf("创建测试媒体库目录失败: %v", err)
	}
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatalf("写入测试媒体失败: %v", err)
	}
	_, media := createSpaceMedia(t, libraryService, models.DefaultSpaceID, libraryDir, source)
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Updates(map[string]any{
		"duration": 20, "file_state": models.MediaFileStateAvailable,
	}).Error; err != nil {
		t.Fatalf("更新测试媒体失败: %v", err)
	}
	return root, media
}

func decodeTimelineAvailableStatus(t *testing.T, response *httptest.ResponseRecorder) struct {
	VTTURL     string            `json:"vtt_url"`
	SpriteURLs map[string]string `json:"sprite_urls"`
} {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("生成后查询应返回 200: %d %s", response.Code, response.Body.String())
	}
	var status struct {
		VTTURL     string            `json:"vtt_url"`
		SpriteURLs map[string]string `json:"sprite_urls"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatalf("解析可用状态失败: %v", err)
	}
	spriteURL := status.SpriteURLs["sprite-001.jpg"]
	if len(status.SpriteURLs) != 1 || !strings.HasPrefix(status.VTTURL, "/api/play/") || !strings.HasPrefix(spriteURL, "/api/play/") {
		t.Fatalf("预览资源必须返回受控 URL: %+v", status)
	}
	return status
}

func TestTimelinePreviewAdapter映射领域错误(t *testing.T) {
	gateway := NewTimelinePreviewGateway(&transcoder.TimelinePreviewService{})
	_, err := gateway.Status(context.Background(), TimelinePreviewIdentity{MediaID: 0})
	if !errors.Is(err, ErrTimelinePreviewInvalid) {
		t.Fatalf("非法身份应映射为 API ErrInvalid: %v", err)
	}
	err = mapTimelinePreviewError(transcoder.ErrTimelinePreviewNotFound)
	if !errors.Is(err, ErrTimelinePreviewNotFound) {
		t.Fatalf("资源不存在应映射为 API ErrNotFound: %v", err)
	}
}

type apiTimelineGenerator struct{}

func (apiTimelineGenerator) Generate(_ context.Context, request transcoder.TimelinePreviewGenerateRequest) error {
	if err := os.MkdirAll(request.OutputDir, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(request.OutputDir, "index.vtt"), []byte("WEBVTT\n\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(request.OutputDir, "sprite-001.jpg"), []byte("jpeg"), 0o600)
}

func assertTimelineHTTPResource(t *testing.T, router *gin.Engine, url, expected string) {
	t.Helper()
	response := getJSON(t, router, url, models.DefaultSpaceID)
	if response.Code != http.StatusOK || response.Body.String() != expected {
		t.Fatalf("时间轴预览资源响应错误: code=%d body=%q", response.Code, response.Body.String())
	}
}

func availableTimelineStatus() TimelinePreviewStatus {
	return TimelinePreviewStatus{
		GenerationID: "generation-a", ProfileID: "desktop", SourceFingerprint: "source-a",
		Duration: 20, Version: 1, SpriteNames: []string{"sprite-001.jpg", "sprite-002.jpg"}, State: TimelinePreviewAvailable,
	}
}

func timelineStatusURL(mediaID int64, profile string) string {
	return "/api/play/" + stringID(mediaID) + "/timeline-preview?profile=" + profile
}

func timelineStatusURLWithoutProfile(mediaID int64) string {
	return "/api/play/" + stringID(mediaID) + "/timeline-preview"
}

func timelineRebuildURL(mediaID int64) string {
	return "/api/play/" + stringID(mediaID) + "/timeline-preview/rebuild"
}

func timelineResourceURL(mediaID int64, profile, fingerprint, generation, resource string) string {
	return "/api/play/" + stringID(mediaID) + "/timeline-preview/resources/" + profile + "/" + fingerprint + "/" + generation + "/" + resource
}

func stringID(value int64) string {
	return strconv.FormatInt(value, 10)
}

func assertJSONField(t *testing.T, payload []byte, field string, expected any) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if body[field] != expected {
		t.Fatalf("字段 %s 期望 %v，实际 %v", field, expected, body[field])
	}
}
