package subtitle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

func setupServiceTest(t *testing.T) (*Service, *gorm.DB, string, models.MediaFile) {
	t.Helper()
	dataDir := t.TempDir()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取测试数据库连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&models.Space{}, &models.LibraryPath{}, &models.MediaFile{},
		&models.MediaMetadata{}, &models.MediaSubtitleTrack{}, &models.AuditEvent{}, &models.CacheAsset{},
	); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	space := models.Space{ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1}
	if err := db.Create(&space).Error; err != nil {
		t.Fatalf("创建 Space 失败: %v", err)
	}
	mediaDir := t.TempDir()
	libraryPath := models.LibraryPath{SpaceID: space.ID, Path: mediaDir, Type: "local", Label: "测试库"}
	if err := db.Create(&libraryPath).Error; err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	mediaPath := filepath.Join(mediaDir, "movie.mkv")
	if err := os.WriteFile(mediaPath, []byte("media-source"), 0o644); err != nil {
		t.Fatalf("创建媒体文件失败: %v", err)
	}
	media := models.MediaFile{SpaceID: space.ID, LibraryID: libraryPath.ID, FilePath: mediaPath, FileName: "movie.mkv", Format: "mkv", FileState: models.MediaFileStateAvailable}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体记录失败: %v", err)
	}
	service := NewService(db, dataDir).WithAudit(audit.NewRecorder(db))
	return service, db, dataDir, media
}

func TestListAggregatesStableTracks(t *testing.T) {
	service, db, _, media := setupServiceTest(t)
	normalized := map[string]any{
		"audio_streams": []map[string]any{{"index": 2, "codec_name": "aac", "language": "zh", "title": "中文", "default": true}},
		"subtitle_streams": []map[string]any{
			{"index": 4, "codec_name": "subrip", "language": "en", "title": "English"},
			{"index": 5, "codec_name": "hdmv_pgs_subtitle", "language": "zh", "title": "图片字幕"},
		},
	}
	encoded, _ := json.Marshal(normalized)
	metadata := models.MediaMetadata{MediaID: media.ID, SpaceID: media.SpaceID, Source: "ffprobe", Tool: "ffprobe", ToolVersion: "7", RawJSON: `{}`, NormalizedJSON: string(encoded), ParsedAt: time.Now(), Stale: false}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatalf("创建元数据失败: %v", err)
	}
	for name, content := range map[string]string{
		"movie.en.srt": "1\n00:00:01,000 --> 00:00:02,000\nEnglish\n",
		"movie.ass":    "[Events]\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,中文\n",
	} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(media.FilePath), name), []byte(content), 0o644); err != nil {
			t.Fatalf("创建外挂字幕失败: %v", err)
		}
	}
	uploaded := models.MediaSubtitleTrack{ID: "upl-fixed", SpaceID: media.SpaceID, MediaID: media.ID, Source: SourceUploaded, SourceRef: "upload.vtt", StorageRelativePath: "subtitles/space-default/1/upl-fixed.vtt", StreamIndex: -1, Format: "vtt", Title: "上传字幕"}
	if err := db.Create(&uploaded).Error; err != nil {
		t.Fatalf("创建上传记录失败: %v", err)
	}

	first, err := service.List(context.Background(), media.SpaceID, media.ID)
	if err != nil {
		t.Fatalf("聚合轨道失败: %v", err)
	}
	if len(first.Tracks) != 6 {
		t.Fatalf("期望 6 条轨道，实际 %d: %#v", len(first.Tracks), first.Tracks)
	}
	ids := make(map[string]bool, len(first.Tracks))
	for _, track := range first.Tracks {
		if track.ID == "" || ids[track.ID] {
			t.Fatalf("轨道 ID 必须非空且唯一: %#v", first.Tracks)
		}
		ids[track.ID] = true
	}
	assertAggregatedTrackCapabilities(t, first)

	if err := os.WriteFile(filepath.Join(filepath.Dir(media.FilePath), "movie.zh.vtt"), []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n新增\n"), 0o644); err != nil {
		t.Fatalf("新增外挂字幕失败: %v", err)
	}
	second, err := service.List(context.Background(), media.SpaceID, media.ID)
	if err != nil {
		t.Fatalf("再次聚合失败: %v", err)
	}
	for id := range ids {
		if !containsTrackID(second.Tracks, id) {
			t.Fatalf("新增来源后既有稳定 ID 丢失: %s", id)
		}
	}
}

