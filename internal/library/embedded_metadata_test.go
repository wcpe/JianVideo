package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func TestParseVideoEmbeddedMetadataNormalizesStreamsAndColor(t *testing.T) {
	raw := []byte(`{
		"format":{"format_name":"matroska,webm","format_long_name":"Matroska","duration":"12.5","bit_rate":"2048000","tags":{"title":"样片","artist":"作者"}},
		"streams":[
			{"index":0,"codec_type":"video","codec_name":"h264","profile":"High","width":1920,"height":1080,"pix_fmt":"yuv420p","r_frame_rate":"30000/1001","avg_frame_rate":"24000/1001","bit_rate":"1800000","color_range":"tv","color_space":"bt709","color_transfer":"bt709","color_primaries":"bt709","disposition":{"default":1},"tags":{"language":"und","title":"主画面"}},
			{"index":1,"codec_type":"audio","codec_name":"aac","sample_rate":"48000","channels":2,"channel_layout":"stereo","bit_rate":"128000","tags":{"language":"zh","title":"国语"}},
			{"index":2,"codec_type":"audio","codec_name":"aac","sample_rate":"48000","channels":2,"tags":{"language":"en","title":"英语"}},
			{"index":3,"codec_type":"subtitle","codec_name":"subrip","disposition":{"forced":1},"tags":{"language":"zh","title":"中文字幕"}}
		]
	}`)

	parsed, err := parseVideoEmbeddedMetadata(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if parsed.Container.FormatName != "matroska,webm" || parsed.Container.Bitrate != 2048000 {
		t.Fatalf("容器规范化错误: %+v", parsed.Container)
	}
	if len(parsed.VideoStreams) != 1 || len(parsed.AudioStreams) != 2 || len(parsed.SubtitleStreams) != 1 {
		t.Fatalf("流分类数量错误: video=%d audio=%d subtitle=%d", len(parsed.VideoStreams), len(parsed.AudioStreams), len(parsed.SubtitleStreams))
	}
	video := parsed.VideoStreams[0]
	if video.FrameRate != "30000/1001" || video.AverageFrameRate != "24000/1001" || video.FrameRateFPS < 29.96 || video.FrameRateFPS > 29.98 {
		t.Fatalf("帧率规范化错误: %+v", video)
	}
	if video.Color.Space != "bt709" || video.Color.Transfer != "bt709" || video.Color.Primaries != "bt709" {
		t.Fatalf("色彩信息错误: %+v", video.Color)
	}
	if parsed.AudioStreams[0].Language != "zh" || parsed.SubtitleStreams[0].Forced != true {
		t.Fatalf("音频或字幕标签错误: %+v %+v", parsed.AudioStreams[0], parsed.SubtitleStreams[0])
	}
	if parsed.Tags["title"] != "样片" || parsed.Tags["artist"] != "作者" {
		t.Fatalf("容器标签错误: %+v", parsed.Tags)
	}
}

func TestRealMultiStreamMaterialIntegration(t *testing.T) {
	ffmpegAvailableForTest(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "multi-stream.mkv")
	subtitleZH := filepath.Join(dir, "zh.srt")
	subtitleEN := filepath.Join(dir, "en.srt")
	for filePath, content := range map[string]string{
		subtitleZH: "1\n00:00:00,000 --> 00:00:00,800\n中文字幕\n",
		subtitleEN: "1\n00:00:00,000 --> 00:00:00,800\nEnglish subtitle\n",
	} {
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			t.Fatalf("写入字幕素材失败: %v", err)
		}
	}
	args := []string{
		"-y", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=24:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=1", "-i", subtitleZH, "-i", subtitleEN,
		"-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s", "-map", "4:s",
		"-c:v", "mpeg4", "-c:a", "aac", "-c:s", "srt",
		"-metadata:s:a:0", "language=zh", "-metadata:s:a:1", "language=en",
		"-metadata:s:s:0", "language=zh", "-metadata:s:s:1", "language=en", path,
	}
	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("生成真实多流素材失败: %v\n%s", err, string(output))
	}

	svc, db := newMetadataTestService(t)
	media := createMetadataTestMediaAtPath(t, db, path)
	if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err != nil {
		t.Fatalf("解析真实多流素材失败: %v", err)
	}
	items, err := svc.ListMediaMetadata(context.Background(), media.SpaceID, media.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("查询真实素材元数据失败: count=%d err=%v", len(items), err)
	}
	if items[0].ToolVersion == "" || items[0].ToolVersion == "unknown" {
		t.Fatalf("真实 ffprobe 解析应记录工具版本: %+v", items[0])
	}
	var normalized NormalizedEmbeddedMetadata
	if err := json.Unmarshal([]byte(items[0].NormalizedJSON), &normalized); err != nil {
		t.Fatalf("解析真实素材规范化 JSON 失败: %v", err)
	}
	if len(normalized.VideoStreams) != 1 || len(normalized.AudioStreams) != 2 || len(normalized.SubtitleStreams) != 2 {
		t.Fatalf("真实素材流数量错误: video=%d audio=%d subtitle=%d", len(normalized.VideoStreams), len(normalized.AudioStreams), len(normalized.SubtitleStreams))
	}
	if normalized.VideoStreams[0].FrameRate == "" || normalized.AudioStreams[0].Language != "zh" || normalized.SubtitleStreams[1].Language != "en" {
		t.Fatalf("真实素材流字段错误: %+v", normalized)
	}
}

