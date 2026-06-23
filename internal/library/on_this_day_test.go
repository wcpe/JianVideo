package library

import (
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// setMediaTime 直接给指定媒体写入 media_time（CreateMediaFile 不接受该字段，测试里旁路设置）。
func setMediaTime(t *testing.T, svc *Service, id int64, mt time.Time) {
	t.Helper()
	if err := svc.db.Model(&models.MediaFile{}).Where("id = ?", id).
		Update("media_time", mt).Error; err != nil {
		t.Fatalf("设置 media_time 失败: %v", err)
	}
}

func TestListOnThisDay(t *testing.T) {
	svc := newWatchTestService(t)
	now := time.Now()

	// 命中：往年的同一月日（去年、前年），按 media_time 倒序应是「去年」在前
	idLastYear := seedWatchMedia(t, svc, "last_year.jpg")
	setMediaTime(t, svc, idLastYear, now.AddDate(-1, 0, 0))
	idTwoYears := seedWatchMedia(t, svc, "two_years.jpg")
	setMediaTime(t, svc, idTwoYears, now.AddDate(-2, 0, 0))

	// 不命中：今年的同一天（不算「那年今日」）
	idThisYear := seedWatchMedia(t, svc, "this_year.jpg")
	setMediaTime(t, svc, idThisYear, now)

	// 不命中：往年的不同月日
	idOtherDay := seedWatchMedia(t, svc, "other_day.jpg")
	setMediaTime(t, svc, idOtherDay, now.AddDate(-1, 0, 1))

	// 不命中：media_time 为空（不回退入库时间）
	seedWatchMedia(t, svc, "no_time.jpg")

	items, err := svc.ListOnThisDay(12)
	if err != nil {
		t.Fatalf("查询那年今日失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 条往年同日记录, 实际 %d: %+v", len(items), idsOf(items))
	}
	// 按 media_time 倒序：去年在前，前年在后
	if items[0].ID != idLastYear || items[1].ID != idTwoYears {
		t.Fatalf("期望按 media_time 倒序 [去年, 前年], 实际 %v", idsOf(items))
	}
}

func TestListOnThisDay_ExcludesDeleted(t *testing.T) {
	svc := newWatchTestService(t)
	now := time.Now()

	idKeep := seedWatchMedia(t, svc, "keep.jpg")
	setMediaTime(t, svc, idKeep, now.AddDate(-1, 0, 0))
	idDel := seedWatchMedia(t, svc, "del.jpg")
	setMediaTime(t, svc, idDel, now.AddDate(-3, 0, 0))

	if err := svc.DeleteMediaFile(idDel); err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}

	items, err := svc.ListOnThisDay(12)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(items) != 1 || items[0].ID != idKeep {
		t.Fatalf("期望仅保留未删除的 1 条, 实际 %v", idsOf(items))
	}
}

func TestListOnThisDay_Limit(t *testing.T) {
	svc := newWatchTestService(t)
	now := time.Now()
	for i := 1; i <= 4; i++ {
		id := seedWatchMedia(t, svc, time.Now().Format("150405")+string(rune('a'+i))+".jpg")
		setMediaTime(t, svc, id, now.AddDate(-i, 0, 0))
	}

	items, err := svc.ListOnThisDay(2)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 limit=2 返回 2 条, 实际 %d", len(items))
	}
}

// idsOf 提取媒体 ID 列表，便于断言失败时打印。
func idsOf(items []models.MediaFile) []int64 {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}