func assertAggregatedTrackCapabilities(t *testing.T, response ListResponse) {
	t.Helper()
	for _, track := range response.Tracks {
		if track.Kind == KindAudio {
			if !track.Available || track.Capability != CapabilityUnsupported || track.UnsupportedReason != ReasonAudioSwitchUnsupported {
				t.Fatalf("音轨必须保持不可切换: %#v", track)
			}
			continue
		}
		if track.Codec == "hdmv_pgs_subtitle" {
			if track.Available || track.Capability != CapabilityUnsupported || track.UnsupportedReason != ReasonImageSubtitleUnsupported {
				t.Fatalf("图片字幕必须结构化不可用: %#v", track)
			}
			continue
		}
		assertSeamlessSubtitleTrack(t, track)
	}
	for _, source := range []string{SourceEmbedded, SourceSidecar, SourceUploaded} {
		assertSeamlessSourceCapability(t, response.Sources[source])
	}
	assertSeamlessSourceCapability(t, response.Backend[KindSubtitle])
	if audio := response.Backend[KindAudio]; audio.Available || audio.Capability != CapabilityUnsupported || audio.UnsupportedReason != ReasonAudioSwitchUnsupported {
		t.Fatalf("后端音轨能力必须保持 unsupported: %#v", audio)
	}
}

func assertSeamlessSubtitleTrack(t *testing.T, track Track) {
	t.Helper()
	if !track.Available || track.Capability != CapabilitySeamless || track.UnsupportedReason != "" {
		t.Fatalf("可渲染字幕轨道必须 seamless: %#v", track)
	}
}

func assertSeamlessSourceCapability(t *testing.T, capability SourceCapability) {
	t.Helper()
	if !capability.Available || capability.Capability != CapabilitySeamless || capability.UnsupportedReason != "" {
		t.Fatalf("字幕能力必须 available/seamless: %#v", capability)
	}
}

func TestEmbeddedSubtitleTrackCapabilities(t *testing.T) {
	media := models.MediaFile{ID: 1, SpaceID: models.DefaultSpaceID}
	text := embeddedSubtitleTrack(media, streamMetadata{Index: 1, CodecName: "subrip"})
	assertSeamlessSubtitleTrack(t, text)
	image := embeddedSubtitleTrack(media, streamMetadata{Index: 2, CodecName: "dvd_subtitle"})
	if image.Available || image.Capability != CapabilityUnsupported || image.UnsupportedReason != ReasonImageSubtitleUnsupported {
		t.Fatalf("图片字幕必须保持 unsupported: %#v", image)
	}
	unknown := embeddedSubtitleTrack(media, streamMetadata{Index: 3, CodecName: "unknown"})
	if unknown.Available || unknown.Capability != CapabilityUnsupported || unknown.UnsupportedReason != ReasonSubtitleCodecUnsupported {
		t.Fatalf("未知字幕编码必须保持 unsupported: %#v", unknown)
	}
}

func TestListSMBSidecarCapability(t *testing.T) {
	service, db, _, media := setupServiceTest(t)
	media.FilePath = "smb://server/share/movie.mkv"
	if err := db.Save(&media).Error; err != nil {
		t.Fatalf("更新 SMB 媒体失败: %v", err)
	}
	response, err := service.List(context.Background(), media.SpaceID, media.ID)
	if err != nil {
		t.Fatalf("列出 SMB 轨道失败: %v", err)
	}
	capability := response.Sources[SourceSidecar]
	if capability.Available || capability.Capability != CapabilityUnsupported || capability.UnsupportedReason != ReasonSMBSidecarUnsupported {
		t.Fatalf("SMB 外挂来源能力错误: %#v", capability)
	}
}

