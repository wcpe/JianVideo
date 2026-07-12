package library

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

func newInferenceTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaInference{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移测试库失败: %v", err)
	}
	return NewService(db).WithAudit(audit.NewRecorder(db)), db
}

func TestInferMediaTitleParsesMovieSeriesAndChineseEpisode(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		kind     string
		title    string
		year     int
		season   int
		episode  int
		epTitle  string
		minScore float64
	}{
		{name: "英文电影年份", path: "D:/Movies/Movie.Name.2024.1080p.mkv", kind: models.LibraryKindMovie, title: "Movie Name", year: 2024, minScore: 0.8},
		{name: "英文剧集季集", path: "D:/Series/Show.Name/Show.Name.S01E02.Pilot.mp4", kind: models.LibraryKindSeries, title: "Show Name", season: 1, episode: 2, epTitle: "Pilot", minScore: 0.9},
		{name: "中文剧集季集", path: "D:/Series/长安十二时辰/长安十二时辰 第1季 第02集 灯火.mp4", kind: models.LibraryKindSeries, title: "长安十二时辰", season: 1, episode: 2, epTitle: "灯火", minScore: 0.9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferMediaTitle(InferenceInput{FilePath: tt.path, FileName: filepath.Base(tt.path), LibraryKind: tt.kind})
			if got.Title != tt.title || got.Year != tt.year || got.Season != tt.season || got.Episode != tt.episode || got.EpisodeTitle != tt.epTitle {
				t.Fatalf("推断结果不匹配: %+v", got)
			}
			if got.Confidence < tt.minScore {
				t.Fatalf("置信度过低: got %.2f want >= %.2f", got.Confidence, tt.minScore)
			}
		})
	}
}

func TestInferMediaTitleDirectoryFallbackHomeVideoAndLowConfidence(t *testing.T) {
	dirFallback := InferMediaTitle(InferenceInput{
		FilePath:    "D:/Movies/Some Film/BDRip.mkv",
		FileName:    "BDRip.mkv",
		LibraryKind: models.LibraryKindMovie,
	})
	if dirFallback.Title != "Some Film" || dirFallback.Confidence < HighConfidenceThreshold {
		t.Fatalf("目录名应作为电影标题回退: %+v", dirFallback)
	}

	homeVideo := InferMediaTitle(InferenceInput{
		FilePath:    "D:/Home/2024_生日_001.mp4",
		FileName:    "2024_生日_001.mp4",
		LibraryKind: models.LibraryKindHomeVideo,
	})
	if homeVideo.Title != "" || homeVideo.Confidence != 0 {
		t.Fatalf("家庭录像库默认不应做影视推断: %+v", homeVideo)
	}

	low := InferMediaTitle(InferenceInput{
		FilePath:    "D:/Mixed/random.clip.1080p.mp4",
		FileName:    "random.clip.1080p.mp4",
		LibraryKind: models.LibraryKindMixed,
	})
	if low.Title == "" || low.Confidence >= HighConfidenceThreshold {
		t.Fatalf("混合库弱命名应只产生低置信候选: %+v", low)
	}
}

func TestResolveInferenceDisplayNameHonorsPriorityAndLowConfidence(t *testing.T) {
	mf := models.MediaFile{FileName: "raw-name.mkv", DisplayName: ""}
	auto := &models.MediaInference{Title: "自动标题", Confidence: HighConfidenceThreshold, Source: InferenceSourceRule}
	if got := ResolveInferenceDisplayName(mf, auto); got != "自动标题" {
		t.Fatalf("高置信自动推断应作为显示名回退，实际 %q", got)
	}

	low := &models.MediaInference{Title: "低置信候选", Confidence: HighConfidenceThreshold - 0.01, Source: InferenceSourceRule}
	if got := ResolveInferenceDisplayName(mf, low); got != "raw-name.mkv" {
		t.Fatalf("低置信候选不得替换显示名，实际 %q", got)
	}

	mf.DisplayName = "库内显示名"
	if got := ResolveInferenceDisplayName(mf, auto); got != "库内显示名" {
		t.Fatalf("display_name 应优先于自动推断，实际 %q", got)
	}

	manual := &models.MediaInference{Title: "人工标题", Manual: true, Source: InferenceSourceManual}
	if got := ResolveInferenceDisplayName(mf, manual); got != "人工标题" {
		t.Fatalf("人工纠正应最高优先级，实际 %q", got)
	}
}

