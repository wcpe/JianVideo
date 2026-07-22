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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// newNegotiateDB 建内存库并迁移协商端点所需的全部表（限单连接保证可见）。
// 用测试名做唯一 cache 名，避免同包测试间共享同一内存库导致表数据串扰。
func newNegotiateDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.Setting{},
		&models.CodecProbeCache{},
	); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return db
}

// seedProbeCache 把指定实测结果写入当前 ffmpeg 版本的缓存，使 Capabilities().Codecs 得到对应并集。
func seedProbeCache(t *testing.T, db *gorm.DB, results []transcoder.EncoderProbeResult) {
	t.Helper()
	version := transcoder.FFmpegVersion(context.Background())
	raw, _ := json.Marshal(results)
	if err := db.Create(&models.CodecProbeCache{FFmpegVersion: version, Results: string(raw), TestedAt: time.Now()}).Error; err != nil {
		t.Fatalf("写入实测缓存失败: %v", err)
	}
}

// doNegotiate 对指定 mediaID 发起协商请求，返回响应记录与解析后的描述符。
func doNegotiate(t *testing.T, h *Handler, mediaID string, body string) (*httptest.ResponseRecorder, transcoder.NegotiationDescriptor) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: mediaID}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/play/"+mediaID+"/negotiate", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.Negotiate(c)
	var d transcoder.NegotiationDescriptor
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	return w, d
}