func TestSidecarContentRemovedAfterEnumerationReturnsNotFound(t *testing.T) {
	service, _, _, media := setupServiceTest(t)
	sidecarPath := filepath.Join(filepath.Dir(media.FilePath), "movie.srt")
	if err := os.WriteFile(sidecarPath, []byte(validServiceSRT("原字幕")), 0o600); err != nil {
		t.Fatalf("创建外挂字幕失败: %v", err)
	}
	response, err := service.List(context.Background(), media.SpaceID, media.ID)
	if err != nil {
		t.Fatalf("枚举外挂字幕失败: %v", err)
	}
	track := findTrackBySource(response.Tracks, SourceSidecar)
	if track == nil {
		t.Fatalf("未找到外挂字幕轨道: %#v", response.Tracks)
	}
	if err := os.Remove(sidecarPath); err != nil {
		t.Fatalf("删除已枚举字幕失败: %v", err)
	}
	content, err := service.Content(context.Background(), media.SpaceID, media.ID, track.ID)
	if !errors.Is(err, ErrNotFound) || content != "" {
		t.Fatalf("已消失外挂字幕必须按未找到处理且不得返回内容: err=%v content=%s", err, content)
	}
}

func TestSidecarContentSymlinkReplacementReturnsNotFound(t *testing.T) {
	service, _, _, media := setupServiceTest(t)
	sidecarPath := filepath.Join(filepath.Dir(media.FilePath), "movie.srt")
	if err := os.WriteFile(sidecarPath, []byte(validServiceSRT("原字幕")), 0o600); err != nil {
		t.Fatalf("创建外挂字幕失败: %v", err)
	}
	response, err := service.List(context.Background(), media.SpaceID, media.ID)
	if err != nil {
		t.Fatalf("枚举外挂字幕失败: %v", err)
	}
	track := findTrackBySource(response.Tracks, SourceSidecar)
	if track == nil {
		t.Fatalf("未找到外挂字幕轨道: %#v", response.Tracks)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.srt")
	if err := os.WriteFile(outsidePath, []byte(validServiceSRT("目录外秘密")), 0o600); err != nil {
		t.Fatalf("创建目录外字幕失败: %v", err)
	}
	if err := os.Remove(sidecarPath); err != nil {
		t.Fatalf("删除已枚举字幕失败: %v", err)
	}
	if err := os.Symlink(outsidePath, sidecarPath); err != nil {
		t.Skipf("当前环境不支持创建符号链接: %v", err)
	}
	content, err := service.Content(context.Background(), media.SpaceID, media.ID, track.ID)
	if !errors.Is(err, ErrNotFound) || strings.Contains(content, "目录外秘密") {
		t.Fatalf("外挂字幕越界符号链接必须按未找到处理且不得泄露内容: err=%v content=%s", err, content)
	}
}

func findTrackBySource(tracks []Track, source string) *Track {
	for index := range tracks {
		if tracks[index].Source == source {
			return &tracks[index]
		}
	}
	return nil
}

func TestUploadFourFormatsAndContent(t *testing.T) {
	samples := map[string]string{
		"srt": "1\n00:00:01,000 --> 00:00:02,000\n<script>alert(1)</script>\n第二行\n",
		"ass": "[Script Info]\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,第一行\\N第二行\n",
		"ssa": "[Script Info]\n[Events]\nFormat: Marked, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: Marked=0,0:00:01.00,0:00:02.00,Default,,0,0,0,,SSA文本\n",
		"vtt": "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n<b>VTT</b><img src=x onerror=alert(1)>\n",
	}
	for format, content := range samples {
		t.Run(format, func(t *testing.T) {
			service, db, dataDir, media := setupServiceTest(t)
			track, err := service.Upload(context.Background(), media.SpaceID, media.ID, "sample."+format, strings.NewReader(content))
			if err != nil {
				t.Fatalf("上传 %s 失败: %v", format, err)
			}
			if track.ID == "" || track.Source != SourceUploaded || track.Format != format {
				t.Fatalf("上传轨道字段错误: %#v", track)
			}
			assertSeamlessSubtitleTrack(t, track)
			vtt, err := service.Content(context.Background(), media.SpaceID, media.ID, track.ID)
			if err != nil {
				t.Fatalf("读取 %s 内容失败: %v", format, err)
			}
			if !strings.HasPrefix(vtt, "WEBVTT") || strings.Contains(strings.ToLower(vtt), "<script") || strings.Contains(strings.ToLower(vtt), "<img") {
				t.Fatalf("VTT 未安全规范化: %s", vtt)
			}
			var cacheCount int64
			if err := db.Model(&models.CacheAsset{}).Count(&cacheCount).Error; err != nil || cacheCount != 0 {
				t.Fatalf("字幕不得进入缓存登记: count=%d err=%v", cacheCount, err)
			}
			assertNoSubtitleTempFiles(t, dataDir)
		})
	}
}

func TestUploadSizeAndValidationBoundaries(t *testing.T) {
	service, _, dataDir, media := setupServiceTest(t)
	prefix := "1\n00:00:01,000 --> 00:00:02,000\n有效字幕\n"
	exact := prefix + strings.Repeat("\n", int(MaxUploadBytes)-len([]byte(prefix)))
	if _, err := service.Upload(context.Background(), media.SpaceID, media.ID, "exact.srt", strings.NewReader(exact)); err != nil {
		t.Fatalf("恰好 10 MiB 应允许: %v", err)
	}
	tooLarge := exact + "x"
	if _, err := service.Upload(context.Background(), media.SpaceID, media.ID, "large.srt", strings.NewReader(tooLarge)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("超过 10 MiB 应拒绝，实际: %v", err)
	}
	invalid := []struct {
		name    string
		content []byte
	}{
		{"../evil.srt", []byte(prefix)},
		{"empty.srt", nil},
		{"binary.srt", []byte{'1', '\n', 0, 'x'}},
		{"fake.srt", []byte("[Events]\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,伪装\n")},
		{"bad.exe", []byte(prefix)},
	}
	for _, item := range invalid {
		if _, err := service.Upload(context.Background(), media.SpaceID, media.ID, item.name, bytes.NewReader(item.content)); err == nil {
			t.Fatalf("无效上传应拒绝: %s", item.name)
		}
	}
	assertNoSubtitleTempFiles(t, dataDir)
}

func TestDeleteUploadedRestoresOnDatabaseFailureAndAuditsSuccess(t *testing.T) {
	service, db, _, media := setupServiceTest(t)
	track, err := service.Upload(context.Background(), media.SpaceID, media.ID, "delete.vtt", strings.NewReader("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n删除\n"))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	stored, err := service.StoredPath(context.Background(), media.SpaceID, media.ID, track.ID)
	if err != nil {
		t.Fatalf("解析存储路径失败: %v", err)
	}
	if err := service.Delete(context.Background(), "other-space", media.ID, track.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨 Space 删除应拒绝: %v", err)
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("跨 Space 删除不得动文件: %v", err)
	}
	if err := service.Delete(context.Background(), media.SpaceID, media.ID, track.ID); err != nil {
		t.Fatalf("删除上传字幕失败: %v", err)
	}
	if _, err := os.Stat(stored); !os.IsNotExist(err) {
		t.Fatalf("删除后文件仍存在: %v", err)
	}
	var eventCount int64
	if err := db.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "subtitle.deleted", track.ID).Count(&eventCount).Error; err != nil || eventCount != 1 {
		t.Fatalf("删除成功必须审计: count=%d err=%v", eventCount, err)
	}
}

