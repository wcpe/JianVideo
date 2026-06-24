package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// setupTranscodeRouter 构造注入了预设存储与预生成队列的测试路由。
// pregenExec 为预生成执行替身，避免依赖真实 ffmpeg。
func setupTranscodeRouter(t *testing.T, pregenExec transcoder.PregenExecFunc) (*gin.Engine, *gorm.DB, *transcoder.PregenQueue) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.TranscodePreset{}, &models.TranscodeTask{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	svc := library.NewService(db)
	store := transcoder.NewPresetStore(db)
	queue := transcoder.NewPregenQueue(db, pregenExec)
	queue.Start()
	t.Cleanup(queue.Stop)

	h := NewHandler(svc).WithTranscodePresets(store, queue)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, db, queue
}

// TestTranscodePreset_CRUD 预设建/列/改/删全流程。
func TestTranscodePreset_CRUD(t *testing.T) {
	exec := func(mediaID int64, codec string) error { return nil }
	r, _, _ := setupTranscodeRouter(t, exec)

	// 建
	w := doJSON(t, r, "POST", "/api/transcode/presets", `{"name":"1080p HEVC","codec":"h265","width":1920,"height":1080}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("建预设期望 201, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var created models.TranscodePreset
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == 0 || created.Codec != "h265" {
		t.Fatalf("建预设返回异常: %+v", created)
	}

	// 列
	w = doJSON(t, r, "GET", "/api/transcode/presets", "")
	if w.Code != http.StatusOK {
		t.Fatalf("列预设期望 200, 实际 %d", w.Code)
	}
	var listResp struct {
		Items []models.TranscodePreset `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if len(listResp.Items) != 1 {
		t.Fatalf("应有 1 条预设, 实际 %d", len(listResp.Items))
	}

	// 改
	w = doJSON(t, r, "PUT", "/api/transcode/presets/"+strconv.FormatInt(created.ID, 10), `{"name":"720p AV1","codec":"av1","width":1280,"height":720}`)
	if w.Code != http.StatusOK {
		t.Fatalf("改预设期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var updated models.TranscodePreset
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Codec != "av1" || updated.Width != 1280 {
		t.Fatalf("改预设未生效: %+v", updated)
	}

	// 删
	w = doJSON(t, r, "DELETE", "/api/transcode/presets/"+strconv.FormatInt(created.ID, 10), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("删预设期望 204, 实际 %d", w.Code)
	}
}

// TestTranscodePreset_CreateRejectInvalid 非法编码被拒为 400。
func TestTranscodePreset_CreateRejectInvalid(t *testing.T) {
	exec := func(mediaID int64, codec string) error { return nil }
	r, _, _ := setupTranscodeRouter(t, exec)

	w := doJSON(t, r, "POST", "/api/transcode/presets", `{"name":"x","codec":"mpeg2"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法编码应 400, 实际 %d, body=%s", w.Code, w.Body.String())
	}
}

// TestTranscodeTask_EnqueueAndList 入预生成队列并列任务，按预设快照编码执行。
func TestTranscodeTask_EnqueueAndList(t *testing.T) {
	var gotCodec string
	done := make(chan struct{})
	exec := func(mediaID int64, codec string) error {
		gotCodec = codec
		close(done)
		return nil
	}
	r, db, _ := setupTranscodeRouter(t, exec)

	// 预置一条媒体记录
	mf := &models.MediaFile{LibraryID: 1, FilePath: "/data/a.mp4", FileName: "a.mp4"}
	if err := db.Create(mf).Error; err != nil {
		t.Fatalf("预置媒体失败: %v", err)
	}
	// 建预设
	w := doJSON(t, r, "POST", "/api/transcode/presets", `{"name":"p","codec":"h265"}`)
	var preset models.TranscodePreset
	_ = json.Unmarshal(w.Body.Bytes(), &preset)

	// 入队
	w = doJSON(t, r, "POST", "/api/transcode/tasks", `{"media_id":`+strconv.FormatInt(mf.ID, 10)+`,"preset_id":`+strconv.FormatInt(preset.ID, 10)+`}`)
	if w.Code != http.StatusOK {
		t.Fatalf("入队期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var enqResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &enqResp)
	if enqResp["status"] != "queued" {
		t.Fatalf("入队响应 status 期望 queued, 实际 %v", enqResp["status"])
	}

	// 等执行完成，断言按预设编码 h265 调用
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("预生成 exec 未在超时内执行")
	}
	if gotCodec != "h265" {
		t.Fatalf("exec 应按预设编码 h265 执行, 实际 %q", gotCodec)
	}

	// 列任务
	w = doJSON(t, r, "GET", "/api/transcode/tasks", "")
	if w.Code != http.StatusOK {
		t.Fatalf("列任务期望 200, 实际 %d", w.Code)
	}
	var listResp struct {
		Tasks []models.TranscodeTask `json:"tasks"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if len(listResp.Tasks) != 1 {
		t.Fatalf("应有 1 条任务, 实际 %d", len(listResp.Tasks))
	}
}

// TestTranscodeTask_EnqueueRejectMissing 媒体或预设不存在时入队 404。
func TestTranscodeTask_EnqueueRejectMissing(t *testing.T) {
	exec := func(mediaID int64, codec string) error { return nil }
	r, db, _ := setupTranscodeRouter(t, exec)

	// 媒体不存在
	w := doJSON(t, r, "POST", "/api/transcode/tasks", `{"media_id":999,"preset_id":1}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("媒体不存在应 404, 实际 %d", w.Code)
	}

	// 预设不存在（媒体存在）
	mf := &models.MediaFile{LibraryID: 1, FilePath: "/data/a.mp4", FileName: "a.mp4"}
	_ = db.Create(mf).Error
	w = doJSON(t, r, "POST", "/api/transcode/tasks", `{"media_id":`+strconv.FormatInt(mf.ID, 10)+`,"preset_id":999}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("预设不存在应 404, 实际 %d", w.Code)
	}
}