func TestRealVariableFrameRateMaterialIntegration(t *testing.T) {
	ffmpegAvailableForTest(t)
	path := filepath.Join(t.TempDir(), "variable-frame-rate.mkv")
	args := []string{
		"-y",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=24:duration=1",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=30:duration=1",
		"-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[v]",
		"-map", "[v]", "-fps_mode", "vfr", "-c:v", "ffv1", path,
	}
	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("生成真实可变帧率素材失败: %v\n%s", err, string(output))
	}

	parsed, err := probeEmbeddedVideoMetadata(context.Background(), path)
	if err != nil {
		t.Fatalf("解析真实可变帧率素材失败: %v", err)
	}
	if len(parsed.Normalized.VideoStreams) != 1 {
		t.Fatalf("可变帧率素材应有一个视频流: %+v", parsed.Normalized.VideoStreams)
	}
	stream := parsed.Normalized.VideoStreams[0]
	if stream.FrameRate == "" || stream.AverageFrameRate == "" || stream.FrameRate == stream.AverageFrameRate {
		t.Fatalf("应保留可变帧率的标称与平均帧率差异: %+v", stream)
	}
}

func TestExtractImageSupplementalMetadataReadsXMPAndIPTC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.jpg")
	jpeg := validJPEGWithXMPAndIPTC(t)
	if err := os.WriteFile(path, jpeg, 0o644); err != nil {
		t.Fatalf("写入图片失败: %v", err)
	}

	got, err := extractImageSupplementalMetadata(path)
	if err != nil {
		t.Fatalf("提取失败: %v", err)
	}
	if got.XMP["dc:title"] != "XMP 标题" || got.XMP["dc:creator"] != "XMP 作者" {
		t.Fatalf("XMP 字段错误: %+v", got.XMP)
	}
	if got.IPTC["object_name"] != "IPTC 标题" || got.IPTC["byline"] != "IPTC 作者" {
		t.Fatalf("IPTC 字段错误: %+v", got.IPTC)
	}
}

func TestMediaMetadataUpsertStaleAndUniqueSource(t *testing.T) {
	svc, db := newMetadataTestService(t)
	media := createMetadataTestMedia(t, db, "clip.mp4")
	calls := 0
	svc.metadataParser = func(context.Context, models.MediaFile) (ParsedEmbeddedMetadata, error) {
		calls++
		return ParsedEmbeddedMetadata{Source: MetadataSourceFFprobe, Tool: "ffprobe", ToolVersion: "7.1", RawJSON: `{"call":1}`, NormalizedJSON: `{"title":"第` + string(rune('0'+calls)) + `次"}`}, nil
	}

	if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err != nil {
		t.Fatalf("首次解析失败: %v", err)
	}
	if err := svc.MarkMediaMetadataStale(context.Background(), media.SpaceID, media.ID); err != nil {
		t.Fatalf("标记过期失败: %v", err)
	}
	if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err != nil {
		t.Fatalf("刷新解析失败: %v", err)
	}

	items, err := svc.ListMediaMetadata(context.Background(), media.SpaceID, media.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("同一来源应只保留一条，实际 %d", len(items))
	}
	if items[0].Stale || !strings.Contains(items[0].NormalizedJSON, "第2次") {
		t.Fatalf("刷新结果错误: %+v", items[0])
	}
}