func TestDeleteUploadedRestoresDatabasePathAndExactAuditOnRemoveFailure(t *testing.T) {
	service, db, _, media := setupServiceTest(t)
	track, err := service.Upload(context.Background(), media.SpaceID, media.ID, "restore.srt", strings.NewReader(validServiceSRT("恢复")))
	if err != nil {
		t.Fatalf("上传补偿测试字幕失败: %v", err)
	}
	stored, err := service.StoredPath(context.Background(), media.SpaceID, media.ID, track.ID)
	if err != nil {
		t.Fatalf("读取补偿测试字幕路径失败: %v", err)
	}
	concurrentID := int64(0)
	service.WithRemoveForTest(func(string) error {
		concurrent := models.AuditEvent{Scope: audit.ScopeSpace, SpaceID: &media.SpaceID, ActorType: audit.ActorSystem,
			Action: "subtitle.deleted", EventType: "subtitle.deleted", ResourceType: "subtitle", ResourceID: track.ID,
			RequestID: "concurrent-delete", CreatedAt: time.Now()}
		if createErr := db.Create(&concurrent).Error; createErr != nil {
			return createErr
		}
		concurrentID = concurrent.ID
		return errors.New("模拟 remove 失败")
	})
	if err := service.Delete(context.Background(), media.SpaceID, media.ID, track.ID); err == nil {
		t.Fatal("最终 remove 失败必须向调用方返回错误")
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("补偿后原字幕路径必须恢复: %v", err)
	}
	var restored models.MediaSubtitleTrack
	if err := db.First(&restored, "id = ?", track.ID).Error; err != nil {
		t.Fatalf("补偿后字幕记录必须恢复: %v", err)
	}
	var deletedCount, compensatedCount, concurrentCount int64
	_ = db.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "subtitle.deleted", track.ID).Count(&deletedCount).Error
	_ = db.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "subtitle.delete_compensated", track.ID).Count(&compensatedCount).Error
	_ = db.Model(&models.AuditEvent{}).Where("id = ? AND request_id = ?", concurrentID, "concurrent-delete").Count(&concurrentCount).Error
	if deletedCount != 1 || compensatedCount != 1 || concurrentCount != 1 {
		t.Fatalf("补偿必须只删除本次明确审计事件: deleted=%d compensated=%d concurrent=%d", deletedCount, compensatedCount, concurrentCount)
	}
}