func TestInferAndStoreRespectsSwitchesAndManualValue(t *testing.T) {
	svc, db := newInferenceTestService(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, dir, "local", "剧集", models.LibraryKindSeries)
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	media, err := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, filepath.Join(dir, "剧名.S01E02.标题.mp4"), 10)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	got, err := svc.GetMediaInferenceInSpace(models.DefaultSpaceID, media.ID)
	if err != nil {
		t.Fatalf("扫描入库应写入推断: %v", err)
	}
	if got.Title != "剧名" || got.Season != 1 || got.Episode != 2 {
		t.Fatalf("入库推断不匹配: %+v", got)
	}

	if _, err := svc.UpsertManualInferenceInSpace(models.DefaultSpaceID, media.ID, InferenceManualInput{Title: "人工剧名", Season: 2, Episode: 3}); err != nil {
		t.Fatalf("人工纠正失败: %v", err)
	}
	if _, err := svc.BackfillMediaInferencesInSpace(models.DefaultSpaceID, lp.ID); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	manual, _ := svc.GetMediaInferenceInSpace(models.DefaultSpaceID, media.ID)
	if !manual.Manual || manual.Title != "人工剧名" || manual.Season != 2 || manual.Episode != 3 {
		t.Fatalf("backfill 不得覆盖人工纠正: %+v", manual)
	}

	svc.WithInferenceConfigProvider(func(string, int64) InferenceConfig {
		return InferenceConfig{Enabled: true, DisabledLibraries: map[int64]bool{lp.ID: true}}
	})
	disabledMedia := models.MediaFile{
		SpaceID:    models.DefaultSpaceID,
		LibraryID:  lp.ID,
		FilePath:   filepath.ToSlash(filepath.Join(dir, "Other.S01E01.mp4")),
		FileName:   "Other.S01E01.mp4",
		Format:     "mp4",
		AddedAt:    time.Now(),
		ModifiedAt: time.Now(),
	}
	if err := db.Create(&disabledMedia).Error; err != nil {
		t.Fatalf("预置媒体失败: %v", err)
	}
	if _, err := svc.InferAndStoreMediaInSpace(models.DefaultSpaceID, disabledMedia.ID); err != nil {
		t.Fatalf("关闭后推断不应报错: %v", err)
	}
	if _, err := svc.GetMediaInferenceInSpace(models.DefaultSpaceID, disabledMedia.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("每库关闭后不应产生新推断，实际 %v", err)
	}
}

func TestAutoInferenceDoesNotOverwriteConcurrentManualValue(t *testing.T) {
	svc, _ := newInferenceTestService(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, dir, "local", "电影", models.LibraryKindMovie)
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	media, err := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, filepath.Join(dir, "Auto.Movie.2024.mkv"), 10)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}

	beforeSave := make(chan struct{})
	releaseSave := make(chan struct{})
	svc.beforeAutoInferenceSave = func() {
		close(beforeSave)
		<-releaseSave
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.InferAndStoreMediaInSpace(models.DefaultSpaceID, media.ID)
		done <- err
	}()
	<-beforeSave
	if _, err := svc.UpsertManualInferenceInSpace(models.DefaultSpaceID, media.ID, InferenceManualInput{Title: "并发人工片名"}); err != nil {
		t.Fatalf("并发写入人工值失败: %v", err)
	}
	close(releaseSave)
	if err := <-done; err != nil {
		t.Fatalf("自动推断失败: %v", err)
	}
	got, err := svc.GetMediaInferenceInSpace(models.DefaultSpaceID, media.ID)
	if err != nil {
		t.Fatalf("读取最终推断失败: %v", err)
	}
	if !got.Manual || got.Title != "并发人工片名" {
		t.Fatalf("自动推断不得覆盖并发人工值: %+v", got)
	}
}