func TestMetadataParseDoesNotModifySourceHashOrMtime(t *testing.T) {
	ffmpegAvailableForTest(t)
	svc, db := newMetadataTestService(t)
	path := filepath.Join(t.TempDir(), "source.mp4")
	generateTestVideoForLibrary(t, path)
	beforeHash := fileSHA256(t, path)
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取源文件失败: %v", err)
	}
	media := createMetadataTestMediaAtPath(t, db, path)
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Updates(map[string]any{
		"content_hash": beforeHash, "content_hash_algo": ContentHashAlgoSHA256,
		"content_hash_computed_at": time.Now(), "content_hash_stale": false,
	}).Error; err != nil {
		t.Fatalf("写入源文件哈希失败: %v", err)
	}

	if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("再次读取源文件失败: %v", err)
	}
	if got := fileSHA256(t, path); got != beforeHash {
		t.Fatalf("源文件 hash 被修改: before=%s after=%s", beforeHash, got)
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("源文件 mtime 被修改: before=%v after=%v", beforeInfo.ModTime(), afterInfo.ModTime())
	}
	items, err := svc.ListMediaMetadata(context.Background(), media.SpaceID, media.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("查询持久化元数据失败: count=%d err=%v", len(items), err)
	}
	var normalized NormalizedEmbeddedMetadata
	if err := json.Unmarshal([]byte(items[0].NormalizedJSON), &normalized); err != nil {
		t.Fatalf("解析规范化元数据失败: %v", err)
	}
	if normalized.FileHash != beforeHash || !normalized.FileMTime.Equal(beforeInfo.ModTime()) {
		t.Fatalf("文件指纹未持久化: hash=%s mtime=%v", normalized.FileHash, normalized.FileMTime)
	}
}

func TestScanChangeMarksMetadataStaleAndEnqueuesRefresh(t *testing.T) {
	svc, db := newMetadataTestService(t)
	path := filepath.Join(t.TempDir(), "changed.jpg")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	media := createMetadataTestMediaAtPath(t, db, path)
	if err := db.Create(&models.MediaMetadata{
		MediaID: media.ID, SpaceID: media.SpaceID, Source: MetadataSourceImage,
		Tool: "imagemeta+stdlib", ToolVersion: "1", RawJSON: `{}`, NormalizedJSON: `{}`,
		ParsedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("写入旧元数据失败: %v", err)
	}
	tasks := tasksvc.NewService(db)
	svc.WithScanChangeHook(svc.MetadataScanChangeHook(tasks, nil))
	if err := os.WriteFile(path, []byte("new-content"), 0o644); err != nil {
		t.Fatalf("修改测试文件失败: %v", err)
	}

	if _, err := svc.ApplyScanChange(ScanChange{
		SpaceID: media.SpaceID, LibraryID: media.LibraryID, Path: path, Op: ScanChangeModified,
	}); err != nil {
		t.Fatalf("应用扫描变化失败: %v", err)
	}
	items, err := svc.ListMediaMetadata(context.Background(), media.SpaceID, media.ID)
	if err != nil || len(items) != 1 || !items[0].Stale {
		t.Fatalf("文件变化后元数据应 stale: items=%+v err=%v", items, err)
	}
	var count int64
	if err := db.Model(&models.Task{}).Where("type = ? AND resource_id = ?", TaskTypeMetadataParse, strconv.FormatInt(media.ID, 10)).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("应幂等入队单文件刷新: count=%d err=%v", count, err)
	}
}

func TestScanAddEnqueuesMetadataParse(t *testing.T) {
	svc, db := newMetadataTestService(t)
	tasks := tasksvc.NewService(db)
	svc.WithScanChangeHook(svc.MetadataScanChangeHook(tasks, nil))
	path := filepath.Join(t.TempDir(), "added.jpg")
	if err := os.WriteFile(path, validJPEGWithXMPAndIPTC(t), 0o644); err != nil {
		t.Fatalf("写入真实图片素材失败: %v", err)
	}

	media, err := svc.CreateMediaFileInSpace(models.DefaultSpaceID, 1, path, int64(len(validJPEGWithXMPAndIPTC(t))))
	if err != nil {
		t.Fatalf("扫描入库失败: %v", err)
	}
	var count int64
	if err := db.Model(&models.Task{}).Where("type = ? AND resource_id = ?", TaskTypeMetadataParse, strconv.FormatInt(media.ID, 10)).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("新增媒体应幂等入队元数据解析: count=%d err=%v", count, err)
	}
}