func TestUploadedStoredPathRejectsNonCanonicalDatabasePath(t *testing.T) {
	service, db, _, media := setupServiceTest(t)
	track, err := service.Upload(context.Background(), media.SpaceID, media.ID, "canonical.vtt", strings.NewReader("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n规范\n"))
	if err != nil {
		t.Fatalf("上传路径测试字幕失败: %v", err)
	}
	if err := db.Exec("UPDATE media_subtitle_tracks SET storage_relative_path = ? WHERE id = ?",
		"subtitles/"+media.SpaceID+"/"+fmt.Sprint(media.ID)+"/nested/../"+track.ID+".vtt", track.ID).Error; err != nil {
		t.Fatalf("篡改测试路径失败: %v", err)
	}
	if _, err := service.Content(context.Background(), media.SpaceID, media.ID, track.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("非规范 stored path 必须拒绝: %v", err)
	}
}

func validServiceSRT(text string) string {
	return "1\n00:00:01,000 --> 00:00:02,000\n" + text + "\n"
}

func TestEmbeddedContentExtractionAndUnsupportedCodec(t *testing.T) {
	service, db, dataDir, media := setupServiceTest(t)
	normalized := `{"subtitle_streams":[{"index":3,"codec_name":"subrip","title":"文本"},{"index":4,"codec_name":"dvd_subtitle","title":"图片"}]}`
	metadata := models.MediaMetadata{MediaID: media.ID, SpaceID: media.SpaceID, Source: "ffprobe", Tool: "ffprobe", ToolVersion: "7", RawJSON: `{}`, NormalizedJSON: normalized, ParsedAt: time.Now(), Stale: false}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatalf("创建元数据失败: %v", err)
	}
	calls := 0
	service.WithExtractor(func(_ context.Context, _ string, streamIndex int, outputPath string) error {
		calls++
		if streamIndex != 3 {
			return fmt.Errorf("错误的流索引: %d", streamIndex)
		}
		return os.WriteFile(outputPath, []byte("1\n00:00:01,000 --> 00:00:02,000\n内嵌字幕\n"), 0o600)
	})
	response, err := service.List(context.Background(), media.SpaceID, media.ID)
	if err != nil {
		t.Fatalf("列轨失败: %v", err)
	}
	textTrack := findTrackByCodec(response.Tracks, "subrip")
	imageTrack := findTrackByCodec(response.Tracks, "dvd_subtitle")
	if textTrack == nil || imageTrack == nil {
		t.Fatalf("缺少内嵌轨道: %#v", response.Tracks)
	}
	for range 2 {
		vtt, err := service.Content(context.Background(), media.SpaceID, media.ID, textTrack.ID)
		if err != nil || !strings.Contains(vtt, "内嵌字幕") {
			t.Fatalf("提取内嵌字幕失败: %v %s", err, vtt)
		}
	}
	if calls != 2 {
		t.Fatalf("连续请求必须独立提取，实际调用 %d 次", calls)
	}
	if _, err := service.Content(context.Background(), media.SpaceID, media.ID, imageTrack.ID); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("图片字幕应返回结构化不支持: %v", err)
	}
	assertNoSubtitleTempFiles(t, dataDir)
}

