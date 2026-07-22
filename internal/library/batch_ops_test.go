package library

import (
	"errors"
	"testing"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// TestBatchReassignLibraryInSpace 索引层改库：移动、跳过已在目标库与缺失 id（FR2-053）。
func TestBatchReassignLibraryInSpace(t *testing.T) {
	svc, gdb := newTagTestService(t)
	src := models.LibraryPath{
		SpaceID: models.DefaultSpaceID, Path: "D:/src", Type: "local",
		LibraryKind: models.LibraryKindMixed, LibraryProfileJSON: "{}", Label: "源", Enabled: 1,
	}
	dst := models.LibraryPath{
		SpaceID: models.DefaultSpaceID, Path: "D:/dst", Type: "local",
		LibraryKind: models.LibraryKindMixed, LibraryProfileJSON: "{}", Label: "目标", Enabled: 1,
	}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&dst).Error; err != nil {
		t.Fatal(err)
	}

	a, err := svc.CreateMediaFile(src.ID, "D:/src/a.mp4", 1024)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateMediaFile(src.ID, "D:/src/b.mp4", 1024)
	if err != nil {
		t.Fatal(err)
	}
	// 已在目标库
	c, err := svc.CreateMediaFile(dst.ID, "D:/dst/c.mp4", 1024)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.BatchReassignLibraryInSpace(models.DefaultSpaceID, []int64{a.ID, b.ID, c.ID, 99999}, dst.ID)
	if err != nil {
		t.Fatalf("批量改库失败: %v", err)
	}
	if result.Moved != 2 {
		t.Fatalf("期望 moved=2, 实际 %d", result.Moved)
	}
	// 已在目标库 1 + 不存在 1
	if result.Skipped != 2 {
		t.Fatalf("期望 skipped=2, 实际 %d", result.Skipped)
	}

	for _, id := range []int64{a.ID, b.ID} {
		mf, err := svc.GetMediaFileByIDInSpace(models.DefaultSpaceID, id)
		if err != nil {
			t.Fatal(err)
		}
		if mf.LibraryID != dst.ID {
			t.Fatalf("媒体 %d 期望 library_id=%d, 实际 %d", id, dst.ID, mf.LibraryID)
		}
	}
}

// TestBatchReassignLibraryInSpace_TargetMissing 目标库不存在时返回明确错误。
func TestBatchReassignLibraryInSpace_TargetMissing(t *testing.T) {
	svc, _ := newTagTestService(t)
	_, err := svc.BatchReassignLibraryInSpace(models.DefaultSpaceID, []int64{1}, 999)
	if !errors.Is(err, ErrBatchTargetLibraryNotFound) {
		t.Fatalf("期望 ErrBatchTargetLibraryNotFound, 实际 %v", err)
	}
}

// TestBatchReassignLibraryInSpace_Empty 空列表 no-op。
func TestBatchReassignLibraryInSpace_Empty(t *testing.T) {
	svc, gdb := newTagTestService(t)
	lp := models.LibraryPath{
		SpaceID: models.DefaultSpaceID, Path: "D:/only", Type: "local",
		LibraryKind: models.LibraryKindMixed, LibraryProfileJSON: "{}", Enabled: 1,
	}
	if err := gdb.Create(&lp).Error; err != nil {
		t.Fatal(err)
	}
	result, err := svc.BatchReassignLibraryInSpace(models.DefaultSpaceID, nil, lp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Moved != 0 || result.Skipped != 0 {
		t.Fatalf("空列表应全零, 实际 %+v", result)
	}
}
