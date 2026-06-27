package library

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newSummaryTestService 创建带内存数据库的测试服务，迁移媒体库与媒体表。
func newSummaryTestService(t *testing.T) *Service {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewService(gdb)
}

// seedSummaryLibrary 入库一个媒体库目录并设置启用状态，返回库 ID。
// enabled 为 0 时需显式写回：GORM 默认会忽略 int 零值而落库列默认 1。
func seedSummaryLibrary(t *testing.T, svc *Service, label string, enabled int) int64 {
	t.Helper()
	lp := models.LibraryPath{Path: "D:/Lib/" + label, Type: "local", Label: label, Enabled: enabled}
	if err := svc.db.Create(&lp).Error; err != nil {
		t.Fatalf("入库媒体库失败: %v", err)
	}
	if err := svc.db.Model(&models.LibraryPath{}).Where("id = ?", lp.ID).Update("enabled", enabled).Error; err != nil {
		t.Fatalf("设置启用状态失败: %v", err)
	}
	return lp.ID
}

// seedSummaryMedia 入库一条媒体并设置库 ID / 格式 / 大小 / 时长，返回 ID。
// CreateMediaFile 不接受格式与时长，测试里旁路写入。
func seedSummaryMedia(t *testing.T, svc *Service, name string, libraryID int64, format string, size int64, duration float64) int64 {
	t.Helper()
	mf, err := svc.CreateMediaFile(libraryID, "D:/Media/"+name, size)
	if err != nil {
		t.Fatalf("入库媒体失败: %v", err)
	}
	if err := svc.db.Model(&models.MediaFile{}).Where("id = ?", mf.ID).Updates(map[string]any{
		"library_id": libraryID,
		"format":     format,
		"file_size":  size,
		"duration":   duration,
	}).Error; err != nil {
		t.Fatalf("设置媒体字段失败: %v", err)
	}
	return mf.ID
}

// TestGetLibrarySummary_EmptyLibrary 验证空库各项为 0、by_library 为非 nil 空切片。
func TestGetLibrarySummary_EmptyLibrary(t *testing.T) {
	svc := newSummaryTestService(t)

	summary, err := svc.GetLibrarySummary()
	if err != nil {
		t.Fatalf("查询概览失败: %v", err)
	}
	if summary.Total != 0 || summary.VideoCount != 0 || summary.ImageCount != 0 {
		t.Fatalf("空库期望计数全 0, 实际 total=%d video=%d image=%d", summary.Total, summary.VideoCount, summary.ImageCount)
	}
	if summary.TotalSize != 0 || summary.TotalDuration != 0 {
		t.Fatalf("空库期望求和全 0, 实际 size=%d duration=%v", summary.TotalSize, summary.TotalDuration)
	}
	if summary.LibraryCount != 0 {
		t.Fatalf("空库期望启用库数 0, 实际 %d", summary.LibraryCount)
	}
	if summary.ByLibrary == nil {
		t.Fatalf("by_library 必须为非 nil 空切片，实际为 nil")
	}
	if len(summary.ByLibrary) != 0 {
		t.Fatalf("空库期望 by_library 为空, 实际 %+v", summary.ByLibrary)
	}
}

// TestGetLibrarySummary_TotalsAndSplit 验证总计、视频/图片拆分口径、SUM(file_size)/SUM(duration)。
func TestGetLibrarySummary_TotalsAndSplit(t *testing.T) {
	svc := newSummaryTestService(t)
	lib := seedSummaryLibrary(t, svc, "默认库", 1)

	// 视频：mp4(时长 100, 1000B)、mkv(时长 200, 2000B)
	seedSummaryMedia(t, svc, "a.mp4", lib, "mp4", 1000, 100)
	seedSummaryMedia(t, svc, "b.mkv", lib, "mkv", 2000, 200)
	// 图片：jpg(时长 0, 300B)、png(时长 0, 400B)
	seedSummaryMedia(t, svc, "c.jpg", lib, "jpg", 300, 0)
	seedSummaryMedia(t, svc, "d.png", lib, "png", 400, 0)

	summary, err := svc.GetLibrarySummary()
	if err != nil {
		t.Fatalf("查询概览失败: %v", err)
	}
	if summary.Total != 4 {
		t.Fatalf("期望总数 4, 实际 %d", summary.Total)
	}
	if summary.VideoCount != 2 {
		t.Fatalf("期望视频 2, 实际 %d", summary.VideoCount)
	}
	if summary.ImageCount != 2 {
		t.Fatalf("期望图片 2, 实际 %d", summary.ImageCount)
	}
	if summary.TotalSize != 3700 {
		t.Fatalf("期望总大小 3700, 实际 %d", summary.TotalSize)
	}
	if summary.TotalDuration != 300 {
		t.Fatalf("期望总时长 300, 实际 %v", summary.TotalDuration)
	}
}

