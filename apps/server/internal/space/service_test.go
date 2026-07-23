package space

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func setupSpaceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:space_svc?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Space{}, &models.SpaceMember{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	now := time.Now()
	if err := db.Create(&models.Space{
		ID: models.DefaultSpaceID, Name: "默认", OwnerUserID: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("插入 Space 失败: %v", err)
	}
	if err := db.Create(&models.SpaceMember{
		SpaceID: models.DefaultSpaceID, UserID: 1, Role: models.SpaceRoleOwner, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("插入 owner 失败: %v", err)
	}
	return db
}

func TestMemberRoleAndRequireRole(t *testing.T) {
	svc := NewService(setupSpaceDB(t))
	now := time.Now()
	if err := svc.db.Create(&models.SpaceMember{
		SpaceID: models.DefaultSpaceID, UserID: 2, Role: models.SpaceRoleViewer, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	role, err := svc.MemberRole(models.DefaultSpaceID, 1)
	if err != nil || role != models.SpaceRoleOwner {
		t.Fatalf("owner role = %q err=%v", role, err)
	}
	role, err = svc.MemberRole(models.DefaultSpaceID, 2)
	if err != nil || role != models.SpaceRoleViewer {
		t.Fatalf("viewer role = %q err=%v", role, err)
	}
	if err := svc.RequireRole(models.DefaultSpaceID, 2, models.SpaceRoleViewer); err != nil {
		t.Fatalf("viewer 应满足 viewer: %v", err)
	}
	if err := svc.RequireRole(models.DefaultSpaceID, 2, models.SpaceRoleEditor); err != ErrForbidden {
		t.Fatalf("viewer 不应满足 editor: %v", err)
	}
	if _, err := svc.MemberRole(models.DefaultSpaceID, 99); err != ErrNotMember {
		t.Fatalf("非成员: %v", err)
	}
}

func TestCreateSpaceAndListAccessible(t *testing.T) {
	svc := NewService(setupSpaceDB(t))
	sp, err := svc.CreateSpace("space-work", "工作", 1)
	if err != nil || sp.ID != "space-work" {
		t.Fatalf("CreateSpace: %+v err=%v", sp, err)
	}
	list, err := svc.ListAccessible(1)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListAccessible owner: n=%d err=%v", len(list), err)
	}
	list2, err := svc.ListAccessible(2)
	if err != nil || len(list2) != 0 {
		t.Fatalf("ListAccessible 非成员: n=%d err=%v", len(list2), err)
	}
}

func TestAddRemoveMember(t *testing.T) {
	svc := NewService(setupSpaceDB(t))
	created, err := svc.AddMember(models.DefaultSpaceID, 2, models.SpaceRoleEditor)
	if err != nil || !created {
		t.Fatalf("首次添加: created=%v err=%v", created, err)
	}
	role, err := svc.MemberRole(models.DefaultSpaceID, 2)
	if err != nil || role != models.SpaceRoleEditor {
		t.Fatalf("role=%q err=%v", role, err)
	}
	created, err = svc.AddMember(models.DefaultSpaceID, 2, models.SpaceRoleViewer)
	if err != nil || created {
		t.Fatalf("更新角色: created=%v err=%v", created, err)
	}
	role, _ = svc.MemberRole(models.DefaultSpaceID, 2)
	if role != models.SpaceRoleViewer {
		t.Fatalf("更新角色失败: %q", role)
	}
	if err := svc.RemoveMember(models.DefaultSpaceID, 1); err != ErrCannotRemoveOwner {
		t.Fatalf("不应移除 owner: %v", err)
	}
	if err := svc.RemoveMember(models.DefaultSpaceID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MemberRole(models.DefaultSpaceID, 2); err != ErrNotMember {
		t.Fatalf("移除后: %v", err)
	}
}

func TestRoleAtLeast(t *testing.T) {
	if !models.RoleAtLeast(models.SpaceRoleOwner, models.SpaceRoleEditor) {
		t.Fatal("owner >= editor")
	}
	if models.RoleAtLeast(models.SpaceRoleViewer, models.SpaceRoleEditor) {
		t.Fatal("viewer < editor")
	}
}

func TestEffectiveMaxRating_MemberOverrideAndSpaceDefault(t *testing.T) {
	svc := NewService(setupSpaceDB(t))
	now := time.Now()
	if err := svc.db.Create(&models.SpaceMember{
		SpaceID: models.DefaultSpaceID, UserID: 2, Role: models.SpaceRoleViewer, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 无限制
	max, err := svc.EffectiveMaxRating(models.DefaultSpaceID, 2)
	if err != nil || max != "" {
		t.Fatalf("默认无限制: max=%q err=%v", max, err)
	}

	if err := svc.SetSpaceDefaultMaxRating(models.DefaultSpaceID, "PG-13"); err != nil {
		t.Fatal(err)
	}
	max, err = svc.EffectiveMaxRating(models.DefaultSpaceID, 2)
	if err != nil || max != models.ContentRatingPG13 {
		t.Fatalf("继承 Space 默认: max=%q err=%v", max, err)
	}

	pg := "PG"
	if err := svc.SetMemberMaxRating(models.DefaultSpaceID, 2, &pg); err != nil {
		t.Fatal(err)
	}
	max, err = svc.EffectiveMaxRating(models.DefaultSpaceID, 2)
	if err != nil || max != models.ContentRatingPG {
		t.Fatalf("成员覆盖: max=%q err=%v", max, err)
	}

	empty := ""
	if err := svc.SetMemberMaxRating(models.DefaultSpaceID, 2, &empty); err != nil {
		t.Fatal(err)
	}
	max, err = svc.EffectiveMaxRating(models.DefaultSpaceID, 2)
	if err != nil || max != models.ContentRatingPG13 {
		t.Fatalf("清除覆盖后继承: max=%q err=%v", max, err)
	}
}
