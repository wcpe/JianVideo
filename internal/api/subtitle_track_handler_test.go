package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/subtitle"
)

type subtitleAPIFixture struct {
	router  *gin.Engine
	db      *gorm.DB
	dataDir string
	media   models.MediaFile
	service *subtitle.Service
}

func setupSubtitleTrackAPI(t *testing.T) subtitleAPIFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "subtitle-api.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开字幕 API 测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取字幕 API 数据库连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Space{}, &models.LibraryPath{}, &models.MediaFile{}, &models.MediaMetadata{}, &models.MediaSubtitleTrack{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移字幕 API 测试表失败: %v", err)
	}
	for _, id := range []string{models.DefaultSpaceID, "space-other"} {
		if err := db.Create(&models.Space{ID: id, Name: id, OwnerUserID: 1}).Error; err != nil {
			t.Fatalf("创建测试 Space 失败: %v", err)
		}
	}
	mediaDir := t.TempDir()
	libraryPath := models.LibraryPath{SpaceID: models.DefaultSpaceID, Path: mediaDir, Type: "local", Label: "字幕 API"}
	if err := db.Create(&libraryPath).Error; err != nil {
		t.Fatalf("创建字幕 API 媒体库失败: %v", err)
	}
	mediaPath := filepath.Join(mediaDir, "movie.mkv")
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatalf("创建字幕 API 媒体失败: %v", err)
	}
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: libraryPath.ID, FilePath: mediaPath, FileName: "movie.mkv", FileState: models.MediaFileStateAvailable}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建字幕 API 媒体记录失败: %v", err)
	}
	service := subtitle.NewService(db, dataDir).WithAudit(audit.NewRecorder(db))
	router := gin.New()
	RegisterRoutes(router, NewHandler(library.NewService(db)).WithSubtitle(service))
	return subtitleAPIFixture{router: router, db: db, dataDir: dataDir, media: media, service: service}
}

func TestSubtitleTrackRoutesReturnSnakeCaseAndStableIDs(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	createSubtitleMetadata(t, fixture.db, fixture.media)
	writeSubtitleSidecar(t, fixture.media.FilePath, "movie.en.srt", validSRT("外挂"))
	first := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	if first.Code != http.StatusOK {
		t.Fatalf("统一轨道列表失败: code=%d body=%s", first.Code, first.Body.String())
	}
	var response subtitle.ListResponse
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析统一轨道响应失败: %v", err)
	}
	assertTrackResponse(t, response)
	second := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	if second.Body.String() != first.Body.String() {
		t.Fatalf("来源未变化时轨道响应与稳定 ID 不应变化\nfirst=%s\nsecond=%s", first.Body.String(), second.Body.String())
	}
	if strings.Contains(first.Body.String(), "channelLayout") || !strings.Contains(first.Body.String(), "channel_layout") {
		t.Fatalf("HTTP DTO 必须统一 snake_case: %s", first.Body.String())
	}
}

func assertTrackResponse(t *testing.T, response subtitle.ListResponse) {
	t.Helper()
	if len(response.Tracks) != 4 {
		t.Fatalf("应返回音轨、文本字幕、图片字幕和外挂字幕: %#v", response.Tracks)
	}
	audio := findAPITrack(response.Tracks, subtitle.KindAudio, "aac")
	if audio == nil || audio.Channels != 6 || audio.ChannelLayout != "5.1" || audio.Capability != subtitle.CapabilityUnsupported {
		t.Fatalf("音轨能力或声道字段错误: %#v", audio)
	}
	if response.Selection[subtitle.KindAudio].SelectedTrackID == nil || response.Selection[subtitle.KindAudio].EffectiveTrackID != nil {
		t.Fatalf("不能证实实际音轨时 effective_track_id 必须为 null: %#v", response.Selection)
	}
	assertAPISeamlessTrack(t, findAPITrack(response.Tracks, subtitle.KindSubtitle, "subrip"))
	assertAPISeamlessTrack(t, findAPITrackBySource(response.Tracks, subtitle.SourceSidecar))
	image := findAPITrack(response.Tracks, subtitle.KindSubtitle, "dvd_subtitle")
	if image == nil || image.Available || image.Capability != subtitle.CapabilityUnsupported || image.UnsupportedReason != subtitle.ReasonImageSubtitleUnsupported {
		t.Fatalf("图片字幕能力错误: %#v", image)
	}
	for _, source := range []string{subtitle.SourceEmbedded, subtitle.SourceSidecar, subtitle.SourceUploaded} {
		assertAPISeamlessCapability(t, response.Sources[source])
	}
	assertAPISeamlessCapability(t, response.Backend[subtitle.KindSubtitle])
	backendAudio := response.Backend[subtitle.KindAudio]
	if backendAudio.Available || backendAudio.Capability != subtitle.CapabilityUnsupported || backendAudio.UnsupportedReason != subtitle.ReasonAudioSwitchUnsupported {
		t.Fatalf("后端音轨能力错误: %#v", backendAudio)
	}
}