// TestNegotiate_H264_ReturnsTSAndRecordsSession 仅可产出 h264 时协商返回 TS 描述符并记录会话。
func TestNegotiate_H264_ReturnsTSAndRecordsSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newNegotiateDB(t)
	libSvc := library.NewService(db)
	mf, err := libSvc.CreateMediaFile(1, "/tmp/sample.mp4", 1000)
	if err != nil {
		t.Fatalf("建媒体记录失败: %v", err)
	}

	// 仅 h264 可产出
	seedProbeCache(t, db, []transcoder.EncoderProbeResult{
		{Encoder: "libx264", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
	})

	pbSvc := playback.NewService()
	t.Cleanup(pbSvc.Stop)
	h := NewHandler(libSvc).
		WithSettings(settings.NewService(db)).
		WithCapabilityService(transcoder.NewCapabilityService(db)).
		WithPlayback(pbSvc)

	// 客户端支持 av1，但系统不可产出 av1 → 兜底 h264
	w, d := doNegotiate(t, h, "1", `{"client_caps":{"av1":true,"h265":true,"vp9":true}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d，体=%s", w.Code, w.Body.String())
	}
	if d.Codec != "h264" || d.Path != "ts" {
		t.Errorf("期望 h264/ts，实得 %s/%s", d.Codec, d.Path)
	}
	if d.FramePresentation != nil {
		t.Error("无法验证真实画面 marker 的普通媒体不得伪装 exact 契约")
	}

	// 会话记录实际编码与路径
	sess := pbSvc.GetOrCreateSession(mf.ID, 0, 0)
	if sess.TargetCodec != "h264" || sess.OutputPath != "ts" {
		t.Errorf("会话应记录 h264/ts，实得 %s/%s", sess.TargetCodec, sess.OutputPath)
	}
}

// TestNegotiate_AdvancedChosen_DegradesWhenProduceFails 协商选中高级编码但 fMP4 产出失败时降级回 h264。
// 测试媒体文件路径不存在 → produceFMP4 返回 false → 降级 h264/TS（不报错）。
func TestNegotiate_AdvancedChosen_DegradesWhenProduceFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newNegotiateDB(t)
	libSvc := library.NewService(db)
	if _, err := libSvc.CreateMediaFile(1, "/nonexistent/av1-source.mp4", 1000); err != nil {
		t.Fatalf("建媒体记录失败: %v", err)
	}

	// av1 与 h264 均可产出
	seedProbeCache(t, db, []transcoder.EncoderProbeResult{
		{Encoder: "libx264", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
		{Encoder: "libsvtav1", Family: "software", Codec: "av1", Compiled: true, TestedOK: true},
	})

	// 设首选优先级 av1 > h264
	settingsSvc := settings.NewService(db)
	if err := settingsSvc.SetTranscodeCodecPriority([]string{"av1", "h264"}, []string{"av1", "h264"}); err != nil {
		t.Fatalf("写优先级失败: %v", err)
	}

	pbSvc := playback.NewService()
	t.Cleanup(pbSvc.Stop)
	h := NewHandler(libSvc).
		WithSettings(settingsSvc).
		WithCapabilityService(transcoder.NewCapabilityService(db)).
		WithPlayback(pbSvc)
	// 注意：未注入 hlsDir/hlsMgr → produceFMP4 直接返回 false → 降级

	// 客户端支持 av1 → 协商选 av1，但产出失败 → 降级 h264/TS
	w, d := doNegotiate(t, h, "1", `{"client_caps":{"av1":true}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d，体=%s", w.Code, w.Body.String())
	}
	if d.Codec != "h264" || d.Path != "ts" {
		t.Errorf("产出失败应降级 h264/ts，实得 %s/%s", d.Codec, d.Path)
	}
}

// TestNegotiate_NotFound 媒体不存在返回 404。
func TestNegotiate_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newNegotiateDB(t)
	h := NewHandler(library.NewService(db)).
		WithSettings(settings.NewService(db)).
		WithCapabilityService(transcoder.NewCapabilityService(db))

	w, _ := doNegotiate(t, h, "999", `{"client_caps":{}}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404，实得 %d", w.Code)
	}
}

// TestNegotiate_NoServices_FallbackH264 未注入 settings/capability 时回退 h264/TS。
func TestNegotiate_NoServices_FallbackH264(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newNegotiateDB(t)
	libSvc := library.NewService(db)
	if _, err := libSvc.CreateMediaFile(1, "/tmp/x.mp4", 1); err != nil {
		t.Fatalf("建媒体记录失败: %v", err)
	}
	h := NewHandler(libSvc) // 不注入 settings/capability/playback

	w, d := doNegotiate(t, h, "1", `{"client_caps":{"av1":true}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d", w.Code)
	}
	if d.Codec != "h264" || d.Path != "ts" {
		t.Errorf("无服务应回退 h264/ts，实得 %s/%s", d.Codec, d.Path)
	}
}

// TestNegotiate_VerifiedMarkerReturnsBoundedFrameContract 真实像素验证成功时返回有界逐帧契约。
func TestNegotiate_VerifiedMarkerReturnsBoundedFrameContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newNegotiateDB(t)
	libSvc := library.NewService(db)
	if _, err := libSvc.CreateMediaFile(1, "/tmp/verified-marker.mp4", 1); err != nil {
		t.Fatalf("建媒体记录失败: %v", err)
	}
	original := detectFramePresentation
	detectFramePresentation = func(context.Context, string) *transcoder.FramePresentationDescriptor {
		return buildFramePresentation(2, 260)
	}
	t.Cleanup(func() { detectFramePresentation = original })

	w, descriptor := doNegotiate(t, NewHandler(libSvc), "1", `{"client_caps":{}}`)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d，体=%s", w.Code, w.Body.String())
	}
	presentation := descriptor.FramePresentation
	if presentation == nil {
		t.Fatal("已验证 marker 的媒体必须返回 frame_presentation")
	}
	if descriptor.Path != "mp4" || descriptor.URL != "/api/play/1/stream" {
		t.Fatalf("逐帧契约必须描述实际验证的原文件直出路径: %+v", descriptor)
	}
	if presentation.NominalFrameRate != 2 || len(presentation.Timeline) != 260 {
		t.Fatalf("逐帧契约不符: %+v", presentation)
	}
	if presentation.Timeline[259].StableFrameID != "binary-marker:259" {
		t.Fatalf("稳定帧索引不符: %+v", presentation.Timeline[259])
	}
}