// TestGetLibrarySummary_SplitMatchesBuiltInList 验证视频/图片拆分与 builtInImageExtensionList 完全一致。
// 对每个内置图片后缀各造一条图片，再造一条视频；image_count 应等于内置图片后缀数。
func TestGetLibrarySummary_SplitMatchesBuiltInList(t *testing.T) {
	svc := newSummaryTestService(t)
	lib := seedSummaryLibrary(t, svc, "默认库", 1)

	imageExts := builtInImageExtensionList()
	for i, ext := range imageExts {
		seedSummaryMedia(t, svc, "img"+ext+".x", lib, ext, int64(100+i), 0)
	}
	// 一条视频（mp4 不在图片集合内）
	seedSummaryMedia(t, svc, "v.mp4", lib, "mp4", 999, 50)

	summary, err := svc.GetLibrarySummary()
	if err != nil {
		t.Fatalf("查询概览失败: %v", err)
	}
	if summary.ImageCount != len(imageExts) {
		t.Fatalf("图片数应等于内置图片后缀数 %d, 实际 %d", len(imageExts), summary.ImageCount)
	}
	if summary.VideoCount != 1 {
		t.Fatalf("期望视频 1, 实际 %d", summary.VideoCount)
	}
	if summary.Total != len(imageExts)+1 {
		t.Fatalf("期望总数 %d, 实际 %d", len(imageExts)+1, summary.Total)
	}
}

// TestGetLibrarySummary_ByLibraryGrouping 验证多库分组：各库 media/video/image/size/duration 与 label。
func TestGetLibrarySummary_ByLibraryGrouping(t *testing.T) {
	svc := newSummaryTestService(t)
	lib1 := seedSummaryLibrary(t, svc, "电影", 1)
	lib2 := seedSummaryLibrary(t, svc, "相册", 1)

	// 库 1：两条视频
	seedSummaryMedia(t, svc, "m1.mp4", lib1, "mp4", 1000, 100)
	seedSummaryMedia(t, svc, "m2.mkv", lib1, "mkv", 2000, 200)
	// 库 2：一视频 + 两图片
	seedSummaryMedia(t, svc, "p1.mp4", lib2, "mp4", 500, 50)
	seedSummaryMedia(t, svc, "p2.jpg", lib2, "jpg", 300, 0)
	seedSummaryMedia(t, svc, "p3.png", lib2, "png", 400, 0)

	summary, err := svc.GetLibrarySummary()
	if err != nil {
		t.Fatalf("查询概览失败: %v", err)
	}
	if summary.LibraryCount != 2 {
		t.Fatalf("期望启用库数 2, 实际 %d", summary.LibraryCount)
	}
	if len(summary.ByLibrary) != 2 {
		t.Fatalf("期望 by_library 2 项, 实际 %d", len(summary.ByLibrary))
	}

	rows := map[int64]LibrarySummaryRow{}
	for _, r := range summary.ByLibrary {
		rows[r.LibraryID] = r
	}
	r1, ok := rows[lib1]
	if !ok {
		t.Fatalf("缺少库 1 分组")
	}
	if r1.Label != "电影" || r1.MediaCount != 2 || r1.VideoCount != 2 || r1.ImageCount != 0 || r1.TotalSize != 3000 || r1.TotalDuration != 300 {
		t.Fatalf("库 1 分组不符: %+v", r1)
	}
	r2, ok := rows[lib2]
	if !ok {
		t.Fatalf("缺少库 2 分组")
	}
	if r2.Label != "相册" || r2.MediaCount != 3 || r2.VideoCount != 1 || r2.ImageCount != 2 || r2.TotalSize != 1200 || r2.TotalDuration != 50 {
		t.Fatalf("库 2 分组不符: %+v", r2)
	}
}

// TestGetLibrarySummary_SoftDeletedExcluded 验证软删项被排除（总计与分组均不计）。
func TestGetLibrarySummary_SoftDeletedExcluded(t *testing.T) {
	svc := newSummaryTestService(t)
	lib := seedSummaryLibrary(t, svc, "默认库", 1)

	seedSummaryMedia(t, svc, "keep.mp4", lib, "mp4", 1000, 100)
	del := seedSummaryMedia(t, svc, "gone.mp4", lib, "mp4", 5000, 500)
	if err := svc.DeleteMediaFile(del); err != nil {
		t.Fatalf("软删失败: %v", err)
	}

	summary, err := svc.GetLibrarySummary()
	if err != nil {
		t.Fatalf("查询概览失败: %v", err)
	}
	if summary.Total != 1 {
		t.Fatalf("期望总数 1（排除软删）, 实际 %d", summary.Total)
	}
	if summary.VideoCount != 1 {
		t.Fatalf("期望视频 1, 实际 %d", summary.VideoCount)
	}
	if summary.TotalSize != 1000 {
		t.Fatalf("期望总大小 1000（排除软删）, 实际 %d", summary.TotalSize)
	}
	if summary.TotalDuration != 100 {
		t.Fatalf("期望总时长 100（排除软删）, 实际 %v", summary.TotalDuration)
	}
	if len(summary.ByLibrary) != 1 || summary.ByLibrary[0].MediaCount != 1 || summary.ByLibrary[0].TotalSize != 1000 {
		t.Fatalf("分组应排除软删, 实际 %+v", summary.ByLibrary)
	}
}

// TestGetLibrarySummary_DisabledLibraryNotCounted 验证 library_count 仅计启用库（enabled=1）。
func TestGetLibrarySummary_DisabledLibraryNotCounted(t *testing.T) {
	svc := newSummaryTestService(t)
	seedSummaryLibrary(t, svc, "启用库", 1)
	seedSummaryLibrary(t, svc, "禁用库", 0)

	summary, err := svc.GetLibrarySummary()
	if err != nil {
		t.Fatalf("查询概览失败: %v", err)
	}
	if summary.LibraryCount != 1 {
		t.Fatalf("期望启用库数 1（禁用库不计）, 实际 %d", summary.LibraryCount)
	}
}
