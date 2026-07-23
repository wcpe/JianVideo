package library

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestBookmarkCRUDRevisionCAS排序与文本规范化(t *testing.T) {
	svc, _, media := newBookmarkTestService(t)
	note := "  稍后复看  "
	later, err := svc.CreateMediaBookmark(context.Background(), media.SpaceID, media.ID, BookmarkInput{PositionMS: 5000, Title: "  关键论点  ", Note: &note})
	if err != nil {
		t.Fatalf("创建书签失败: %v", err)
	}
	if later.Revision != 1 || later.Title != "关键论点" || later.Note == nil || *later.Note != "稍后复看" {
		t.Fatalf("创建结果未规范化: %+v", later)
	}
	emptyNote := "   "
	earlier, err := svc.CreateMediaBookmark(context.Background(), media.SpaceID, media.ID, BookmarkInput{PositionMS: 1000, Title: "开场", Note: &emptyNote})
	if err != nil {
		t.Fatalf("创建第二个书签失败: %v", err)
	}
	if earlier.Note != nil {
		t.Fatalf("空备注应规范化为 null: %+v", earlier)
	}
	items, err := svc.ListMediaBookmarks(context.Background(), media.SpaceID, media.ID)
	if err != nil || len(items) != 2 || items[0].ID != earlier.ID || items[1].ID != later.ID {
		t.Fatalf("书签排序错误: items=%+v err=%v", items, err)
	}

	updated, err := svc.UpdateMediaBookmark(context.Background(), media.SpaceID, media.ID, later.ID, BookmarkUpdate{PositionMS: 6000, Title: "修正", Revision: 1})
	if err != nil {
		t.Fatalf("更新书签失败: %v", err)
	}
	if updated.Revision != 2 || updated.PositionMS != 6000 || updated.Title != "修正" {
		t.Fatalf("更新结果错误: %+v", updated)
	}
	_, err = svc.UpdateMediaBookmark(context.Background(), media.SpaceID, media.ID, later.ID, BookmarkUpdate{PositionMS: 7000, Title: "旧端覆盖", Revision: 1})
	assertBookmarkConflict(t, err, 2, false)
	if err := svc.DeleteMediaBookmark(context.Background(), media.SpaceID, media.ID, later.ID, 1); err == nil {
		t.Fatal("旧 revision 删除必须冲突")
	} else {
		assertBookmarkConflict(t, err, 2, false)
	}
	if err := svc.DeleteMediaBookmark(context.Background(), media.SpaceID, media.ID, later.ID, 2); err != nil {
		t.Fatalf("删除书签失败: %v", err)
	}
	if err := svc.DeleteMediaBookmark(context.Background(), media.SpaceID, media.ID, later.ID, 2); err == nil {
		t.Fatal("重复删除必须返回删除状态冲突")
	} else {
		assertBookmarkConflict(t, err, 0, true)
	}
}

func TestBookmark校验Space与软删隔离(t *testing.T) {
	svc, db, media := newBookmarkTestService(t)
	cases := []struct {
		name string
		in   BookmarkInput
		err  error
	}{
		{name: "标题为空", in: BookmarkInput{Title: "   ", PositionMS: 1}, err: ErrBookmarkTitleRequired},
		{name: "标题过长", in: BookmarkInput{Title: strings.Repeat("界", 121), PositionMS: 1}, err: ErrBookmarkTitleTooLong},
		{name: "备注过长", in: BookmarkInput{Title: "标题", Note: stringPointer(strings.Repeat("注", 2001)), PositionMS: 1}, err: ErrBookmarkNoteTooLong},
		{name: "负时间", in: BookmarkInput{Title: "标题", PositionMS: -1}, err: ErrBookmarkInvalidPosition},
		{name: "超过时长", in: BookmarkInput{Title: "标题", PositionMS: 10001}, err: ErrBookmarkInvalidPosition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.CreateMediaBookmark(context.Background(), media.SpaceID, media.ID, tc.in); !errors.Is(err, tc.err) {
				t.Fatalf("错误不匹配: got=%v want=%v", err, tc.err)
			}
		})
	}
	bookmark, err := svc.CreateMediaBookmark(context.Background(), media.SpaceID, media.ID, BookmarkInput{Title: "保留", PositionMS: 1000})
	if err != nil {
		t.Fatalf("创建有效书签失败: %v", err)
	}
	if _, err := svc.ListMediaBookmarks(context.Background(), "space-b", media.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨 Space 读取应不可见: %v", err)
	}
	now := time.Now()
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Update("deleted_at", &now).Error; err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}
	if _, err := svc.ListMediaBookmarks(context.Background(), media.SpaceID, media.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("软删媒体的书签应不可见: %v", err)
	}
	var count int64
	if err := db.Model(&models.MediaBookmark{}).Where("id = ?", bookmark.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("软删期间书签业务行必须保留: count=%d err=%v", count, err)
	}
}