func TestSubtitleTrackEmbeddedTextAndImageContent(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	createSubtitleMetadata(t, fixture.db, fixture.media)
	fixture.service.WithExtractor(func(_ context.Context, _ string, streamIndex int, outputPath string) error {
		if streamIndex != 2 {
			return fmt.Errorf("错误的内嵌字幕流: %d", streamIndex)
		}
		return os.WriteFile(outputPath, []byte(validSRT("内嵌文本")), 0o600)
	})
	response := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	var tracks subtitle.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &tracks); err != nil {
		t.Fatalf("解析内嵌轨道响应失败: %v", err)
	}
	textTrack := findAPITrack(tracks.Tracks, subtitle.KindSubtitle, "subrip")
	imageTrack := findAPITrack(tracks.Tracks, subtitle.KindSubtitle, "dvd_subtitle")
	if textTrack == nil || imageTrack == nil {
		t.Fatalf("缺少内嵌文本或图片字幕: %#v", tracks.Tracks)
	}
	text := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/"+textTrack.ID+"/content"), nil, "", "")
	if text.Code != http.StatusOK || !strings.Contains(text.Body.String(), "内嵌文本") {
		t.Fatalf("内嵌文本字幕内容失败: code=%d body=%s", text.Code, text.Body.String())
	}
	image := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/"+imageTrack.ID+"/content"), nil, "", "")
	assertSubtitleAPIError(t, image, http.StatusUnprocessableEntity, subtitle.ReasonImageSubtitleUnsupported)
	assertNoAPITempFiles(t, fixture.dataDir)
}

