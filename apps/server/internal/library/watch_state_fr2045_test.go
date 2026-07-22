package library

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestApplyWatchEvent当前会话重复倒序不落库(t *testing.T) {
	svc, db := newFR2045WatchService(t, false)
	media := seedFR2045Media(t, svc, "space-a", "repeat.mp4", 100)
	first := mustApplyWatchEvent(t, svc, "space-a", media.ID, WatchEventInput{
		PositionSeconds: 20, ExpectedRevision: 0, SessionID: "session-a", EventSeq: 2,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	})
	before := first.State.LastWatchedAt

	for _, seq := range []int64{2, 1} {
		result, err := svc.ApplyWatchEventInSpace("space-a", media.ID, WatchEventInput{
			PositionSeconds: 70, ExpectedRevision: first.State.Revision, SessionID: "session-a", EventSeq: seq,
			EventType: WatchEventProgress, Reason: WatchReasonUser,
		})
		if err != nil {
			t.Fatalf("重复或倒序事件不应报错: %v", err)
		}
		if result.Applied || result.State.Revision != 1 || result.State.PositionSeconds != 20 || !result.State.LastWatchedAt.Equal(before) {
			t.Fatalf("重复或倒序事件改变了状态: %+v", result)
		}
	}
	assertProjection(t, db, media.ID, 20, false, 0)
}

func TestApplyWatchEvent历史会话旧Revision冲突(t *testing.T) {
	svc, db := newFR2045WatchService(t, false)
	media := seedFR2045Media(t, svc, "space-a", "conflict.mp4", 100)
	mustApplyWatchEvent(t, svc, "space-a", media.ID, WatchEventInput{
		PositionSeconds: 10, ExpectedRevision: 0, SessionID: "session-old", EventSeq: 1,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	})
	current := mustApplyWatchEvent(t, svc, "space-a", media.ID, WatchEventInput{
		PositionSeconds: 55, ExpectedRevision: 1, SessionID: "session-new", EventSeq: 1,
		EventType: WatchEventSeek, Reason: WatchReasonUser,
	})

	_, err := svc.ApplyWatchEventInSpace("space-a", media.ID, WatchEventInput{
		PositionSeconds: 80, ExpectedRevision: 1, SessionID: "session-old", EventSeq: 2,
		EventType: WatchEventPause, Reason: WatchReasonSystem,
	})
	var conflict *WatchStateConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("历史会话迟到包应返回 revision 冲突，实际 %v", err)
	}
	if conflict.Current.Revision != current.State.Revision || conflict.Current.PositionSeconds != 55 {
		t.Fatalf("冲突未携带当前完整状态: %+v", conflict.Current)
	}
	assertProjection(t, db, media.ID, 55, false, 0)
}

func TestApplyWatchEvent完成ABLoop重播与未知时长(t *testing.T) {
	svc, db := newFR2045WatchService(t, false)
	known := seedFR2045Media(t, svc, "space-a", "known.mp4", 100)
	unknown := seedFR2045Media(t, svc, "space-a", "unknown.mp4", 0)

	abLoop := mustApplyWatchEvent(t, svc, "space-a", known.ID, WatchEventInput{
		PositionSeconds: 99, ExpectedRevision: 0, SessionID: "loop", EventSeq: 1,
		EventType: WatchEventSeek, Reason: WatchReasonABLoop,
	})
	if abLoop.State.Completed {
		t.Fatal("A-B 循环回跳不得完成")
	}
	assertProjection(t, db, known.ID, 99, false, 0)

	completed := mustApplyWatchEvent(t, svc, "space-a", known.ID, WatchEventInput{
		PositionSeconds: 95, ExpectedRevision: 1, SessionID: "watch", EventSeq: 1,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	})
	if !completed.State.Completed || completed.State.PositionSeconds != 0 || completed.State.CompletedAt == nil {
		t.Fatalf("95%% 完成规则未生效: %+v", completed.State)
	}
	assertProjection(t, db, known.ID, 0, true, 1)

	repeatedEnded := mustApplyWatchEvent(t, svc, "space-a", known.ID, WatchEventInput{
		PositionSeconds: 100, ExpectedRevision: 2, SessionID: "watch", EventSeq: 2,
		EventType: WatchEventEnded, Reason: WatchReasonSystem,
	})
	if !repeatedEnded.State.Completed {
		t.Fatal("重复 ended 后仍应保持完成")
	}
	assertProjection(t, db, known.ID, 0, true, 1)

	replay := mustApplyWatchEvent(t, svc, "space-a", known.ID, WatchEventInput{
		PositionSeconds: 8, ExpectedRevision: 3, SessionID: "replay", EventSeq: 1,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	})
	if replay.State.Completed || replay.State.PositionSeconds != 8 {
		t.Fatalf("从头重播应恢复未完成: %+v", replay.State)
	}
	mustApplyWatchEvent(t, svc, "space-a", known.ID, WatchEventInput{
		PositionSeconds: 90, ExpectedRevision: 4, SessionID: "replay", EventSeq: 2,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	})
	assertProjection(t, db, known.ID, 0, true, 2)

	ordinaryUnknown := mustApplyWatchEvent(t, svc, "space-a", unknown.ID, WatchEventInput{
		PositionSeconds: 999, ExpectedRevision: 0, SessionID: "unknown", EventSeq: 1,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	})
	if ordinaryUnknown.State.Completed {
		t.Fatal("未知时长普通进度不得推断完成")
	}
	endedUnknown := mustApplyWatchEvent(t, svc, "space-a", unknown.ID, WatchEventInput{
		PositionSeconds: 999, ExpectedRevision: 1, SessionID: "unknown", EventSeq: 2,
		EventType: WatchEventEnded, Reason: WatchReasonSystem,
	})
	if !endedUnknown.State.Completed {
		t.Fatal("未知时长 ended 应完成")
	}
}