func TestMetadataParseTaskUsesFileFingerprintGeneration(t *testing.T) {
	svc, db := newMetadataTestService(t)
	media := createMetadataTestMedia(t, db, "changing.jpg")
	calls := 0
	svc.metadataParser = func(context.Context, models.MediaFile) (ParsedEmbeddedMetadata, error) {
		calls++
		return ParsedEmbeddedMetadata{Source: MetadataSourceImage, Tool: "imagemeta+stdlib", ToolVersion: "1", RawJSON: `{}`, NormalizedJSON: `{}`}, nil
	}
	tasks := tasksvc.NewService(db)
	oldTask, err := EnqueueMetadataParse(context.Background(), tasks, media)
	if err != nil {
		t.Fatalf("旧指纹任务入队失败: %v", err)
	}
	newModifiedAt := media.ModifiedAt.Add(time.Minute)
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Updates(map[string]any{
		"file_size": media.FileSize + 1, "modified_at": newModifiedAt,
	}).Error; err != nil {
		t.Fatalf("更新媒体指纹失败: %v", err)
	}
	var changed models.MediaFile
	if err := db.First(&changed, media.ID).Error; err != nil {
		t.Fatalf("读取新指纹媒体失败: %v", err)
	}
	newTask, err := EnqueueMetadataParse(context.Background(), tasks, changed)
	if err != nil {
		t.Fatalf("新指纹任务入队失败: %v", err)
	}
	if newTask.ID == oldTask.ID {
		t.Fatal("文件指纹变化后应创建新的解析任务")
	}
	if err := metadataParseHandler(svc)(context.Background(), *oldTask); err != nil {
		t.Fatalf("过期解析任务应安全结束: %v", err)
	}
	if calls != 0 {
		t.Fatalf("过期指纹任务不应执行解析，实际调用 %d 次", calls)
	}
}