func TestSubtitleTrackLegacyListAndIndexRemainCompatibleInProductionRouter(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	writeSubtitleSidecar(t, fixture.media.FilePath, "movie.zh.srt", validSRT("旧路由"))
	list := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles"), nil, "", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"index":0`) || !strings.Contains(list.Body.String(), `"url":"/api/play/`) {
		t.Fatalf("旧字幕列表必须保留 index 和 url: code=%d body=%s", list.Code, list.Body.String())
	}
	content := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/0"), nil, "", "")
	if content.Code != http.StatusOK || !strings.Contains(content.Body.String(), "旧路由") {
		t.Fatalf("旧字幕 index 内容路径不兼容: code=%d body=%s", content.Code, content.Body.String())
	}
	tracks := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	var unified subtitle.ListResponse
	if err := json.Unmarshal(tracks.Body.Bytes(), &unified); err != nil || len(unified.Tracks) != 1 {
		t.Fatalf("解析外挂稳定轨道失败: err=%v body=%s", err, tracks.Body.String())
	}
	deleted := performSubtitleRequest(fixture.router, http.MethodDelete, subtitleAPIPath(fixture.media.ID, "/subtitles/"+unified.Tracks[0].ID), nil, "", "")
	if deleted.Code != http.StatusNotFound {
		t.Fatalf("外挂字幕不得通过删除 API 删除: code=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.media.FilePath), "movie.zh.srt")); err != nil {
		t.Fatalf("外挂字幕源文件不得被删除: %v", err)
	}
}

func TestSubtitleContentEndpointsKeepMalformedSidecarAs422(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	writeSubtitleSidecar(t, fixture.media.FilePath, "movie.srt", validASS("伪装格式"))
	tracksResponse := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	var tracks subtitle.ListResponse
	if err := json.Unmarshal(tracksResponse.Body.Bytes(), &tracks); err != nil {
		t.Fatalf("解析格式错误外挂轨道失败: %v", err)
	}
	track := findAPITrackBySource(tracks.Tracks, subtitle.SourceSidecar)
	if track == nil {
		t.Fatalf("未找到格式错误外挂轨道: %#v", tracks.Tracks)
	}
	unified := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/"+track.ID+"/content"), nil, "", "")
	assertSubtitleAPIError(t, unified, http.StatusUnprocessableEntity, "SUBTITLE_UNPROCESSABLE")
	legacy := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/0"), nil, "", "")
	assertSubtitleAPIError(t, legacy, http.StatusUnprocessableEntity, "SUBTITLE_UNPROCESSABLE")
}

func TestSubtitleContentEndpointsReturn404WhenEnumeratedSidecarDisappears(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	sidecarPath := filepath.Join(filepath.Dir(fixture.media.FilePath), "movie.srt")
	writeSubtitleSidecar(t, fixture.media.FilePath, "movie.srt", validSRT("原字幕"))
	tracksResponse := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	var tracks subtitle.ListResponse
	if err := json.Unmarshal(tracksResponse.Body.Bytes(), &tracks); err != nil {
		t.Fatalf("解析统一外挂轨道失败: %v", err)
	}
	track := findAPITrackBySource(tracks.Tracks, subtitle.SourceSidecar)
	if track == nil {
		t.Fatalf("未找到统一外挂轨道: %#v", tracks.Tracks)
	}
	legacyList := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles"), nil, "", "")
	if legacyList.Code != http.StatusOK || !strings.Contains(legacyList.Body.String(), `"index":0`) {
		t.Fatalf("枚举旧兼容外挂轨道失败: code=%d body=%s", legacyList.Code, legacyList.Body.String())
	}
	if err := os.Remove(sidecarPath); err != nil {
		t.Fatalf("删除已枚举字幕失败: %v", err)
	}
	unified := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/"+track.ID+"/content"), nil, "", "")
	assertSubtitleAPIError(t, unified, http.StatusNotFound, "SUBTITLE_NOT_FOUND")
	legacy := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/0"), nil, "", "")
	assertSubtitleAPIError(t, legacy, http.StatusNotFound, "SUBTITLE_NOT_FOUND")
}

func TestSubtitleContentEndpointsReturn404AfterSidecarSymlinkReplacement(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	sidecarPath := filepath.Join(filepath.Dir(fixture.media.FilePath), "movie.srt")
	writeSubtitleSidecar(t, fixture.media.FilePath, "movie.srt", validSRT("原字幕"))
	tracksResponse := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	var tracks subtitle.ListResponse
	if err := json.Unmarshal(tracksResponse.Body.Bytes(), &tracks); err != nil {
		t.Fatalf("解析统一外挂轨道失败: %v", err)
	}
	track := findAPITrackBySource(tracks.Tracks, subtitle.SourceSidecar)
	if track == nil {
		t.Fatalf("未找到统一外挂轨道: %#v", tracks.Tracks)
	}
	legacyList := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles"), nil, "", "")
	if legacyList.Code != http.StatusOK || !strings.Contains(legacyList.Body.String(), `"index":0`) {
		t.Fatalf("枚举旧兼容外挂轨道失败: code=%d body=%s", legacyList.Code, legacyList.Body.String())
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.srt")
	if err := os.WriteFile(outsidePath, []byte(validSRT("目录外秘密")), 0o600); err != nil {
		t.Fatalf("创建目录外字幕失败: %v", err)
	}
	if err := os.Remove(sidecarPath); err != nil {
		t.Fatalf("删除已枚举字幕失败: %v", err)
	}
	if err := os.Symlink(outsidePath, sidecarPath); err != nil {
		t.Skipf("当前环境不支持创建符号链接: %v", err)
	}
	unified := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/"+track.ID+"/content"), nil, "", "")
	if unified.Code != http.StatusNotFound || strings.Contains(unified.Body.String(), "目录外秘密") {
		t.Fatalf("统一字幕内容端点必须返回 404 且不得泄露内容: code=%d body=%s", unified.Code, unified.Body.String())
	}
	legacy := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/0"), nil, "", "")
	if legacy.Code != http.StatusNotFound || strings.Contains(legacy.Body.String(), "目录外秘密") {
		t.Fatalf("旧兼容字幕内容端点必须返回 404 且不得泄露内容: code=%d body=%s", legacy.Code, legacy.Body.String())
	}
}

func TestSubtitleTrackHTTPUploadContentDeleteAndSpaceIsolation(t *testing.T) {
	samples := map[string]string{
		"srt": validSRT("SRT"),
		"ass": validASS("ASS"),
		"ssa": validASS("SSA"),
		"vtt": "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nVTT\n",
	}
	for format, content := range samples {
		t.Run(format, func(t *testing.T) {
			fixture := setupSubtitleTrackAPI(t)
			track := uploadSubtitleAPI(t, fixture, "sample."+format, content, http.StatusCreated)
			path, err := fixture.service.StoredPath(context.Background(), fixture.media.SpaceID, fixture.media.ID, track.ID)
			if err != nil {
				t.Fatalf("解析上传字幕路径失败: %v", err)
			}
			expectedDir := filepath.Join(fixture.dataDir, "subtitles", fixture.media.SpaceID, strconv.FormatInt(fixture.media.ID, 10))
			if filepath.Dir(path) != expectedDir {
				t.Fatalf("上传字幕路径未严格隔离到 space/media/track: %s", path)
			}
			denied := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/"+track.ID+"/content"), nil, "", "space-other")
			if denied.Code != http.StatusNotFound {
				t.Fatalf("跨 Space 内容读取必须 404: code=%d body=%s", denied.Code, denied.Body.String())
			}
			contentResponse := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/"+track.ID+"/content"), nil, "", "")
			if contentResponse.Code != http.StatusOK || !strings.HasPrefix(contentResponse.Body.String(), "WEBVTT") {
				t.Fatalf("读取上传字幕失败: code=%d body=%s", contentResponse.Code, contentResponse.Body.String())
			}
			deleted := performSubtitleRequest(fixture.router, http.MethodDelete, subtitleAPIPath(fixture.media.ID, "/subtitles/"+track.ID), nil, "", "")
			if deleted.Code != http.StatusNoContent {
				t.Fatalf("删除上传字幕失败: code=%d body=%s", deleted.Code, deleted.Body.String())
			}
			missing := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/"+track.ID+"/content"), nil, "", "")
			if missing.Code != http.StatusNotFound {
				t.Fatalf("删除后内容必须 404: code=%d body=%s", missing.Code, missing.Body.String())
			}
		})
	}
}

func TestSubtitleTrackHTTPStructuredUploadErrors(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	cases := []struct {
		name, fileName, content, code string
		status                        int
	}{
		{name: "伪装格式", fileName: "fake.srt", content: validASS("伪装"), status: http.StatusUnprocessableEntity, code: "SUBTITLE_UNPROCESSABLE"},
		{name: "路径文件名", fileName: "../evil.srt", content: validSRT("路径"), status: http.StatusBadRequest, code: "INVALID_SUBTITLE"},
		{name: "二进制", fileName: "binary.srt", content: "1\n\x00bad", status: http.StatusUnprocessableEntity, code: "SUBTITLE_UNPROCESSABLE"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			response := uploadSubtitleResponse(t, fixture, item.fileName, item.content)
			assertSubtitleAPIError(t, response, item.status, item.code)
		})
	}
	large := strings.Repeat("x", int(subtitle.MaxUploadBytes+1))
	response := uploadSubtitleResponse(t, fixture, "large.srt", large)
	assertSubtitleAPIError(t, response, http.StatusRequestEntityTooLarge, "SUBTITLE_TOO_LARGE")
	invalidBody := bytes.NewBufferString(`{"file":"not-multipart"}`)
	response = performSubtitleRequest(fixture.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/subtitles"), invalidBody, "application/json", "")
	assertSubtitleAPIError(t, response, http.StatusBadRequest, "INVALID_SUBTITLE")
	assertNoAPITempFiles(t, fixture.dataDir)
}

func TestSubtitleTrackHTTPUploadRejectsAdditionalMultipartParts(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	file := multipartTestPart{field: "file", fileName: "sample.srt", content: validSRT("严格单文件")}
	cases := []struct {
		name  string
		parts []multipartTestPart
	}{
		{name: "前置普通字段", parts: []multipartTestPart{{field: "note", content: "before"}, file}},
		{name: "前置其他文件", parts: []multipartTestPart{{field: "other", fileName: "other.srt", content: validSRT("其他")}, file}},
		{name: "后置普通字段", parts: []multipartTestPart{file, {field: "note", content: "after"}}},
		{name: "双 file", parts: []multipartTestPart{file, {field: "file", fileName: "second.srt", content: validSRT("第二个")}}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			response := uploadSubtitlePartsResponse(t, fixture, item.parts)
			assertSubtitleAPIError(t, response, http.StatusBadRequest, "INVALID_SUBTITLE")
		})
	}
	assertNoAPITempFiles(t, fixture.dataDir)
}

func TestSubtitleTrackServiceUnavailableReturns503(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	router := gin.New()
	RegisterRoutes(router, NewHandler(library.NewService(fixture.db)))
	response := performSubtitleRequest(router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	assertSubtitleAPIError(t, response, http.StatusServiceUnavailable, "SUBTITLE_SERVICE_UNAVAILABLE")
}

func TestSubtitleTrackSMBSourceCannotBypassContent(t *testing.T) {
	fixture := setupSubtitleTrackAPI(t)
	fixture.media.FilePath = "smb://server/share/movie.mkv"
	if err := fixture.db.Save(&fixture.media).Error; err != nil {
		t.Fatalf("更新 SMB 媒体失败: %v", err)
	}
	response := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/tracks"), nil, "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), subtitle.ReasonSMBSidecarUnsupported) {
		t.Fatalf("SMB 轨道来源能力错误: code=%d body=%s", response.Code, response.Body.String())
	}
	content := performSubtitleRequest(fixture.router, http.MethodGet, subtitleAPIPath(fixture.media.ID, "/subtitles/sid-forged/content"), nil, "", "")
	if content.Code != http.StatusNotFound {
		t.Fatalf("SMB 外挂内容不得绕过来源能力: code=%d body=%s", content.Code, content.Body.String())
	}
}

func createSubtitleMetadata(t *testing.T, db *gorm.DB, media models.MediaFile) {
	t.Helper()
	normalized := map[string]any{
		"audio_streams":    []map[string]any{{"index": 1, "codec_name": "aac", "language": "zh", "title": "主音轨", "channels": 6, "channel_layout": "5.1", "default": true}},
		"subtitle_streams": []map[string]any{{"index": 2, "codec_name": "subrip", "title": "文本"}, {"index": 3, "codec_name": "dvd_subtitle", "title": "图片"}},
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("编码测试元数据失败: %v", err)
	}
	metadata := models.MediaMetadata{MediaID: media.ID, SpaceID: media.SpaceID, Source: "ffprobe", Tool: "ffprobe", ToolVersion: "7", RawJSON: `{}`, NormalizedJSON: string(encoded), ParsedAt: time.Now(), Stale: false}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatalf("创建测试元数据失败: %v", err)
	}
}

func writeSubtitleSidecar(t *testing.T, mediaPath, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(mediaPath), name), []byte(content), 0o600); err != nil {
		t.Fatalf("创建外挂字幕失败: %v", err)
	}
}

func findAPITrack(tracks []subtitle.Track, kind, codec string) *subtitle.Track {
	for index := range tracks {
		if tracks[index].Kind == kind && tracks[index].Codec == codec {
			return &tracks[index]
		}
	}
	return nil
}

func findAPITrackBySource(tracks []subtitle.Track, source string) *subtitle.Track {
	for index := range tracks {
		if tracks[index].Kind == subtitle.KindSubtitle && tracks[index].Source == source {
			return &tracks[index]
		}
	}
	return nil
}

func assertAPISeamlessTrack(t *testing.T, track *subtitle.Track) {
	t.Helper()
	if track == nil || !track.Available || track.Capability != subtitle.CapabilitySeamless || track.UnsupportedReason != "" {
		t.Fatalf("可渲染字幕轨道必须 available/seamless: %#v", track)
	}
}

func assertAPISeamlessCapability(t *testing.T, capability subtitle.SourceCapability) {
	t.Helper()
	if !capability.Available || capability.Capability != subtitle.CapabilitySeamless || capability.UnsupportedReason != "" {
		t.Fatalf("字幕来源或后端能力必须 available/seamless: %#v", capability)
	}
}

func uploadSubtitleAPI(t *testing.T, fixture subtitleAPIFixture, fileName, content string, status int) subtitle.Track {
	t.Helper()
	response := uploadSubtitleResponse(t, fixture, fileName, content)
	if response.Code != status {
		t.Fatalf("上传字幕状态错误: code=%d body=%s", response.Code, response.Body.String())
	}
	var track subtitle.Track
	if err := json.Unmarshal(response.Body.Bytes(), &track); err != nil {
		t.Fatalf("解析上传字幕响应失败: %v", err)
	}
	assertAPISeamlessTrack(t, &track)
	return track
}

func uploadSubtitleResponse(t *testing.T, fixture subtitleAPIFixture, fileName, content string) *httptest.ResponseRecorder {
	t.Helper()
	return uploadSubtitlePartsResponse(t, fixture, []multipartTestPart{{field: "file", fileName: fileName, content: content}})
}

type multipartTestPart struct {
	field    string
	fileName string
	content  string
}

func uploadSubtitlePartsResponse(t *testing.T, fixture subtitleAPIFixture, parts []multipartTestPart) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, item := range parts {
		part, err := createMultipartTestPart(writer, item)
		if err != nil {
			t.Fatalf("创建 multipart 字段失败: %v", err)
		}
		if _, err := part.Write([]byte(item.content)); err != nil {
			t.Fatalf("写 multipart 字段失败: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}
	return performSubtitleRequest(fixture.router, http.MethodPost, subtitleAPIPath(fixture.media.ID, "/subtitles"), &body, writer.FormDataContentType(), "")
}

func createMultipartTestPart(writer *multipart.Writer, item multipartTestPart) (io.Writer, error) {
	if item.fileName != "" {
		return writer.CreateFormFile(item.field, item.fileName)
	}
	return writer.CreateFormField(item.field)
}

func performSubtitleRequest(router *gin.Engine, method, path string, body *bytes.Buffer, contentType, spaceID string) *httptest.ResponseRecorder {
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		requestBody = bytes.NewReader(body.Bytes())
	}
	request := httptest.NewRequest(method, path, requestBody)
	request.Header.Set("Content-Type", contentType)
	if spaceID != "" {
		request.Header.Set(spaceHeader, spaceID)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func subtitleAPIPath(mediaID int64, suffix string) string {
	return fmt.Sprintf("/api/play/%d%s", mediaID, suffix)
}

func assertSubtitleAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("结构化字幕错误不符: want=%d/%s got=%d/%s", status, code, response.Code, response.Body.String())
	}
}

func assertNoAPITempFiles(t *testing.T, dataDir string) {
	t.Helper()
	_ = filepath.WalkDir(filepath.Join(dataDir, "subtitles"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry != nil && !entry.IsDir() && strings.Contains(entry.Name(), ".tmp-") {
			t.Errorf("发现残留字幕临时文件: %s", path)
		}
		return nil
	})
}

func validSRT(text string) string {
	return "1\n00:00:01,000 --> 00:00:02,000\n" + text + "\n"
}

func validASS(text string) string {
	return "[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,," + text + "\n"
}