func TestEmbeddedContentRealFFmpegIntegrationWhenAvailable(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("环境未安装 ffmpeg，跳过真实内嵌字幕集成测试")
	}
	service, db, dataDir, media := setupServiceTest(t)
	oldPath := transcoder.GetFFmpegPath()
	transcoder.SetFFmpegPath(ffmpegPath)
	t.Cleanup(func() { transcoder.SetFFmpegPath(oldPath) })
	sourceSubtitle := filepath.Join(dataDir, "embedded.srt")
	if err := os.WriteFile(sourceSubtitle, []byte(validServiceSRT("真实 FFmpeg")), 0o600); err != nil {
		t.Fatalf("创建真实字幕输入失败: %v", err)
	}
	containerPath := filepath.Join(dataDir, "embedded.mkv")
	output, err := exec.Command(ffmpegPath, "-v", "error", "-y", "-f", "srt", "-i", sourceSubtitle, "-c:s", "srt", containerPath).CombinedOutput()
	if err != nil {
		t.Fatalf("创建真实内嵌字幕容器失败: %v; %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.Remove(sourceSubtitle); err != nil {
		t.Fatalf("清理真实字幕输入失败: %v", err)
	}
	media.FilePath = containerPath
	media.FileName = filepath.Base(containerPath)
	if err := db.Save(&media).Error; err != nil {
		t.Fatalf("更新真实内嵌字幕媒体失败: %v", err)
	}
	metadata := models.MediaMetadata{MediaID: media.ID, SpaceID: media.SpaceID, Source: "ffprobe", Tool: "ffprobe", ToolVersion: "7",
		RawJSON: `{}`, NormalizedJSON: `{"subtitle_streams":[{"index":0,"codec_name":"subrip","title":"真实"}]}`, ParsedAt: time.Now(), Stale: false}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatalf("创建真实内嵌字幕元数据失败: %v", err)
	}
	response, err := service.List(context.Background(), media.SpaceID, media.ID)
	if err != nil || len(response.Tracks) != 1 {
		t.Fatalf("列出真实内嵌字幕失败: tracks=%#v err=%v", response.Tracks, err)
	}
	content, err := service.Content(context.Background(), media.SpaceID, media.ID, response.Tracks[0].ID)
	if err != nil || !strings.Contains(content, "真实 FFmpeg") {
		t.Fatalf("真实 FFmpeg 提取内容失败: err=%v content=%s", err, content)
	}
	assertNoSubtitleTempFiles(t, dataDir)
}

func TestUploadDoesNotModifyMediaOrSidecar(t *testing.T) {
	service, _, _, media := setupServiceTest(t)
	sidecar := filepath.Join(filepath.Dir(media.FilePath), "movie.srt")
	if err := os.WriteFile(sidecar, []byte("1\n00:00:01,000 --> 00:00:02,000\n原字幕\n"), 0o644); err != nil {
		t.Fatalf("创建外挂字幕失败: %v", err)
	}
	beforeMedia := fileSnapshot(t, media.FilePath)
	beforeSidecar := fileSnapshot(t, sidecar)
	if _, err := service.Upload(context.Background(), media.SpaceID, media.ID, "new.srt", strings.NewReader("1\n00:00:01,000 --> 00:00:02,000\n新字幕\n")); err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	afterMedia := fileSnapshot(t, media.FilePath)
	afterSidecar := fileSnapshot(t, sidecar)
	if beforeMedia != afterMedia || beforeSidecar != afterSidecar {
		t.Fatalf("上传修改了媒体目录源文件")
	}
}

func findTrackByCodec(tracks []Track, codec string) *Track {
	for i := range tracks {
		if tracks[i].Codec == codec {
			return &tracks[i]
		}
	}
	return nil
}

func containsTrackID(tracks []Track, id string) bool {
	for _, track := range tracks {
		if track.ID == id {
			return true
		}
	}
	return false
}

func assertNoSubtitleTempFiles(t *testing.T, dataDir string) {
	t.Helper()
	root := filepath.Join(dataDir, "subtitles")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry != nil && !entry.IsDir() && strings.Contains(entry.Name(), ".tmp-") {
			t.Errorf("发现残留临时文件: %s", path)
		}
		return nil
	})
}

func fileSnapshot(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取文件状态失败: %v", err)
	}
	return fmt.Sprintf("%x:%d", sha256.Sum256(data), info.ModTime().UnixNano())
}
