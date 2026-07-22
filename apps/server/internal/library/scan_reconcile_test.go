package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// countDeleted 统计指定库内已软删（deleted_at 非空）记录数，供对账断言。
func countDeleted(t *testing.T, svc *Service, libraryID int64) int64 {
	t.Helper()
	var n int64
	if err := svc.db.Model(&models.MediaFile{}).
		Where("library_id = ? AND deleted_at IS NOT NULL", libraryID).
		Count(&n).Error; err != nil {
		t.Fatalf("统计软删记录失败: %v", err)
	}
	return n
}

// fileStateOf 返回指定库内某路径记录的文件状态与 deleted_at 状态（不存在则 t.Fatal）。
func fileStateOf(t *testing.T, svc *Service, libraryID int64, filePath string) (string, bool) {
	t.Helper()
	var mf models.MediaFile
	if err := svc.db.Where("library_id = ? AND file_path = ?", libraryID, filepath.ToSlash(filePath)).
		First(&mf).Error; err != nil {
		t.Fatalf("查询记录失败: %s, err=%v", filePath, err)
	}
	return mf.FileState, mf.DeletedAt != nil
}

// TestFullScan_ReconcilesDeletedFiles 全量扫描对账：删除磁盘源文件后全量扫，
// 该记录应被标记 missing，仍存在的文件不动。
func TestFullScan_ReconcilesDeletedFiles(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()

	keep := filepath.Join(dir, "keep.mp4")
	gone := filepath.Join(dir, "gone.mp4")
	if err := os.WriteFile(keep, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if err := os.WriteFile(gone, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}

	// 首次全量扫描入库两条
	count, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeFull)
	if err != nil {
		t.Fatalf("首次全量扫描失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("首次全量扫描应入库 2 条, 实际 %d", count)
	}
	if n := countDeleted(t, svc, 1); n != 0 {
		t.Fatalf("首次扫描后不应有软删记录, 实际 %d", n)
	}

	// 删除其中一个源文件，再全量扫描触发对账
	if err := os.Remove(gone); err != nil {
		t.Fatalf("删除源文件失败: %v", err)
	}
	if _, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeFull); err != nil {
		t.Fatalf("二次全量扫描失败: %v", err)
	}

	goneState, goneDeleted := fileStateOf(t, svc, 1, gone)
	if goneDeleted {
		t.Fatal("源文件已删除，全量扫描后该记录不应进入回收站")
	}
	if goneState != models.MediaFileStateMissing {
		t.Fatalf("源文件已删除，全量扫描后应标记 missing，实际 %q", goneState)
	}
	keepState, keepDeleted := fileStateOf(t, svc, 1, keep)
	if keepDeleted {
		t.Fatal("仍存在的文件不应被软删")
	}
	if keepState != models.MediaFileStateAvailable {
		t.Fatalf("仍存在的文件应保持 available，实际 %q", keepState)
	}

	// missing 项常规列表不可见，且不进入回收站
	recycled, err := svc.ListDeletedMediaFiles()
	if err != nil {
		t.Fatalf("查询回收站失败: %v", err)
	}
	if len(recycled) != 0 {
		t.Fatalf("回收站不应包含 missing 记录, 实际 %d", len(recycled))
	}
	_, total, err := svc.ListMediaFiles(1, "", "", 1, 20)
	if err != nil {
		t.Fatalf("查询常规列表失败: %v", err)
	}
	if total != 1 {
		t.Fatalf("常规列表应只剩 1 条, 实际 %d", total)
	}
}