func TestWatchHistory稳定游标继续观看与Space软删隔离(t *testing.T) {
	svc, db := newFR2045WatchService(t, false)
	first := seedFR2045Media(t, svc, "space-a", "first.mp4", 100)
	second := seedFR2045Media(t, svc, "space-a", "second.mp4", 100)
	completed := seedFR2045Media(t, svc, "space-a", "completed.mp4", 100)
	otherSpace := seedFR2045Media(t, svc, "space-b", "other.mp4", 100)

	fixed := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	for _, item := range []struct {
		space string
		id    int64
		pos   float64
	}{
		{"space-a", first.ID, 10}, {"space-a", second.ID, 20}, {"space-a", completed.ID, 100}, {"space-b", otherSpace.ID, 30},
	} {
		eventType := WatchEventProgress
		if item.id == completed.ID {
			eventType = WatchEventEnded
		}
		mustApplyWatchEvent(t, svc, item.space, item.id, WatchEventInput{
			PositionSeconds: item.pos, SessionID: "session-" + item.space, EventSeq: item.id,
			EventType: eventType, Reason: WatchReasonUser,
		})
	}

	page1, err := svc.ListWatchHistoryInSpace("space-a", "", 2)
	if err != nil {
		t.Fatalf("查询第一页历史失败: %v", err)
	}
	if len(page1.Items) != 2 || page1.Items[0].Media.ID != completed.ID || page1.Items[1].Media.ID != second.ID || page1.NextCursor == "" {
		t.Fatalf("历史第一页排序或游标错误: %+v", page1)
	}
	page2, err := svc.ListWatchHistoryInSpace("space-a", page1.NextCursor, 2)
	if err != nil {
		t.Fatalf("查询第二页历史失败: %v", err)
	}
	if len(page2.Items) != 1 || page2.Items[0].Media.ID != first.ID {
		t.Fatalf("历史第二页不稳定: %+v", page2)
	}

	continueItems, err := svc.ListContinueWatchingStatesInSpace("space-a", 10)
	if err != nil {
		t.Fatalf("查询继续观看失败: %v", err)
	}
	if len(continueItems) != 2 || continueItems[0].Media.ID != second.ID || continueItems[1].Media.ID != first.ID {
		t.Fatalf("继续观看未读取真源或排序错误: %+v", continueItems)
	}

	if err := db.Model(&models.MediaFile{}).Where("id = ?", second.ID).Update("deleted_at", fixed).Error; err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}
	continueItems, err = svc.ListContinueWatchingStatesInSpace("space-a", 10)
	if err != nil || len(continueItems) != 1 || continueItems[0].Media.ID != first.ID {
		t.Fatalf("继续观看未隔离软删媒体: items=%+v err=%v", continueItems, err)
	}
}