func TestListMediaFilesIncludesInferenceAndFiltersStatus(t *testing.T) {
	svc, db := newInferenceTestService(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, dir, "local", "电影", models.LibraryKindMovie)
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	auto, err := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, filepath.Join(dir, "Auto.Movie.2024.mkv"), 10)
	if err != nil {
		t.Fatalf("创建自动推断媒体失败: %v", err)
	}
	manual, err := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, filepath.Join(dir, "Manual.Movie.2025.mkv"), 10)
	if err != nil {
		t.Fatalf("创建人工推断媒体失败: %v", err)
	}
	if _, err := svc.UpsertManualInferenceInSpace(models.DefaultSpaceID, manual.ID, InferenceManualInput{Title: "人工电影"}); err != nil {
		t.Fatalf("保存人工推断失败: %v", err)
	}
	missing := models.MediaFile{
		SpaceID: models.DefaultSpaceID, LibraryID: lp.ID, FilePath: filepath.Join(dir, "Missing.mkv"),
		FileName: "Missing.mkv", Format: "mkv", AddedAt: time.Now(), ModifiedAt: time.Now(),
	}
	if err := db.Create(&missing).Error; err != nil {
		t.Fatalf("创建待推断媒体失败: %v", err)
	}

	result, err := svc.ListMediaFilesPage(MediaFilter{SpaceID: models.DefaultSpaceID, InferenceStatus: InferenceStatusManual}, MediaPageRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("按人工推断筛选失败: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != manual.ID {
		t.Fatalf("人工推断筛选结果不正确: %+v", result.Items)
	}
	if result.Items[0].Inference == nil || !result.Items[0].Inference.Manual || result.Items[0].Inference.Title != "人工电影" {
		t.Fatalf("列表应批量附带人工推断信息: %+v", result.Items[0].Inference)
	}

	result, err = svc.ListMediaFilesPage(MediaFilter{SpaceID: models.DefaultSpaceID, InferenceStatus: InferenceStatusAuto}, MediaPageRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("按自动推断筛选失败: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != auto.ID || result.Items[0].Inference == nil {
		t.Fatalf("自动推断筛选结果不正确: %+v", result.Items)
	}

	result, err = svc.ListMediaFilesPage(MediaFilter{SpaceID: models.DefaultSpaceID, InferenceStatus: InferenceStatusMissing}, MediaPageRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("按待推断筛选失败: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != missing.ID || result.Items[0].Inference != nil {
		t.Fatalf("待推断筛选结果不正确: %+v", result.Items)
	}
}

func TestBackfillMediaInferencesReportsProgressAndCancels(t *testing.T) {
	svc, _ := newInferenceTestService(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, dir, "local", "电影", models.LibraryKindMovie)
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	for _, name := range []string{"First.Movie.2024.mkv", "Second.Movie.2025.mkv"} {
		if _, err := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, filepath.Join(dir, name), 10); err != nil {
			t.Fatalf("创建媒体失败: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	var completed []int
	updated, err := svc.BackfillMediaInferencesWithProgressInSpace(ctx, models.DefaultSpaceID, lp.ID, func(done, total int, _ int64) error {
		if total != 2 {
			t.Fatalf("进度总数不正确: %d", total)
		}
		completed = append(completed, done)
		if done == 1 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应返回 context.Canceled，实际 %v", err)
	}
	if updated != 1 || len(completed) != 1 || completed[0] != 1 {
		t.Fatalf("应逐媒体报告进度并在取消后停止: updated=%d progress=%v", updated, completed)
	}
}

func TestManualInferenceRecordsAuditAndRollsBackOnAuditFailure(t *testing.T) {
	svc, db := newInferenceTestService(t)
	media := models.MediaFile{
		SpaceID:    models.DefaultSpaceID,
		LibraryID:  1,
		FilePath:   "D:/Movies/Movie.2024.mkv",
		FileName:   "Movie.2024.mkv",
		AddedAt:    time.Now(),
		ModifiedAt: time.Now(),
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("预置媒体失败: %v", err)
	}
	if _, err := svc.UpsertManualInferenceInSpace(models.DefaultSpaceID, media.ID, InferenceManualInput{Title: "人工电影", Year: 2024}); err != nil {
		t.Fatalf("人工纠正失败: %v", err)
	}
	assertAuditCount(t, db, "media.inference.updated", 1)

	failing := NewService(db).WithAudit(failingInferenceAudit{})
	if _, err := failing.UpsertManualInferenceInSpace(models.DefaultSpaceID, media.ID, InferenceManualInput{Title: "失败标题"}); err == nil {
		t.Fatal("审计失败时人工纠正应失败")
	}
	got, err := svc.GetMediaInferenceInSpace(models.DefaultSpaceID, media.ID)
	if err != nil {
		t.Fatalf("读取推断失败: %v", err)
	}
	if got.Title != "人工电影" {
		t.Fatalf("审计失败后推断应回滚，实际 %+v", got)
	}
}

type failingInferenceAudit struct{}

func (f failingInferenceAudit) Record(context.Context, audit.EventInput) error {
	return errors.New("审计写入失败")
}

func (f failingInferenceAudit) RecordTx(context.Context, *gorm.DB, audit.EventInput) error {
	return errors.New("审计写入失败")
}

func (f failingInferenceAudit) List(context.Context, audit.Query) (audit.Page, error) {
	return audit.Page{}, nil
}
