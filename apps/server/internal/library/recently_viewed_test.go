package library

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestSetMediaViewed(t *testing.T) {
	svc := newWatchTestService(t)
	id := seedWatchMedia(t, svc, "a.jpg")

	before := time.Now()
	if err := svc.SetMediaViewed(id); err != nil {
		t.Fatalf("记录最近查看失败: %v", err)
	}

	mf, err := svc.GetMediaFileByID(id)
	if err != nil {
		t.Fatalf("读取媒体失败: %v", err)
	}
	if mf.LastViewedAt == nil {
		t.Fatalf("期望 last_viewed_at 非空")
	}
	if mf.LastViewedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("期望 last_viewed_at 为当前时间附近, 实际 %v", mf.LastViewedAt)
	}
}

func TestSetMediaViewed_NotFound(t *testing.T) {
	svc := newWatchTestService(t)
	err := svc.SetMediaViewed(999)
	if err == nil {
		t.Fatalf("期望媒体不存在时报错")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("期望返回 gorm.ErrRecordNotFound, 实际 %v", err)
	}
}

func TestSetMediaViewed_PicksUpInRecentlyViewed(t *testing.T) {
	svc := newWatchTestService(t)
	id := seedWatchMedia(t, svc, "a.jpg")

	if err := svc.SetMediaViewed(id); err != nil {
		t.Fatalf("记录最近查看失败: %v", err)
	}
	items, err := svc.RecentlyViewed(12)
	if err != nil {
		t.Fatalf("查询最近查看失败: %v", err)
	}
	if len(items) != 1 || items[0].ID != id {
		t.Fatalf("期望最近查看含 id=%d, 实际 %d 条", id, len(items))
	}
}

func TestRecentlyViewed_OrderAndExcludeUnviewed(t *testing.T) {
	svc := newWatchTestService(t)
	idA := seedWatchMedia(t, svc, "a.jpg") // 先看
	idB := seedWatchMedia(t, svc, "b.jpg") // 后看，应排在前
	seedWatchMedia(t, svc, "c.jpg")        // 从未查看，不应出现

	if err := svc.SetMediaViewed(idA); err != nil {
		t.Fatalf("记录 A 失败: %v", err)
	}
	// 拉开 last_viewed_at 时间差，确保 B 比 A 更晚
	time.Sleep(10 * time.Millisecond)
	if err := svc.SetMediaViewed(idB); err != nil {
		t.Fatalf("记录 B 失败: %v", err)
	}

	items, err := svc.RecentlyViewed(12)
	if err != nil {
		t.Fatalf("查询最近查看失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 条最近查看, 实际 %d", len(items))
	}
	// 按最近查看倒序：B 在前，A 在后
	if items[0].ID != idB || items[1].ID != idA {
		t.Fatalf("期望按最近查看倒序 [B, A], 实际 [%d, %d]", items[0].ID, items[1].ID)
	}
}

func TestRecentlyViewed_ExcludeSoftDeleted(t *testing.T) {
	svc := newWatchTestService(t)
	idKeep := seedWatchMedia(t, svc, "keep.jpg")
	idDel := seedWatchMedia(t, svc, "del.jpg")
	for _, id := range []int64{idKeep, idDel} {
		if err := svc.SetMediaViewed(id); err != nil {
			t.Fatalf("记录失败: %v", err)
		}
	}

	if err := svc.DeleteMediaFile(idDel); err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}

	items, err := svc.RecentlyViewed(12)
	if err != nil {
		t.Fatalf("查询最近查看失败: %v", err)
	}
	for _, it := range items {
		if it.ID == idDel {
			t.Fatalf("已软删媒体不应出现在最近查看列表")
		}
	}
	if len(items) != 1 || items[0].ID != idKeep {
		t.Fatalf("期望仅保留 id=%d, 实际 %d 条", idKeep, len(items))
	}
}

func TestRecentlyViewed_LimitClamp(t *testing.T) {
	svc := newWatchTestService(t)

	// limit 上限收敛：>50 收敛到 50（库内不足 50 条时返回全部，单独验证不越上限不便，这里只验证不报错并能返回）
	items, err := svc.RecentlyViewed(100)
	if err != nil {
		t.Fatalf("超上限 limit 查询失败: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("空库期望 0 条, 实际 %d", len(items))
	}

	// 准备 3 条已查看媒体，验证 limit 生效
	var ids []int64
	for _, name := range []string{"1.jpg", "2.jpg", "3.jpg"} {
		ids = append(ids, seedWatchMedia(t, svc, name))
	}
	for _, id := range ids {
		if err := svc.SetMediaViewed(id); err != nil {
			t.Fatalf("记录失败: %v", err)
		}
	}

	// limit=2 应只返回 2 条
	got, err := svc.RecentlyViewed(2)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("期望 limit=2 返回 2 条, 实际 %d", len(got))
	}

	// limit<1 回退默认 12：3 条全返回
	got, err = svc.RecentlyViewed(0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("期望 limit<1 回退默认返回全部 3 条, 实际 %d", len(got))
	}
}

func TestRecentlyViewed_EmptyLibrary(t *testing.T) {
	svc := newWatchTestService(t)
	items, err := svc.RecentlyViewed(12)
	if err != nil {
		t.Fatalf("空库查询失败: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("期望空库返回 0 条, 实际 %d", len(items))
	}
}
