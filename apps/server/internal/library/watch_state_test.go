package library

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newWatchTestService 创建带内存数据库的测试服务，迁移媒体表。
func newWatchTestService(t *testing.T) *Service {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.WatchState{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewService(gdb)
}

// seedWatchMedia 入库一条媒体记录并返回 ID。
func seedWatchMedia(t *testing.T, svc *Service, name string) int64 {
	t.Helper()
	mf, err := svc.CreateMediaFile(1, "D:/Videos/"+name, 1024)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	return mf.ID
}

func TestUpdateWatchPosition(t *testing.T) {
	svc := newWatchTestService(t)
	id := seedWatchMedia(t, svc, "a.mp4")

	mf, err := svc.UpdateWatchPosition(id, 42.5)
	if err != nil {
		t.Fatalf("上报位置失败: %v", err)
	}
	if mf.LastPosition != 42.5 {
		t.Fatalf("期望 last_position=42.5, 实际 %v", mf.LastPosition)
	}
	if mf.LastWatchedAt == nil {
		t.Fatalf("期望 last_watched_at 非空")
	}
	if mf.Watched {
		t.Fatalf("仅上报位置不应标记已看")
	}
}

func TestUpdateWatchPosition_NegativeClampedToZero(t *testing.T) {
	svc := newWatchTestService(t)
	id := seedWatchMedia(t, svc, "a.mp4")

	mf, err := svc.UpdateWatchPosition(id, -10)
	if err != nil {
		t.Fatalf("上报位置失败: %v", err)
	}
	if mf.LastPosition != 0 {
		t.Fatalf("期望负值归零, 实际 %v", mf.LastPosition)
	}
}

func TestUpdateWatchPosition_NotFound(t *testing.T) {
	svc := newWatchTestService(t)
	if _, err := svc.UpdateWatchPosition(999, 1.0); err == nil {
		t.Fatalf("期望媒体不存在时报错")
	}
}

func TestMarkWatched(t *testing.T) {
	svc := newWatchTestService(t)
	id := seedWatchMedia(t, svc, "a.mp4")

	// 先有进度，再标记已看应清零续播位置
	if _, err := svc.UpdateWatchPosition(id, 30); err != nil {
		t.Fatalf("上报位置失败: %v", err)
	}

	mf, err := svc.MarkWatched(id)
	if err != nil {
		t.Fatalf("标记已看失败: %v", err)
	}
	if !mf.Watched {
		t.Fatalf("期望 watched=true")
	}
	if mf.LastPosition != 0 {
		t.Fatalf("期望标记已看后 last_position 清零, 实际 %v", mf.LastPosition)
	}
	if mf.LastWatchedAt == nil {
		t.Fatalf("期望 last_watched_at 非空")
	}
}

func TestMarkWatched_NotFound(t *testing.T) {
	svc := newWatchTestService(t)
	if _, err := svc.MarkWatched(999); err == nil {
		t.Fatalf("期望媒体不存在时报错")
	}
}

func TestListContinueWatching(t *testing.T) {
	svc := newWatchTestService(t)
	idA := seedWatchMedia(t, svc, "a.mp4") // 有进度未看完
	idB := seedWatchMedia(t, svc, "b.mp4") // 有进度未看完，但观看更晚
	idC := seedWatchMedia(t, svc, "c.mp4") // 已看完，不应出现
	seedWatchMedia(t, svc, "d.mp4")        // 无进度，不应出现

	if _, err := svc.UpdateWatchPosition(idA, 10); err != nil {
		t.Fatalf("上报 A 失败: %v", err)
	}
	// 拉开 last_watched_at 时间差，确保 B 比 A 更晚
	time.Sleep(10 * time.Millisecond)
	if _, err := svc.UpdateWatchPosition(idB, 20); err != nil {
		t.Fatalf("上报 B 失败: %v", err)
	}
	if _, err := svc.UpdateWatchPosition(idC, 5); err != nil {
		t.Fatalf("上报 C 失败: %v", err)
	}
	if _, err := svc.MarkWatched(idC); err != nil {
		t.Fatalf("标记 C 已看失败: %v", err)
	}

	items, err := svc.ListContinueWatching(12)
	if err != nil {
		t.Fatalf("查询继续观看失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 条继续观看, 实际 %d", len(items))
	}
	// 按最近观看倒序：B 在前，A 在后
	if items[0].ID != idB || items[1].ID != idA {
		t.Fatalf("期望按最近观看倒序 [B, A], 实际 [%d, %d]", items[0].ID, items[1].ID)
	}
}

func TestListContinueWatching_LimitAndDeleted(t *testing.T) {
	svc := newWatchTestService(t)
	id1 := seedWatchMedia(t, svc, "1.mp4")
	id2 := seedWatchMedia(t, svc, "2.mp4")
	id3 := seedWatchMedia(t, svc, "3.mp4")
	for _, id := range []int64{id1, id2, id3} {
		if _, err := svc.UpdateWatchPosition(id, 5); err != nil {
			t.Fatalf("上报失败: %v", err)
		}
	}

	// limit 生效
	items, err := svc.ListContinueWatching(2)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 limit=2 返回 2 条, 实际 %d", len(items))
	}

	// 删除的媒体不应出现在继续观看
	if err := svc.DeleteMediaFile(id1); err != nil {
		t.Fatalf("删除媒体失败: %v", err)
	}
	items, err = svc.ListContinueWatching(12)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	for _, it := range items {
		if it.ID == id1 {
			t.Fatalf("已删除媒体不应出现在继续观看列表")
		}
	}
}
