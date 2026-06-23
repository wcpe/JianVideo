package library

import (
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// seedStatsMedia 入库一条媒体并设置库 ID / 格式 / 时长，返回 ID。
// CreateMediaFile 不接受这些字段，测试里旁路写入。
func seedStatsMedia(t *testing.T, svc *Service, name string, libraryID int64, format string, duration float64) int64 {
	t.Helper()
	mf, err := svc.CreateMediaFile(libraryID, "D:/Videos/"+name, 1024)
	if err != nil {
		t.Fatalf("入库媒体失败: %v", err)
	}
	if err := svc.db.Model(&models.MediaFile{}).Where("id = ?", mf.ID).Updates(map[string]any{
		"library_id": libraryID,
		"format":     format,
		"duration":   duration,
	}).Error; err != nil {
		t.Fatalf("设置媒体字段失败: %v", err)
	}
	return mf.ID
}

// TestMarkWatchedIncrementsViewCount 验证看完时观看次数 +1，位置上报不计数。
func TestMarkWatchedIncrementsViewCount(t *testing.T) {
	svc := newWatchTestService(t)
	id := seedWatchMedia(t, svc, "a.mp4")

	// 多次位置上报不应增加观看次数
	for i := 0; i < 3; i++ {
		if _, err := svc.UpdateWatchPosition(id, float64(10*(i+1))); err != nil {
			t.Fatalf("上报位置失败: %v", err)
		}
	}
	mf, err := svc.GetMediaFileByID(id)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if mf.ViewCount != 0 {
		t.Fatalf("位置上报不应计数, 期望 view_count=0, 实际 %d", mf.ViewCount)
	}

	// 看完一次 +1
	if _, err := svc.MarkWatched(id); err != nil {
		t.Fatalf("标记已看失败: %v", err)
	}
	mf, _ = svc.GetMediaFileByID(id)
	if mf.ViewCount != 1 {
		t.Fatalf("看完一次后期望 view_count=1, 实际 %d", mf.ViewCount)
	}

	// 再看完一次 +1（累加）
	if _, err := svc.MarkWatched(id); err != nil {
		t.Fatalf("第二次标记已看失败: %v", err)
	}
	mf, _ = svc.GetMediaFileByID(id)
	if mf.ViewCount != 2 {
		t.Fatalf("看完两次后期望 view_count=2, 实际 %d", mf.ViewCount)
	}
}

// TestGetWatchStats_WatchedCounts 验证已看/未看/总数计数（排除软删）。
func TestGetWatchStats_WatchedCounts(t *testing.T) {
	svc := newWatchTestService(t)
	idA := seedStatsMedia(t, svc, "a.mp4", 1, "mp4", 100)
	idB := seedStatsMedia(t, svc, "b.mp4", 1, "mp4", 100)
	seedStatsMedia(t, svc, "c.mp4", 1, "mkv", 100) // 未看
	idDel := seedStatsMedia(t, svc, "d.mp4", 1, "mp4", 100)

	if _, err := svc.MarkWatched(idA); err != nil {
		t.Fatalf("标记 A 失败: %v", err)
	}
	if _, err := svc.MarkWatched(idB); err != nil {
		t.Fatalf("标记 B 失败: %v", err)
	}
	// 软删的看完媒体不应计入
	if _, err := svc.MarkWatched(idDel); err != nil {
		t.Fatalf("标记 D 失败: %v", err)
	}
	if err := svc.DeleteMediaFile(idDel); err != nil {
		t.Fatalf("软删 D 失败: %v", err)
	}

	stats, err := svc.GetWatchStats()
	if err != nil {
		t.Fatalf("查询统计失败: %v", err)
	}
	if stats.Total != 3 {
		t.Fatalf("期望总数 3（排除软删）, 实际 %d", stats.Total)
	}
	if stats.Watched != 2 {
		t.Fatalf("期望已看 2, 实际 %d", stats.Watched)
	}
	if stats.Unwatched != 1 {
		t.Fatalf("期望未看 1, 实际 %d", stats.Unwatched)
	}
}

// TestGetWatchStats_PositionHeatmap 验证续播位置比例分桶（10 档）。
func TestGetWatchStats_PositionHeatmap(t *testing.T) {
	svc := newWatchTestService(t)
	// duration=100：位置 5→第 0 档(0-10%)、55→第 5 档(50-60%)、95→第 9 档(90-100%)
	id1 := seedStatsMedia(t, svc, "p1.mp4", 1, "mp4", 100)
	id2 := seedStatsMedia(t, svc, "p2.mp4", 1, "mp4", 100)
	id3 := seedStatsMedia(t, svc, "p3.mp4", 1, "mp4", 100)
	// duration=0 不计入热力
	id4 := seedStatsMedia(t, svc, "p4.mp4", 1, "mp4", 0)

	if _, err := svc.UpdateWatchPosition(id1, 5); err != nil {
		t.Fatalf("上报 1 失败: %v", err)
	}
	if _, err := svc.UpdateWatchPosition(id2, 55); err != nil {
		t.Fatalf("上报 2 失败: %v", err)
	}
	if _, err := svc.UpdateWatchPosition(id3, 95); err != nil {
		t.Fatalf("上报 3 失败: %v", err)
	}
	if _, err := svc.UpdateWatchPosition(id4, 50); err != nil {
		t.Fatalf("上报 4 失败: %v", err)
	}

	stats, err := svc.GetWatchStats()
	if err != nil {
		t.Fatalf("查询统计失败: %v", err)
	}
	if len(stats.PositionHeatmap) != 10 {
		t.Fatalf("期望 10 档热力, 实际 %d", len(stats.PositionHeatmap))
	}
	if stats.PositionHeatmap[0] != 1 || stats.PositionHeatmap[5] != 1 || stats.PositionHeatmap[9] != 1 {
		t.Fatalf("期望第 0/5/9 档各 1, 实际 %v", stats.PositionHeatmap)
	}
	// duration=0 的不应落入任何档（合计仅 3）
	total := 0
	for _, v := range stats.PositionHeatmap {
		total += v
	}
	if total != 3 {
		t.Fatalf("期望热力合计 3（排除 duration=0）, 实际 %d", total)
	}
}

// TestGetWatchStats_RecentTimeline 验证最近观看时间线按天分桶。
func TestGetWatchStats_RecentTimeline(t *testing.T) {
	svc := newWatchTestService(t)
	id1 := seedStatsMedia(t, svc, "t1.mp4", 1, "mp4", 100)
	id2 := seedStatsMedia(t, svc, "t2.mp4", 1, "mp4", 100)
	if _, err := svc.MarkWatched(id1); err != nil {
		t.Fatalf("标记 1 失败: %v", err)
	}
	if _, err := svc.MarkWatched(id2); err != nil {
		t.Fatalf("标记 2 失败: %v", err)
	}

	stats, err := svc.GetWatchStats()
	if err != nil {
		t.Fatalf("查询统计失败: %v", err)
	}
	// 今天的本地日期应出现在时间线中、计数为 2
	today := time.Now().Format("2006-01-02")
	var found bool
	for _, b := range stats.RecentTimeline {
		if b.Date == today {
			found = true
			if b.Count != 2 {
				t.Fatalf("期望今天计数 2, 实际 %d", b.Count)
			}
		}
	}
	if !found {
		t.Fatalf("期望时间线包含今天 %s, 实际 %+v", today, stats.RecentTimeline)
	}
}

// TestGetWatchStats_ByLibraryAndFormat 验证各库/各格式观看分布。
func TestGetWatchStats_ByLibraryAndFormat(t *testing.T) {
	svc := newWatchTestService(t)
	// 库 1：mp4 两条已看；库 2：mkv 一条已看
	a := seedStatsMedia(t, svc, "a.mp4", 1, "mp4", 100)
	b := seedStatsMedia(t, svc, "b.mp4", 1, "mp4", 100)
	c := seedStatsMedia(t, svc, "c.mkv", 2, "mkv", 100)
	for _, id := range []int64{a, b, c} {
		if _, err := svc.MarkWatched(id); err != nil {
			t.Fatalf("标记失败: %v", err)
		}
	}

	stats, err := svc.GetWatchStats()
	if err != nil {
		t.Fatalf("查询统计失败: %v", err)
	}
	libCounts := map[int64]int{}
	for _, l := range stats.ByLibrary {
		libCounts[l.LibraryID] = l.Watched
	}
	if libCounts[1] != 2 || libCounts[2] != 1 {
		t.Fatalf("期望库 1=2、库 2=1, 实际 %+v", stats.ByLibrary)
	}
	fmtCounts := map[string]int{}
	for _, f := range stats.ByFormat {
		fmtCounts[f.Format] = f.Watched
	}
	if fmtCounts["mp4"] != 2 || fmtCounts["mkv"] != 1 {
		t.Fatalf("期望 mp4=2、mkv=1, 实际 %+v", stats.ByFormat)
	}
}

// TestGetWatchStats_TopViewed 验证观看次数 Top N 倒序。
func TestGetWatchStats_TopViewed(t *testing.T) {
	svc := newWatchTestService(t)
	idA := seedStatsMedia(t, svc, "a.mp4", 1, "mp4", 100)
	idB := seedStatsMedia(t, svc, "b.mp4", 1, "mp4", 100)
	seedStatsMedia(t, svc, "c.mp4", 1, "mp4", 100) // view_count=0，不应出现

	// A 看完 3 次，B 看完 1 次
	for i := 0; i < 3; i++ {
		if _, err := svc.MarkWatched(idA); err != nil {
			t.Fatalf("标记 A 失败: %v", err)
		}
	}
	if _, err := svc.MarkWatched(idB); err != nil {
		t.Fatalf("标记 B 失败: %v", err)
	}

	stats, err := svc.GetWatchStats()
	if err != nil {
		t.Fatalf("查询统计失败: %v", err)
	}
	if len(stats.TopViewed) != 2 {
		t.Fatalf("期望 Top 2 条（排除 view_count=0）, 实际 %d", len(stats.TopViewed))
	}
	if stats.TopViewed[0].ID != idA || stats.TopViewed[0].ViewCount != 3 {
		t.Fatalf("期望榜首 A(view_count=3), 实际 %+v", stats.TopViewed[0])
	}
	if stats.TopViewed[1].ID != idB || stats.TopViewed[1].ViewCount != 1 {
		t.Fatalf("期望次席 B(view_count=1), 实际 %+v", stats.TopViewed[1])
	}
}