// TestFullScan_MarksMissingWithoutSoftDelete FR2-027：全量对账只标记 missing，
// 不把缺失文件放入回收站；常规列表仍需隐藏 missing 项。
func TestFullScan_MarksMissingWithoutSoftDelete(t *testing.T) {
	svc, gdb := newTestService(t)
	dir := t.TempDir()

	keep := filepath.Join(dir, "keep.mp4")
	gone := filepath.Join(dir, "gone.mp4")
	if err := os.WriteFile(keep, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if err := os.WriteFile(gone, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if _, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeFull); err != nil {
		t.Fatalf("首次全量扫描失败: %v", err)
	}

	if err := os.Remove(gone); err != nil {
		t.Fatalf("删除源文件失败: %v", err)
	}
	if _, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeFull); err != nil {
		t.Fatalf("二次全量扫描失败: %v", err)
	}

	var missing models.MediaFile
	if err := gdb.Where("library_id = ? AND file_path = ?", int64(1), filepath.ToSlash(gone)).First(&missing).Error; err != nil {
		t.Fatalf("缺失文件记录应仍存在: %v", err)
	}
	if missing.DeletedAt != nil {
		t.Fatal("缺失文件不应进入回收站")
	}
	if missing.FileState != models.MediaFileStateMissing {
		t.Fatalf("缺失文件应标记 missing，实际 %q", missing.FileState)
	}

	items, total, err := svc.ListMediaFilesFiltered(MediaFilter{LibraryID: 1}, 1, 20)
	if err != nil {
		t.Fatalf("查询常规列表失败: %v", err)
	}
	if total != 1 || len(items) != 1 || filepath.ToSlash(items[0].FilePath) != filepath.ToSlash(keep) {
		t.Fatalf("常规列表应只包含存在文件，total=%d items=%+v", total, items)
	}
	deleted, err := svc.ListDeletedMediaFiles()
	if err != nil {
		t.Fatalf("查询回收站失败: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("missing 不应出现在回收站，实际 %d 条", len(deleted))
	}
}

// TestIncrementalScan_DoesNotReconcile 增量扫描不对账：删除源文件后增量扫，
// 旧记录不应被软删，只索引新增文件。
func TestIncrementalScan_DoesNotReconcile(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()

	a := filepath.Join(dir, "a.mp4")
	b := filepath.Join(dir, "b.mp4")
	if err := os.WriteFile(a, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if err := os.WriteFile(b, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}

	if _, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeIncremental); err != nil {
		t.Fatalf("首次增量扫描失败: %v", err)
	}

	// 删除一个源文件，增量扫描不应对账软删
	if err := os.Remove(b); err != nil {
		t.Fatalf("删除源文件失败: %v", err)
	}
	if _, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeIncremental); err != nil {
		t.Fatalf("二次增量扫描失败: %v", err)
	}
	if n := countDeleted(t, svc, 1); n != 0 {
		t.Fatalf("增量扫描不应软删任何记录, 实际软删 %d", n)
	}

	// 增量扫描只索引新增文件
	c := filepath.Join(dir, "c.mp4")
	if err := os.WriteFile(c, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	added, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeIncremental)
	if err != nil {
		t.Fatalf("三次增量扫描失败: %v", err)
	}
	if added != 1 {
		t.Fatalf("增量扫描应只索引 1 个新增文件, 实际 %d", added)
	}
}

// TestFullScan_OtherLibraryUntouched 全量扫描对账只影响当前库，不误标其他库同路径记录。
func TestFullScan_OtherLibraryUntouched(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared.mp4")
	if err := os.WriteFile(shared, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}

	// 库 1 全量扫入库，库 2 手动建一条同路径记录
	if _, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeFull); err != nil {
		t.Fatalf("库 1 全量扫描失败: %v", err)
	}
	if _, err := svc.CreateMediaFile(2, shared, 1024); err != nil {
		t.Fatalf("库 2 建记录失败: %v", err)
	}

	// 删源文件后只全量扫库 1，库 2 记录不应被误标
	if err := os.Remove(shared); err != nil {
		t.Fatalf("删除源文件失败: %v", err)
	}
	if _, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeFull); err != nil {
		t.Fatalf("库 1 二次全量扫描失败: %v", err)
	}
	state1, deleted1 := fileStateOf(t, svc, 1, shared)
	if deleted1 {
		t.Fatal("库 1 缺失文件不应被软删")
	}
	if state1 != models.MediaFileStateMissing {
		t.Fatalf("库 1 缺失文件应标记 missing，实际 %q", state1)
	}
	state2, deleted2 := fileStateOf(t, svc, 2, shared)
	if deleted2 {
		t.Fatal("库 2 同路径记录不应被对账误标")
	}
	if state2 != models.MediaFileStateAvailable {
		t.Fatalf("库 2 同路径记录应保持 available，实际 %q", state2)
	}
}

// TestFullScan_AlreadyDeletedNotReprocessed 已软删记录在全量扫描时不被重复处理（deleted_at 不变）。
func TestFullScan_AlreadyDeletedNotReprocessed(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "x.mp4")
	if err := os.WriteFile(f, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if _, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeFull); err != nil {
		t.Fatalf("全量扫描失败: %v", err)
	}

	mf, err := svc.GetMediaFileByLibraryAndPath(1, f)
	if err != nil {
		t.Fatalf("查询记录失败: %v", err)
	}
	// 手动软删，记录首次 deleted_at
	if err := svc.DeleteMediaFile(mf.ID); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	var before models.MediaFile
	if err := svc.db.First(&before, mf.ID).Error; err != nil {
		t.Fatalf("查询软删后记录失败: %v", err)
	}

	// 删源文件后全量扫：对账应跳过已软删记录，不改写其 deleted_at
	if err := os.Remove(f); err != nil {
		t.Fatalf("删除源文件失败: %v", err)
	}
	if _, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeFull); err != nil {
		t.Fatalf("二次全量扫描失败: %v", err)
	}
	var after models.MediaFile
	if err := svc.db.First(&after, mf.ID).Error; err != nil {
		t.Fatalf("查询二次扫描后记录失败: %v", err)
	}
	if before.DeletedAt == nil || after.DeletedAt == nil {
		t.Fatal("软删记录 deleted_at 不应为空")
	}
	if !before.DeletedAt.Equal(*after.DeletedAt) {
		t.Fatalf("已软删记录的 deleted_at 不应被对账改写: before=%v after=%v", before.DeletedAt, after.DeletedAt)
	}
}