func TestRemovedScanChangeMarksMetadataStale(t *testing.T) {
	svc, db := newMetadataTestService(t)
	path := filepath.Join(t.TempDir(), "removed.jpg")
	if err := os.WriteFile(path, []byte("source"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	media := createMetadataTestMediaAtPath(t, db, path)
	if err := db.Create(&models.MediaMetadata{
		MediaID: media.ID, SpaceID: media.SpaceID, Source: MetadataSourceImage,
		Tool: "imagemeta+stdlib", ToolVersion: "1", RawJSON: `{}`, NormalizedJSON: `{}`, ParsedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("写入元数据失败: %v", err)
	}
	svc.WithScanChangeHook(svc.MetadataScanChangeHook(nil, nil))
	if err := os.Remove(path); err != nil {
		t.Fatalf("删除测试文件失败: %v", err)
	}
	if _, err := svc.ApplyScanChange(ScanChange{SpaceID: media.SpaceID, LibraryID: media.LibraryID, Path: path, Op: ScanChangeRemoved}); err != nil {
		t.Fatalf("应用删除变化失败: %v", err)
	}
	var item models.MediaMetadata
	if err := db.Where("media_id = ?", media.ID).First(&item).Error; err != nil {
		t.Fatalf("读取元数据失败: %v", err)
	}
	if !item.Stale {
		t.Fatal("源文件缺失后元数据应标记为 stale")
	}
}

func TestMetadataBackfillAutomaticRetryKeepsCheckpoint(t *testing.T) {
	svc, db := newMetadataTestService(t)
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		createMetadataTestMedia(t, db, name)
	}
	svc.metadataParser = func(_ context.Context, media models.MediaFile) (ParsedEmbeddedMetadata, error) {
		if media.FileName == "b.jpg" {
			return ParsedEmbeddedMetadata{}, errors.New("模拟解析失败")
		}
		return ParsedEmbeddedMetadata{Source: MetadataSourceImage, Tool: "imagemeta+stdlib", ToolVersion: "1", RawJSON: `{}`, NormalizedJSON: `{}`}, nil
	}
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	if err := RegisterMetadataWorkers(workers, tasks, svc); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}
	task, err := EnqueueMetadataBackfill(context.Background(), tasks, models.DefaultSpaceID, 0)
	if err != nil {
		t.Fatalf("回填入队失败: %v", err)
	}

	for attempt := 1; attempt <= 3; attempt++ {
		if err := workers.RunPending(context.Background()); err != nil {
			t.Fatalf("第 %d 次执行 worker 失败: %v", attempt, err)
		}
		got, getErr := tasks.Get(context.Background(), task.ID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
		if getErr != nil {
			t.Fatalf("查询任务失败: %v", getErr)
		}
		if got.Checkpoint != "media:1" || got.Attempts != attempt {
			t.Fatalf("自动重试应保留最后成功 checkpoint: attempt=%d task=%+v", attempt, got)
		}
		if attempt < 3 {
			if got.Status != models.TaskStatusPending {
				t.Fatalf("剩余重试次数时任务应回 pending: %+v", got)
			}
			if err := db.Model(&models.Task{}).Where("id = ?", task.ID).Update("next_run_at", nil).Error; err != nil {
				t.Fatalf("推进重试时间失败: %v", err)
			}
		} else if got.Status != models.TaskStatusFailed {
			t.Fatalf("三次失败后任务应终态 failed: %+v", got)
		}
	}
}

func TestMetadataBackfillTaskPersistsCheckpoint(t *testing.T) {
	svc, db := newMetadataTestService(t)
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		createMetadataTestMedia(t, db, name)
	}
	svc.metadataParser = func(_ context.Context, media models.MediaFile) (ParsedEmbeddedMetadata, error) {
		return ParsedEmbeddedMetadata{Source: MetadataSourceImage, Tool: "imagemeta+stdlib", ToolVersion: "1", RawJSON: `{}`, NormalizedJSON: `{"media_id":` + metadataInt64(media.ID) + `}`}, nil
	}
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	if err := RegisterMetadataWorkers(workers, tasks, svc); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}
	task, err := EnqueueMetadataBackfill(context.Background(), tasks, models.DefaultSpaceID, 0)
	if err != nil {
		t.Fatalf("回填入队失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行 worker 失败: %v", err)
	}
	got, err := tasks.Get(context.Background(), task.ID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if got.Status != models.TaskStatusSucceeded || got.Progress != 100 || got.Checkpoint != "media:3" {
		t.Fatalf("任务终态或 checkpoint 错误: %+v", got)
	}
}

func newMetadataTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.MediaFile{}, &models.MediaMetadata{}, &models.Task{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewService(db), db
}

func createMetadataTestMedia(t *testing.T, db *gorm.DB, name string) models.MediaFile {
	t.Helper()
	return createMetadataTestMediaAtPath(t, db, filepath.ToSlash(filepath.Join(t.TempDir(), name)))
}

func createMetadataTestMediaAtPath(t *testing.T, db *gorm.DB, path string) models.MediaFile {
	t.Helper()
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: filepath.ToSlash(path), FileName: filepath.Base(path), Format: strings.TrimPrefix(filepath.Ext(path), "."), FileState: models.MediaFileStateAvailable, AddedAt: time.Now(), ModifiedAt: time.Now()}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	return media
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func metadataInt64(value int64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[value%10]
		value /= 10
	}
	return string(buf[i:])
}

func validJPEGWithXMPAndIPTC(t *testing.T) []byte {
	t.Helper()
	imageData := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			imageData.Set(x, y, color.RGBA{R: uint8(x * 24), G: uint8(y * 24), B: 96, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, imageData, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("编码真实 JPEG 素材失败: %v", err)
	}

	xmp := []byte(`http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>XMP 标题</dc:title><dc:creator>XMP 作者</dc:creator></rdf:Description></rdf:RDF></x:xmpmeta>`)
	xmp = []byte(strings.ReplaceAll(string(xmp), `\x00`, string([]byte{0})))
	iptc := append([]byte("Photoshop 3.0\x00"), []byte{0x1c, 0x02, 0x05, 0x00, 0x0b}...)
	iptc = append(iptc, []byte("IPTC 标题")...)
	iptc = append(iptc, []byte{0x1c, 0x02, 0x50, 0x00, 0x0b}...)
	iptc = append(iptc, []byte("IPTC 作者")...)
	segment := func(marker byte, payload []byte) []byte {
		length := len(payload) + 2
		return append([]byte{0xff, marker, byte(length >> 8), byte(length)}, payload...)
	}
	data := append([]byte{}, encoded.Bytes()[:2]...)
	data = append(data, segment(0xe1, xmp)...)
	data = append(data, segment(0xed, iptc)...)
	return append(data, encoded.Bytes()[2:]...)
}