func TestBookmark审计与业务写入同事务且不复制完整备注(t *testing.T) {
	svc, db, media := newBookmarkTestService(t)
	note := "完整私密备注不可进入审计"
	created, err := svc.CreateMediaBookmark(context.Background(), media.SpaceID, media.ID, BookmarkInput{Title: "审计标题", PositionMS: 2000, Note: &note})
	if err != nil {
		t.Fatalf("创建书签失败: %v", err)
	}
	if _, err := svc.UpdateMediaBookmark(context.Background(), media.SpaceID, media.ID, created.ID, BookmarkUpdate{Title: "审计标题二", PositionMS: 2500, Note: &note, Revision: 1}); err != nil {
		t.Fatalf("更新书签失败: %v", err)
	}
	if err := svc.DeleteMediaBookmark(context.Background(), media.SpaceID, media.ID, created.ID, 2); err != nil {
		t.Fatalf("删除书签失败: %v", err)
	}
	var events []models.AuditEvent
	if err := db.Where("resource_id = ?", created.ID).Order("id").Find(&events).Error; err != nil {
		t.Fatalf("读取审计失败: %v", err)
	}
	if len(events) != 3 || events[0].Action != "bookmark.created" || events[1].Action != "bookmark.updated" || events[2].Action != "bookmark.deleted" {
		t.Fatalf("审计事件不完整: %+v", events)
	}
	for _, event := range events {
		if strings.Contains(event.BeforeJSON+event.AfterJSON+event.MetadataJSON, note) {
			t.Fatalf("审计不得包含完整备注: %+v", event)
		}
	}

	failing := NewService(db).WithAudit(failingBookmarkAudit{})
	if _, err := failing.CreateMediaBookmark(context.Background(), media.SpaceID, media.ID, BookmarkInput{Title: "应回滚", PositionMS: 3000}); err == nil {
		t.Fatal("审计失败时业务写入必须失败")
	}
	var count int64
	if err := db.Model(&models.MediaBookmark{}).Where("title = ?", "应回滚").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("审计失败时业务写入未回滚: count=%d err=%v", count, err)
	}
}

func newBookmarkTestService(t *testing.T) (*Service, *gorm.DB, models.MediaFile) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaBookmark{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}
	media := models.MediaFile{SpaceID: "space-a", LibraryID: 1, FilePath: "bookmark.mp4", FileName: "bookmark.mp4", Format: "mp4", Duration: 10, AddedAt: time.Now(), ModifiedAt: time.Now()}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	return NewService(db).WithAudit(audit.NewRecorder(db)), db, media
}

func assertBookmarkConflict(t *testing.T, err error, revision int64, deleted bool) {
	t.Helper()
	var conflict *BookmarkConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("应返回书签冲突: %v", err)
	}
	if conflict.Deleted != deleted {
		t.Fatalf("删除状态错误: %+v", conflict)
	}
	if revision > 0 && (conflict.Current == nil || conflict.Current.Revision != revision) {
		t.Fatalf("当前服务端记录错误: %+v", conflict)
	}
}

func stringPointer(value string) *string { return &value }

type failingBookmarkAudit struct{}

func (failingBookmarkAudit) Record(context.Context, audit.EventInput) error {
	return errors.New("审计失败")
}
func (failingBookmarkAudit) RecordTx(context.Context, *gorm.DB, audit.EventInput) error {
	return errors.New("审计失败")
}
func (failingBookmarkAudit) List(context.Context, audit.Query) (audit.Page, error) {
	return audit.Page{}, nil
}
func (failingBookmarkAudit) GetByID(context.Context, int64) (*models.AuditEvent, error) {
	return nil, errors.New("审计失败")
}