func TestWatchState读写隔离Space并拒绝软删媒体(t *testing.T) {
	svc, db := newFR2045WatchService(t, false)
	media := seedFR2045Media(t, svc, "space-a", "scoped.mp4", 100)
	applied := mustApplyWatchEvent(t, svc, "space-a", media.ID, WatchEventInput{
		PositionSeconds: 30, SessionID: "space-a-session", EventSeq: 1,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	})

	if _, err := svc.GetWatchStateInSpace("space-b", media.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("其他 Space 不应读取观看状态，实际 %v", err)
	}
	if _, err := svc.ApplyWatchEventInSpace("space-b", media.ID, WatchEventInput{
		PositionSeconds: 60, SessionID: "space-b-session", EventSeq: 1,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("其他 Space 不应更新观看状态，实际 %v", err)
	}

	deletedAt := time.Now().UTC()
	if err := db.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", "space-a", media.ID).Update("deleted_at", deletedAt).Error; err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}
	if _, err := svc.ApplyWatchEventInSpace("space-a", media.ID, WatchEventInput{
		PositionSeconds: 70, ExpectedRevision: applied.State.Revision,
		SessionID: "space-a-session", EventSeq: 2,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("软删媒体不应再接受观看事件，实际 %v", err)
	}

	var state models.WatchState
	if err := db.Where("space_id = ? AND media_id = ?", "space-a", media.ID).First(&state).Error; err != nil {
		t.Fatalf("读取软删后的真源失败: %v", err)
	}
	if state.Revision != applied.State.Revision || state.PositionSeconds != 30 {
		t.Fatalf("拒绝写入后真源不应改变: %+v", state)
	}
}

func TestApplyWatchEvent投影失败整体回滚(t *testing.T) {
	svc, db := newFR2045WatchService(t, false)
	media := seedFR2045Media(t, svc, "space-a", "rollback.mp4", 100)
	if err := db.Exec(`CREATE TRIGGER fail_watch_projection BEFORE UPDATE OF last_position ON media_files
		WHEN NEW.last_position = 77 BEGIN SELECT RAISE(ABORT, '投影失败'); END;`).Error; err != nil {
		t.Fatalf("创建投影失败触发器失败: %v", err)
	}

	_, err := svc.ApplyWatchEventInSpace("space-a", media.ID, WatchEventInput{
		PositionSeconds: 77, SessionID: "rollback", EventSeq: 1,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	})
	if err == nil {
		t.Fatal("投影失败时统一事务应失败")
	}
	var count int64
	if err := db.Model(&models.WatchState{}).Count(&count).Error; err != nil {
		t.Fatalf("统计真源失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("投影失败后真源不应提交，实际 %d 行", count)
	}
	assertProjection(t, db, media.ID, 0, false, 0)
}

func TestApplyWatchEventSQLite两事务争用同一Revision(t *testing.T) {
	svcA, db := newFR2045WatchService(t, true)
	media := seedFR2045Media(t, svcA, "space-a", "race.mp4", 100)
	mustApplyWatchEvent(t, svcA, "space-a", media.ID, WatchEventInput{
		PositionSeconds: 5, SessionID: "seed", EventSeq: 1,
		EventType: WatchEventProgress, Reason: WatchReasonUser,
	})
	svcB := NewService(db)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for index, svc := range []*Service{svcA, svcB} {
		wg.Add(1)
		go func(index int, svc *Service) {
			defer wg.Done()
			<-start
			_, err := svc.ApplyWatchEventInSpace("space-a", media.ID, WatchEventInput{
				PositionSeconds: float64(20 + index), ExpectedRevision: 1,
				SessionID: "race-session-" + string(rune('a'+index)), EventSeq: 1,
				EventType: WatchEventSeek, Reason: WatchReasonUser,
			})
			results <- err
		}(index, svc)
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var conflict *WatchStateConflictError
		if errors.As(err, &conflict) {
			conflicts++
			continue
		}
		t.Fatalf("争用应只产生成功或 revision 冲突，实际 %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("同一 revision 争用应一成一败，success=%d conflict=%d", successes, conflicts)
	}
	state, err := svcA.GetWatchStateInSpace("space-a", media.ID)
	if err != nil {
		t.Fatalf("读取争用后状态失败: %v", err)
	}
	if state.Revision != 2 || (state.PositionSeconds != 20 && state.PositionSeconds != 21) {
		t.Fatalf("争用后真源异常: %+v", state)
	}
	assertProjection(t, db, media.ID, state.PositionSeconds, state.Completed, 0)
}

func newFR2045WatchService(t *testing.T, fileDB bool) (*Service, *gorm.DB) {
	t.Helper()
	dsn := ":memory:"
	if fileDB {
		dsn = filepath.Join(t.TempDir(), "watch-state.db") + "?_busy_timeout=5000&_journal_mode=WAL"
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开观看状态测试库失败: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		if !fileDB {
			sqlDB.SetMaxOpenConns(1)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.WatchState{}); err != nil {
		t.Fatalf("迁移观看状态测试表失败: %v", err)
	}
	return NewService(db), db
}

func seedFR2045Media(t *testing.T, svc *Service, spaceID, name string, duration float64) *models.MediaFile {
	t.Helper()
	lp, err := svc.CreateLibraryPathInSpace(spaceID, t.TempDir(), "local", spaceID)
	if err != nil {
		t.Fatalf("创建测试媒体库失败: %v", err)
	}
	media, err := svc.CreateMediaFileInSpace(spaceID, lp.ID, filepath.ToSlash(filepath.Join("D:/Videos", spaceID, name)), 100)
	if err != nil {
		t.Fatalf("创建测试媒体失败: %v", err)
	}
	if err := svc.db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Update("duration", duration).Error; err != nil {
		t.Fatalf("写入测试媒体时长失败: %v", err)
	}
	media.Duration = duration
	return media
}

func mustApplyWatchEvent(t *testing.T, svc *Service, spaceID string, mediaID int64, input WatchEventInput) WatchEventResult {
	t.Helper()
	result, err := svc.ApplyWatchEventInSpace(spaceID, mediaID, input)
	if err != nil {
		t.Fatalf("应用观看事件失败: %v", err)
	}
	if !result.Applied {
		t.Fatalf("新观看事件应被应用: %+v", result)
	}
	return result
}

func assertProjection(t *testing.T, db *gorm.DB, mediaID int64, position float64, watched bool, viewCount int) {
	t.Helper()
	var media models.MediaFile
	if err := db.First(&media, mediaID).Error; err != nil {
		t.Fatalf("读取媒体投影失败: %v", err)
	}
	if media.LastPosition != position || media.Watched != watched || media.ViewCount != viewCount {
		t.Fatalf("媒体投影不一致: %+v", media)
	}
}
