package library

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newTrendsTestService 创建带内存数据库的测试服务，迁移媒体库与媒体表。
func newTrendsTestService(t *testing.T) *Service {
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

// seedTrendsMedia 入库一条媒体并设置 added_at / 大小 / 时长，返回 ID。
// added_at 用显式 Update 写回：CreateMediaFile 落库时置 time.Now()，
// 需精确控制分桶日期，故旁路覆盖。固定本地正午，避开 'localtime' 跨日边界抖动。
func seedTrendsMedia(t *testing.T, svc *Service, name string, day time.Time, size int64, duration float64) int64 {
	t.Helper()
	mf, err := svc.CreateMediaFile(1, "D:/Media/"+name, size)
	if err != nil {
		t.Fatalf("入库媒体失败: %v", err)
	}
	addedAt := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.Local)
	if err := svc.db.Model(&models.MediaFile{}).Where("id = ?", mf.ID).Updates(map[string]any{
		"added_at":  addedAt,
		"file_size": size,
		"duration":  duration,
	}).Error; err != nil {
		t.Fatalf("设置媒体字段失败: %v", err)
	}
	return mf.ID
}

// TestGetMediaTrends_EmptyLibrary 验证空库 media_added 为非 nil 空切片。
func TestGetMediaTrends_EmptyLibrary(t *testing.T) {
	svc := newTrendsTestService(t)

	trends, err := svc.GetMediaTrends()
	if err != nil {
		t.Fatalf("查询趋势失败: %v", err)
	}
	if trends.MediaAdded == nil {
		t.Fatalf("media_added 必须为非 nil 空切片，实际为 nil")
	}
	if len(trends.MediaAdded) != 0 {
		t.Fatalf("空库期望 media_added 为空, 实际 %+v", trends.MediaAdded)
	}
}

// TestGetMediaTrends_MultiDayAscending 验证多日分桶且按日期升序。
func TestGetMediaTrends_MultiDayAscending(t *testing.T) {
	svc := newTrendsTestService(t)

	d1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	d2 := time.Date(2026, 5, 3, 0, 0, 0, 0, time.Local)
	d3 := time.Date(2026, 5, 2, 0, 0, 0, 0, time.Local)
	// 故意乱序入库，验证查询结果按日期升序
	seedTrendsMedia(t, svc, "a.mp4", d2, 100, 10)
	seedTrendsMedia(t, svc, "b.mp4", d1, 200, 20)
	seedTrendsMedia(t, svc, "c.mp4", d3, 300, 30)

	trends, err := svc.GetMediaTrends()
	if err != nil {
		t.Fatalf("查询趋势失败: %v", err)
	}
	if len(trends.MediaAdded) != 3 {
		t.Fatalf("期望 3 个分桶, 实际 %d (%+v)", len(trends.MediaAdded), trends.MediaAdded)
	}
	wantDates := []string{"2026-05-01", "2026-05-02", "2026-05-03"}
	for i, want := range wantDates {
		if trends.MediaAdded[i].Date != want {
			t.Fatalf("第 %d 个分桶日期期望 %s, 实际 %s (升序不符)", i, want, trends.MediaAdded[i].Date)
		}
	}
}

// TestGetMediaTrends_SameDayAggregation 验证同一天多条聚合 count / SUM(size) / SUM(duration)。
func TestGetMediaTrends_SameDayAggregation(t *testing.T) {
	svc := newTrendsTestService(t)

	day := time.Date(2026, 5, 10, 0, 0, 0, 0, time.Local)
	seedTrendsMedia(t, svc, "x1.mp4", day, 1000, 100)
	seedTrendsMedia(t, svc, "x2.mp4", day, 2000, 200)
	seedTrendsMedia(t, svc, "x3.mp4", day, 3000, 300)

	trends, err := svc.GetMediaTrends()
	if err != nil {
		t.Fatalf("查询趋势失败: %v", err)
	}
	if len(trends.MediaAdded) != 1 {
		t.Fatalf("同一天应聚合为 1 个分桶, 实际 %d (%+v)", len(trends.MediaAdded), trends.MediaAdded)
	}
	p := trends.MediaAdded[0]
	if p.Date != "2026-05-10" {
		t.Fatalf("分桶日期期望 2026-05-10, 实际 %s", p.Date)
	}
	if p.Count != 3 {
		t.Fatalf("当天 count 期望 3, 实际 %d", p.Count)
	}
	if p.Size != 6000 {
		t.Fatalf("当天 SUM(size) 期望 6000, 实际 %d", p.Size)
	}
	if p.Duration != 600 {
		t.Fatalf("当天 SUM(duration) 期望 600, 实际 %v", p.Duration)
	}
}

// TestGetMediaTrends_SoftDeletedExcluded 验证软删项被排除（不计入分桶）。
func TestGetMediaTrends_SoftDeletedExcluded(t *testing.T) {
	svc := newTrendsTestService(t)

	day := time.Date(2026, 5, 20, 0, 0, 0, 0, time.Local)
	seedTrendsMedia(t, svc, "keep.mp4", day, 1000, 100)
	del := seedTrendsMedia(t, svc, "gone.mp4", day, 5000, 500)
	if err := svc.DeleteMediaFile(del); err != nil {
		t.Fatalf("软删失败: %v", err)
	}

	trends, err := svc.GetMediaTrends()
	if err != nil {
		t.Fatalf("查询趋势失败: %v", err)
	}
	if len(trends.MediaAdded) != 1 {
		t.Fatalf("期望 1 个分桶, 实际 %d (%+v)", len(trends.MediaAdded), trends.MediaAdded)
	}
	p := trends.MediaAdded[0]
	if p.Count != 1 {
		t.Fatalf("软删后 count 期望 1, 实际 %d", p.Count)
	}
	if p.Size != 1000 {
		t.Fatalf("软删后 SUM(size) 期望 1000（排除软删）, 实际 %d", p.Size)
	}
	if p.Duration != 100 {
		t.Fatalf("软删后 SUM(duration) 期望 100（排除软删）, 实际 %v", p.Duration)
	}
}
