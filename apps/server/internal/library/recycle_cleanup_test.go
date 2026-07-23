package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// createSoftDeletedMedia 在指定库内创建一条媒体记录并立即软删（deleted_at=指定时刻）。
// 返回入库后的记录，filePath 直接落库（已正斜杠化）。
func createSoftDeletedMedia(t *testing.T, svc *Service, libraryID int64, filePath string, deletedAt time.Time) *models.MediaFile {
	t.Helper()
	mf, err := svc.CreateMediaFile(libraryID, filePath, 100)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	if err := svc.db.Model(&models.MediaFile{}).Where("id = ?", mf.ID).Update("deleted_at", deletedAt).Error; err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}
	mf.DeletedAt = &deletedAt
	return mf
}

func expectedRecycleTarget(recycleDir string, mf *models.MediaFile) string {
	dateDir := "未知日期"
	if mf.DeletedAt != nil {
		dateDir = mf.DeletedAt.Format("2006-01-02")
	}
	name := ".jianvideo-" + strconv.FormatInt(mf.ID, 10) + "-" + mf.FileName
	return filepath.Join(recycleDir, dateDir, name)
}

func injectGORMCallbackError(t *testing.T, tx *gorm.DB, injectedErr error) {
	t.Helper()
	if addErr := tx.AddError(injectedErr); !errors.Is(addErr, injectedErr) {
		t.Errorf("GORM 回调错误注入未生效: got=%v want=%v", addErr, injectedErr)
	}
}

func injectMediaFileDeleteFailure(t *testing.T, db *gorm.DB, beforeFailure func()) {
	t.Helper()
	const callbackName = "test:fail_recycle_media_delete"
	injectedErr := errors.New("注入媒体记录删除失败")
	failed := false
	if err := db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if failed || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media_files" {
			return
		}
		failed = true
		if beforeFailure != nil {
			beforeFailure()
		}
		injectGORMCallbackError(t, tx, injectedErr)
	}); err != nil {
		t.Fatalf("注册媒体删除失败注入失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Delete().Remove(callbackName)
	})
}

func newConcurrentRecycleTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dbPath := filepath.ToSlash(filepath.Join(t.TempDir(), "recycle-concurrency.db"))
	dsn := "file:" + dbPath + "?_journal_mode=WAL&_busy_timeout=100"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开并发测试数据库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取并发测试数据库连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}, &models.MediaHashGroup{}, &models.WatchState{}); err != nil {
		t.Fatalf("迁移并发测试数据库失败: %v", err)
	}
	return NewService(gdb), gdb
}

// TestCleanupRecycle_UnsetPathRejected 软删项所在盘符未配置回收站路径时，整体拒绝、不移动不删除。
func TestCleanupRecycle_UnsetPathRejected(t *testing.T) {
	svc, _ := newTestService(t)

	// 在临时目录里造一个真实源文件，软删它
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "误删.mp4")
	if err := os.WriteFile(srcFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	srcSlash := filepath.ToSlash(srcFile)
	mf := createSoftDeletedMedia(t, svc, 1, srcSlash, time.Now())

	// 传入一个不含该盘符（或为空）的配置映射 → 期望拒绝
	_, err := svc.CleanupRecycle(map[string]string{})
	if !errors.Is(err, ErrRecycleBinPathUnset) {
		t.Fatalf("期望 ErrRecycleBinPathUnset, 实际 %v", err)
	}

	// 源文件仍在原位
	if _, statErr := os.Stat(srcFile); statErr != nil {
		t.Fatalf("拒绝清理后源文件应仍在原位, 实际: %v", statErr)
	}
	// 记录仍在（仍可在回收站查到）
	deleted, _ := svc.ListDeletedMediaFiles()
	if len(deleted) != 1 || deleted[0].ID != mf.ID {
		t.Fatalf("拒绝清理后软删记录应仍在, 实际: %+v", deleted)
	}
}

// TestCleanupRecycle_MovesFileAndDeletesRecord 配置盘符路径后，源文件移动到回收站按删除日期分目录、记录被删。
func TestCleanupRecycle_MovesFileAndDeletesRecord(t *testing.T) {
	svc, db := newTestService(t)

	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "片子.mkv")
	if err := os.WriteFile(srcFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	srcSlash := filepath.ToSlash(srcFile)
	// 解析出盘符（Windows 为盘符字母，类 Unix 临时目录无盘符则跳过此用例）
	drive := driveOfPath(srcSlash)
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过真实移动用例")
	}

	deletedAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local)
	mf := createSoftDeletedMedia(t, svc, 1, srcSlash, deletedAt)
	if err := db.Model(&models.MediaFile{}).Where("id = ?", mf.ID).Updates(map[string]any{
		"content_hash":       strings.Repeat("0", 64),
		"content_hash_algo":  ContentHashAlgoSHA256,
		"content_hash_stale": true,
	}).Error; err != nil {
		t.Fatalf("设置过期内容哈希失败: %v", err)
	}
	watchState := models.WatchState{
		SpaceID: mf.SpaceID, MediaID: mf.ID, PositionSeconds: 10,
		LastWatchedAt: deletedAt, Revision: 1, CreatedAt: deletedAt, UpdatedAt: deletedAt,
	}
	if err := db.Create(&watchState).Error; err != nil {
		t.Fatalf("创建观看状态失败: %v", err)
	}

	// 回收站目录配在同盘符下的临时目录（保证同卷可 Rename）
	recycleDir := filepath.Join(srcDir, ".recycle")
	result, err := svc.CleanupRecycle(map[string]string{drive: filepath.ToSlash(recycleDir)})
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if result.Moved != 1 || result.Failed != 0 {
		t.Fatalf("期望 moved=1 failed=0, 实际 %+v", result)
	}

	// 源文件已移走
	if _, statErr := os.Stat(srcFile); !os.IsNotExist(statErr) {
		t.Fatalf("清理后源文件应已移走, 实际 stat err=%v", statErr)
	}
	// 目标按删除日期分目录存在，且名称由媒体 ID 确定以支持重试恢复
	dest := expectedRecycleTarget(recycleDir, mf)
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Fatalf("目标文件应存在于 %s, 实际: %v", dest, statErr)
	}
	manifestPath := dest + ".manifest.json"
	manifestData, manifestErr := os.ReadFile(manifestPath)
	if manifestErr != nil {
		t.Fatalf("成功暂存后必须写入归属清单: %v", manifestErr)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("归属清单必须是合法 JSON: %v", err)
	}
	expectedContentHash := sha256.Sum256([]byte("hello"))
	if manifest["media_id"] != float64(mf.ID) || manifest["source_path_sha256"] == "" ||
		manifest["content_sha256"] != hex.EncodeToString(expectedContentHash[:]) || manifest["staged_size"] != float64(len("hello")) {
		t.Fatalf("归属清单缺少媒体 ID、源路径摘要、内容摘要或实际大小: %+v", manifest)
	}
	// 记录被删（回收站清空）
	deleted, _ := svc.ListDeletedMediaFiles()
	if len(deleted) != 0 {
		t.Fatalf("清理后回收站应为空, 实际还剩 %d 条", len(deleted))
	}
	var stateCount int64
	if err := db.Model(&models.WatchState{}).Where("space_id = ? AND media_id = ?", mf.SpaceID, mf.ID).Count(&stateCount).Error; err != nil {
		t.Fatalf("统计观看状态失败: %v", err)
	}
	if stateCount != 0 {
		t.Fatalf("回收站物理清理后不应遗留观看状态, 实际 %d 条", stateCount)
	}
}

func TestCleanupRecycle_RestoreAfterSnapshotPreventsMove(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "已还原.mkv")
	original := []byte("用户已还原的媒体")
	if err := os.WriteFile(srcFile, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(srcFile))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过清理与还原交错用例")
	}
	mf := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(srcFile), time.Now())
	snapshot := *mf
	if err := svc.RestoreMediaFileInSpace(mf.SpaceID, mf.ID); err != nil {
		t.Fatalf("并发还原失败: %v", err)
	}

	recycleDir := filepath.Join(srcDir, ".recycle")
	completed, err := svc.cleanupRecycleItem(mf.SpaceID, map[string]string{drive: recycleDir}, snapshot)
	if err != nil {
		t.Fatalf("过期软删快照应被安全跳过: %v", err)
	}
	if completed {
		t.Fatal("记录已还原后，过期清理快照不得完成移动或删除")
	}
	got, err := os.ReadFile(srcFile)
	if err != nil || string(got) != string(original) {
		t.Fatalf("记录已还原后源文件必须留在原位: content=%q err=%v", got, err)
	}
	if _, err := os.Lstat(expectedRecycleTarget(recycleDir, mf)); !os.IsNotExist(err) {
		t.Fatalf("记录已还原后不得创建回收站目标: %v", err)
	}
	var stored models.MediaFile
	if err := db.Where("space_id = ? AND id = ?", mf.SpaceID, mf.ID).First(&stored).Error; err != nil {
		t.Fatalf("记录已还原后数据库记录必须保留: %v", err)
	}
	if stored.DeletedAt != nil {
		t.Fatalf("记录已还原后 deleted_at 必须保持为空: %v", stored.DeletedAt)
	}
}

func TestCleanupRecycle_RestoreAfterMoveCannotWinClaim(t *testing.T) {
	svc, db := newConcurrentRecycleTestService(t)
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "移动后还原.mkv")
	if err := os.WriteFile(srcFile, []byte("原始媒体"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(srcFile))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过移动后还原交错用例")
	}
	mf := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(srcFile), time.Now())
	recycleDir := filepath.Join(srcDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, mf)

	queryPaused := make(chan struct{})
	releaseQuery := make(chan struct{})
	var paused atomic.Bool
	const callbackName = "test:pause_recycle_delete_query"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media_files" {
			return
		}
		if _, err := os.Lstat(srcFile); err == nil || !os.IsNotExist(err) || !paused.CompareAndSwap(false, true) {
			return
		}
		close(queryPaused)
		<-releaseQuery
	}); err != nil {
		t.Fatalf("注册删除前暂停回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	type cleanupOutcome struct {
		completed bool
		err       error
	}
	cleanupDone := make(chan cleanupOutcome, 1)
	go func() {
		completed, err := svc.cleanupRecycleItem(mf.SpaceID, map[string]string{drive: recycleDir}, *mf)
		cleanupDone <- cleanupOutcome{completed: completed, err: err}
	}()

	select {
	case <-queryPaused:
	case <-time.After(3 * time.Second):
		t.Fatal("清理未在移动后、数据库删除前进入暂停点")
	}
	if _, err := os.Lstat(srcFile); !os.IsNotExist(err) {
		t.Fatalf("暂停点应位于文件移动之后: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("暂停点应保留已暂存目标: %v", err)
	}

	restoreDone := make(chan error, 1)
	go func() { restoreDone <- svc.RestoreMediaFileInSpace(mf.SpaceID, mf.ID) }()
	var restoreErr error
	select {
	case restoreErr = <-restoreDone:
	case <-time.After(3 * time.Second):
		close(releaseQuery)
		t.Fatal("并发还原未在数据库锁冲突后及时返回")
	}
	close(releaseQuery)
	outcome := <-cleanupDone
	if restoreErr == nil {
		t.Fatal("清理已取得条件 claim 并移动文件后，并发还原不得成功清空 deleted_at")
	}
	if outcome.err != nil || !outcome.completed {
		t.Fatalf("取得 claim 的清理应完成数据库删除: completed=%v err=%v", outcome.completed, outcome.err)
	}
	var count int64
	if err := db.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", mf.SpaceID, mf.ID).Count(&count).Error; err != nil {
		t.Fatalf("统计清理后记录失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("取得 claim 的清理完成后记录应删除, 实际 %d", count)
	}
}

func TestCleanupRecycle_StagedTargetWithoutManifestPreservesRecord(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "旧暂存.mkv")
	if err := os.WriteFile(srcFile, []byte("原始文件"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(srcFile))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过旧暂存归属校验用例")
	}
	mf := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(srcFile), time.Now())
	recycleDir := filepath.Join(srcDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, mf)
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatalf("创建暂存目录失败: %v", err)
	}
	if err := os.Remove(srcFile); err != nil {
		t.Fatalf("移除源文件失败: %v", err)
	}
	foreign := []byte("无法证明归属的旧暂存文件")
	if err := os.WriteFile(target, foreign, 0o644); err != nil {
		t.Fatalf("写旧暂存文件失败: %v", err)
	}

	completed, err := svc.cleanupRecycleItem(mf.SpaceID, map[string]string{drive: recycleDir}, *mf)
	if err == nil || completed {
		t.Fatalf("无归属清单的旧暂存文件必须拒绝删除数据库: completed=%v err=%v", completed, err)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || string(got) != string(foreign) {
		t.Fatalf("无法证明归属时不得改动暂存文件: content=%q err=%v", got, readErr)
	}
	var stored models.MediaFile
	if err := db.Where("space_id = ? AND id = ?", mf.SpaceID, mf.ID).First(&stored).Error; err != nil {
		t.Fatalf("无法证明归属时必须保留数据库记录: %v", err)
	}
}

func TestCleanupRecycle_RejectsNonRegularStagedTargets(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "目录",
			setup: func(t *testing.T, target string) {
				t.Helper()
				if err := os.Mkdir(target, 0o750); err != nil {
					t.Fatalf("创建目录目标失败: %v", err)
				}
			},
		},
		{
			name: "文件符号链接",
			setup: func(t *testing.T, target string) {
				t.Helper()
				outside := filepath.Join(t.TempDir(), "outside.mkv")
				if err := os.WriteFile(outside, []byte("外部文件"), 0o644); err != nil {
					t.Fatalf("写外部文件失败: %v", err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Skipf("当前环境不支持文件符号链接: %v", err)
				}
			},
		},
		{
			name: "目录符号链接",
			setup: func(t *testing.T, target string) {
				t.Helper()
				outside := t.TempDir()
				if err := os.Symlink(outside, target); err != nil {
					t.Skipf("当前环境不支持目录符号链接或 junction 等价场景: %v", err)
				}
			},
		},
		{
			name: "悬空符号链接",
			setup: func(t *testing.T, target string) {
				t.Helper()
				missing := filepath.Join(t.TempDir(), "missing.mkv")
				if err := os.Symlink(missing, target); err != nil {
					t.Skipf("当前环境不支持悬空符号链接: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, db := newTestService(t)
			srcDir := t.TempDir()
			srcFile := filepath.Join(srcDir, "非普通目标.mkv")
			if err := os.WriteFile(srcFile, []byte("原始文件"), 0o644); err != nil {
				t.Fatalf("写源文件失败: %v", err)
			}
			drive := driveOfPath(filepath.ToSlash(srcFile))
			if drive == "" {
				t.Skip("当前平台临时目录无盘符，跳过非普通暂存目标用例")
			}
			mf := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(srcFile), time.Now())
			recycleDir := filepath.Join(srcDir, ".recycle")
			target := expectedRecycleTarget(recycleDir, mf)
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				t.Fatalf("创建暂存目录失败: %v", err)
			}
			if err := os.Remove(srcFile); err != nil {
				t.Fatalf("移除源文件失败: %v", err)
			}
			tc.setup(t, target)

			completed, err := svc.cleanupRecycleItem(mf.SpaceID, map[string]string{drive: recycleDir}, *mf)
			if err == nil || completed {
				t.Fatalf("非普通暂存目标必须拒绝删除数据库: completed=%v err=%v", completed, err)
			}
			var count int64
			if err := db.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", mf.SpaceID, mf.ID).Count(&count).Error; err != nil {
				t.Fatalf("统计数据库记录失败: %v", err)
			}
			if count != 1 {
				t.Fatalf("非普通暂存目标必须保留数据库记录, 实际 %d", count)
			}
		})
	}
}

func TestRestoreRecycleSource_RejectsNonRegularStagedFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	staged := filepath.Join(dir, "staged.mkv")
	if err := os.Mkdir(staged, 0o750); err != nil {
		t.Fatalf("创建目录暂存目标失败: %v", err)
	}

	if err := restoreRecycleSource(source, staged); err == nil {
		t.Fatal("恢复补偿必须拒绝目录等非普通暂存目标")
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatalf("拒绝非普通暂存目标后不得创建源路径: %v", err)
	}
	if info, err := os.Lstat(staged); err != nil || !info.IsDir() {
		t.Fatalf("拒绝后目录暂存目标必须保留: info=%v err=%v", info, err)
	}
}

func TestRestoreRecycleSource_ConcurrentCreateDoesNotOverwrite(t *testing.T) {
	for i := 0; i < 64; i++ {
		dir := t.TempDir()
		source := filepath.Join(dir, "source.mkv")
		staged := filepath.Join(dir, "staged.mkv")
		original := []byte("原始媒体")
		created := []byte("并发新文件")
		if err := os.WriteFile(staged, original, 0o644); err != nil {
			t.Fatalf("写暂存文件失败: %v", err)
		}
		start := make(chan struct{})
		restoreDone := make(chan error, 1)
		createDone := make(chan error, 1)
		go func() {
			<-start
			restoreDone <- restoreRecycleSource(source, staged)
		}()
		go func() {
			<-start
			file, err := os.OpenFile(source, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err == nil {
				_, err = file.Write(created)
				if closeErr := file.Close(); err == nil {
					err = closeErr
				}
			}
			createDone <- err
		}()
		close(start)
		restoreErr := <-restoreDone
		createErr := <-createDone

		switch {
		case restoreErr == nil:
			if !os.IsExist(createErr) {
				t.Fatalf("恢复成功时并发创建必须原子失败: restoreErr=%v createErr=%v", restoreErr, createErr)
			}
			got, err := os.ReadFile(source)
			if err != nil || string(got) != string(original) {
				t.Fatalf("恢复成功后源文件内容必须是原始媒体: content=%q err=%v", got, err)
			}
		case createErr == nil:
			if !errors.Is(restoreErr, errRecycleSourceOccupied) {
				t.Fatalf("并发创建成功时恢复必须稳定返回 source_occupied: %v", restoreErr)
			}
			got, err := os.ReadFile(source)
			if err != nil || string(got) != string(created) {
				t.Fatalf("并发创建成功后不得覆盖新文件: content=%q err=%v", got, err)
			}
			got, err = os.ReadFile(staged)
			if err != nil || string(got) != string(original) {
				t.Fatalf("目标占用时原始暂存文件必须保留: content=%q err=%v", got, err)
			}
		default:
			t.Fatalf("恢复与并发创建必须恰有一方成功: restoreErr=%v createErr=%v", restoreErr, createErr)
		}
	}
}

func TestCleanupRecycle_DeleteFailureRestoresSourceFile(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "待补偿.mkv")
	original := []byte("原始媒体内容")
	if err := os.WriteFile(srcFile, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(srcFile))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过 Windows 原子补偿用例")
	}
	deletedAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.Local)
	mf := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(srcFile), deletedAt)
	injectMediaFileDeleteFailure(t, db, nil)

	recycleDir := filepath.Join(srcDir, ".recycle")
	result, err := svc.CleanupRecycle(map[string]string{drive: filepath.ToSlash(recycleDir)})
	if err != nil {
		t.Fatalf("数据库失败但文件补偿成功时不应返回整体错误: %v", err)
	}
	if result.Moved != 0 || result.Failed != 1 {
		t.Fatalf("补偿成功后期望 moved=0 failed=1, 实际 %+v", result)
	}
	got, statErr := os.ReadFile(srcFile)
	if statErr != nil {
		t.Fatalf("数据库删除失败后源文件应恢复原位: %v", statErr)
	}
	if string(got) != string(original) {
		t.Fatalf("恢复后的源文件内容不正确: %q", got)
	}
	dest := expectedRecycleTarget(recycleDir, mf)
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("补偿成功后回收站目标不应残留, stat err=%v", statErr)
	}
	var stored models.MediaFile
	if err := db.Where("space_id = ? AND id = ?", mf.SpaceID, mf.ID).First(&stored).Error; err != nil {
		t.Fatalf("补偿成功后数据库软删记录应保留: %v", err)
	}
	if stored.FilePath != filepath.ToSlash(srcFile) {
		t.Fatalf("数据库仍应指向已恢复的源路径, 实际 %s", stored.FilePath)
	}
}

func TestCleanupRecycle_CompensationFailurePreservesRecreatedSource(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "冲突.mkv")
	original := []byte("原始媒体内容")
	if err := os.WriteFile(srcFile, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(srcFile))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过 Windows 目标冲突用例")
	}
	deletedAt := time.Date(2026, 7, 15, 11, 0, 0, 0, time.Local)
	mf := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(srcFile), deletedAt)
	recreated := []byte("用户重新创建的文件")
	var recreateErr error
	injectMediaFileDeleteFailure(t, db, func() {
		recreateErr = os.WriteFile(srcFile, recreated, 0o644)
	})

	recycleDir := filepath.Join(srcDir, ".recycle")
	result, err := svc.CleanupRecycle(map[string]string{drive: filepath.ToSlash(recycleDir)})
	if recreateErr != nil {
		t.Fatalf("重建源路径冲突文件失败: %v", recreateErr)
	}
	if err == nil || !strings.Contains(err.Error(), "补偿失败") ||
		!strings.Contains(err.Error(), "数据库删除失败") || !strings.Contains(err.Error(), "文件恢复失败") {
		t.Fatalf("源路径已被重建时必须返回包含双重原因的补偿错误, 实际 %v", err)
	}
	if result.Moved != 0 || result.Failed != 1 {
		t.Fatalf("补偿失败后期望 moved=0 failed=1, 实际 %+v", result)
	}
	gotSource, readErr := os.ReadFile(srcFile)
	if readErr != nil {
		t.Fatalf("用户重建的源文件不应被删除: %v", readErr)
	}
	if string(gotSource) != string(recreated) {
		t.Fatalf("用户重建的源文件内容不应被覆盖, 实际 %q", gotSource)
	}
	dest := expectedRecycleTarget(recycleDir, mf)
	gotDest, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("补偿失败时原始媒体应留在回收站目标供诊断恢复: %v", readErr)
	}
	if string(gotDest) != string(original) {
		t.Fatalf("回收站目标中的原始媒体内容不正确: %q", gotDest)
	}
	var stored models.MediaFile
	if err := db.Where("space_id = ? AND id = ?", mf.SpaceID, mf.ID).First(&stored).Error; err != nil {
		t.Fatalf("补偿失败后数据库软删记录应保留: %v", err)
	}

	retryResult, retryErr := svc.CleanupRecycle(map[string]string{drive: filepath.ToSlash(recycleDir)})
	if retryErr == nil {
		t.Fatal("源路径仍被新文件占用时重试必须返回恢复冲突")
	}
	if retryResult.Moved != 0 || retryResult.Failed != 1 {
		t.Fatalf("恢复冲突重试应保留部分结果: %+v", retryResult)
	}
	if got, readErr := os.ReadFile(srcFile); readErr != nil || string(got) != string(recreated) {
		t.Fatalf("恢复冲突重试不得移动或删除新源文件: content=%q err=%v", got, readErr)
	}
	if got, readErr := os.ReadFile(dest); readErr != nil || string(got) != string(original) {
		t.Fatalf("恢复冲突重试不得移动或删除已暂存目标: content=%q err=%v", got, readErr)
	}
	if err := db.Where("space_id = ? AND id = ?", mf.SpaceID, mf.ID).First(&stored).Error; err != nil {
		t.Fatalf("恢复冲突期间数据库软删记录必须保留: %v", err)
	}

	if err := os.Remove(srcFile); err != nil {
		t.Fatalf("移除冲突源文件以继续恢复失败: %v", err)
	}
	tampered := bytes.Repeat([]byte("x"), len(original))
	if err := os.WriteFile(dest, tampered, 0o644); err != nil {
		t.Fatalf("替换为同尺寸不同内容的暂存目标失败: %v", err)
	}
	tamperedResult, tamperedErr := svc.CleanupRecycle(map[string]string{drive: filepath.ToSlash(recycleDir)})
	if tamperedErr == nil || tamperedResult.Moved != 0 || tamperedResult.Failed != 1 {
		t.Fatalf("同尺寸不同内容的暂存目标必须拒绝并报告残余: result=%+v err=%v", tamperedResult, tamperedErr)
	}
	if got, readErr := os.ReadFile(dest); readErr != nil || !bytes.Equal(got, tampered) {
		t.Fatalf("内容校验失败时不得改动暂存目标: content=%q err=%v", got, readErr)
	}
	if err := db.Where("space_id = ? AND id = ?", mf.SpaceID, mf.ID).First(&stored).Error; err != nil {
		t.Fatalf("内容校验失败时数据库软删记录必须保留: %v", err)
	}
	if err := os.WriteFile(dest, original, 0o644); err != nil {
		t.Fatalf("恢复真实暂存目标失败: %v", err)
	}
	converged, err := svc.CleanupRecycle(map[string]string{drive: filepath.ToSlash(recycleDir)})
	if err != nil {
		t.Fatalf("恢复真实目标后重试应完成数据库删除: %v", err)
	}
	if converged.Moved != 1 || converged.Failed != 0 {
		t.Fatalf("恢复重试应收敛为 moved=1 failed=0: %+v", converged)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("恢复收敛后原始媒体应保留在回收站目标: %v", err)
	}
	if err := db.Where("space_id = ? AND id = ?", mf.SpaceID, mf.ID).First(&stored).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("恢复收敛后数据库软删记录应删除: %v", err)
	}
}

func openConcurrentRecyclePeer(t *testing.T, db *gorm.DB) *gorm.DB {
	t.Helper()
	dialector, ok := db.Dialector.(*sqlite.Dialector)
	if !ok {
		t.Fatal("并发回收测试必须使用 SQLite")
	}
	peer, err := gorm.Open(sqlite.Open(dialector.DSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开并发回收测试连接失败: %v", err)
	}
	sqlDB, err := peer.DB()
	if err != nil {
		t.Fatalf("获取并发回收测试底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return peer
}

func TestDeleteRecycleClaim_IdentityMutationRollsBackRelations(t *testing.T) {
	mutations := []struct {
		name   string
		column string
		value  any
	}{
		{name: "space_id", column: "space_id", value: "space-raced"},
		{name: "library_id", column: "library_id", value: int64(99)},
		{name: "file_path", column: "file_path", value: "C:/raced/path.mkv"},
		{name: "file_name", column: "file_name", value: "raced.mkv"},
		{name: "deleted_at", column: "deleted_at", value: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)},
		{name: "file_state", column: "file_state", value: "raced"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			svc, db := newTestService(t)
			media := createSoftDeletedMedia(t, svc, 1, "/tmp/cas-"+mutation.name+".mkv", time.Now().UTC())
			claim, claimed, err := svc.claimRecycleMedia(media.SpaceID, *media)
			if err != nil || !claimed {
				t.Fatalf("取得回收站清理 claim 失败: claimed=%v err=%v", claimed, err)
			}
			now := time.Now().UTC()
			state := models.WatchState{
				SpaceID: claim.media.SpaceID, MediaID: claim.media.ID, PositionSeconds: 10,
				LastWatchedAt: now, Revision: 1, CreatedAt: now, UpdatedAt: now,
			}
			if err := db.Create(&state).Error; err != nil {
				t.Fatalf("创建关联观看状态失败: %v", err)
			}

			const callbackName = "test:mutate_recycle_identity_before_delete"
			mutated := false
			if err := db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
				if mutated || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media_files" {
					return
				}
				mutated = true
				err := tx.Session(&gorm.Session{NewDB: true}).Model(&models.MediaFile{}).
					Where("id = ?", claim.media.ID).
					UpdateColumn(mutation.column, mutation.value).Error
				if err != nil {
					injectGORMCallbackError(t, tx, err)
				}
			}); err != nil {
				t.Fatalf("注册 identity 竞态注入失败: %v", err)
			}
			t.Cleanup(func() { _ = db.Callback().Delete().Remove(callbackName) })

			_, rows, err := svc.deleteRecycleClaim(claim)
			if err == nil || rows != 0 {
				t.Fatalf("identity 变化后最终 CAS DELETE 必须为 0 行并回滚: rows=%d err=%v", rows, err)
			}
			if !mutated {
				t.Fatal("未在候选查询后、最终 DELETE 前注入 identity 变化")
			}
			var stored models.MediaFile
			if err := db.Where("id = ?", claim.media.ID).First(&stored).Error; err != nil {
				t.Fatalf("CAS 失败后媒体记录必须保留: %v", err)
			}
			if stored.SpaceID != claim.media.SpaceID || stored.LibraryID != claim.media.LibraryID ||
				stored.FilePath != claim.media.FilePath || stored.FileName != claim.media.FileName ||
				stored.DeletedAt == nil || !stored.DeletedAt.Equal(*claim.media.DeletedAt) || stored.FileState != claim.state {
				t.Fatalf("CAS 失败后 identity 变化必须随事务回滚: %+v", stored)
			}
			var stateCount int64
			if err := db.Model(&models.WatchState{}).
				Where("space_id = ? AND media_id = ?", claim.media.SpaceID, claim.media.ID).
				Count(&stateCount).Error; err != nil {
				t.Fatalf("统计关联观看状态失败: %v", err)
			}
			if stateCount != 1 {
				t.Fatalf("最终 CAS 失败时关联删除必须完整回滚, 实际 %d 条", stateCount)
			}
		})
	}
}

func TestCleanupRecycle_DoesNotHoldWriteTransactionAcrossFilesystemWork(t *testing.T) {
	svc, db := newConcurrentRecycleTestService(t)
	peer := openConcurrentRecyclePeer(t, db)
	srcDir := t.TempDir()
	source := filepath.Join(srcDir, "短事务.mkv")
	if err := os.WriteFile(source, bytes.Repeat([]byte("a"), 4*1024*1024), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(source))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过 SQLite 短事务用例")
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now())
	control, err := svc.CreateMediaFile(1, filepath.ToSlash(filepath.Join(srcDir, "并发写.mp4")), 1)
	if err != nil {
		t.Fatalf("创建并发写控制记录失败: %v", err)
	}
	recycleDir := filepath.Join(srcDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)

	var triggered atomic.Bool
	var concurrentWriteErr error
	const callbackName = "test:write_during_recycle_filesystem_phase"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if triggered.Load() || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media_files" {
			return
		}
		if _, err := os.Lstat(source); err == nil || !os.IsNotExist(err) {
			return
		}
		if _, err := os.Lstat(target); err != nil || !triggered.CompareAndSwap(false, true) {
			return
		}
		concurrentWriteErr = peer.Model(&models.MediaFile{}).
			Where("id = ?", control.ID).
			Update("notes", "并发写成功").Error
	}); err != nil {
		t.Fatalf("注册文件系统阶段并发写回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	result, err := svc.CleanupRecycle(map[string]string{drive: recycleDir})
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if result.Moved != 1 || result.Failed != 0 {
		t.Fatalf("清理结果异常: %+v", result)
	}
	if !triggered.Load() {
		t.Fatal("未在文件系统阶段后的最终数据库查询触发并发写")
	}
	if concurrentWriteErr != nil {
		t.Fatalf("大文件哈希与文件系统操作期间不得持有 SQLite 写事务: %v", concurrentWriteErr)
	}
}

type recycleRelationFixture struct {
	watch    models.WatchState
	metadata models.MediaMetadata
	chapter  models.MediaChapter
	bookmark models.MediaBookmark
}

func createRecycleRelationFixture(t *testing.T, db *gorm.DB, media *models.MediaFile) recycleRelationFixture {
	t.Helper()
	if err := db.AutoMigrate(&models.MediaMetadata{}, &models.MediaChapter{}, &models.MediaBookmark{}); err != nil {
		t.Fatalf("迁移回收补偿关联表失败: %v", err)
	}
	createdAt := time.Date(2026, 7, 15, 8, 9, 10, 0, time.UTC)
	updatedAt := createdAt.Add(2 * time.Hour)
	completedAt := createdAt.Add(time.Hour)
	note := "原始书签备注"
	fixture := recycleRelationFixture{
		watch: models.WatchState{
			SpaceID: media.SpaceID, MediaID: media.ID, PositionSeconds: 123.5, Completed: true,
			LastWatchedAt: updatedAt, CompletedAt: &completedAt, Revision: 7,
			LastSessionID: "session-original", LastEventSeq: 19, CreatedAt: createdAt, UpdatedAt: updatedAt,
		},
		metadata: models.MediaMetadata{
			SpaceID: media.SpaceID, MediaID: media.ID, Source: "ffprobe", Tool: "ffprobe",
			ToolVersion: "7.1", RawJSON: `{"raw":"原始"}`, NormalizedJSON: `{"normalized":true}`,
			ParsedAt: updatedAt, Stale: true,
		},
		chapter: models.MediaChapter{
			ID: "chapter-original", SpaceID: media.SpaceID, MediaID: media.ID,
			Source: models.MediaChapterSourceEmbedded, SourceIndex: 3, StartMS: 1250, EndMS: 9750,
			Title: "原始章节", Language: "zh-CN", SourceFingerprint: "fingerprint-original",
			ParsedAt: createdAt, CreatedAt: createdAt, UpdatedAt: updatedAt,
		},
		bookmark: models.MediaBookmark{
			ID: "bookmark-original", SpaceID: media.SpaceID, MediaID: media.ID,
			PositionMS: 4321, Title: "原始书签", Note: &note, Revision: 5,
			CreatedAt: createdAt, UpdatedAt: updatedAt,
		},
	}
	for _, row := range []any{&fixture.watch, &fixture.metadata, &fixture.chapter, &fixture.bookmark} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("创建回收补偿关联数据失败: %v", err)
		}
	}
	return fixture
}

func assertRecycleSnapshotRestored(t *testing.T, db *gorm.DB, media *models.MediaFile, fixture recycleRelationFixture) {
	t.Helper()
	var stored models.MediaFile
	if err := db.Where("id = ?", media.ID).First(&stored).Error; err != nil {
		t.Fatalf("post-CAS 补偿后媒体记录必须恢复: %v", err)
	}
	if stored.SpaceID != media.SpaceID || stored.LibraryID != media.LibraryID || stored.FilePath != media.FilePath ||
		stored.FileName != media.FileName || stored.DisplayName != media.DisplayName || stored.Notes != media.Notes ||
		stored.FileState != media.FileState || stored.DeletedAt == nil || media.DeletedAt == nil || !stored.DeletedAt.Equal(*media.DeletedAt) {
		t.Fatalf("post-CAS 补偿未逐字段恢复媒体记录: got=%+v want=%+v", stored, *media)
	}
	assertRecycleWatchStateRestored(t, db, fixture.watch)
	assertRecycleMetadataRestored(t, db, fixture.metadata)
	assertRecycleChapterRestored(t, db, fixture.chapter)
	assertRecycleBookmarkRestored(t, db, fixture.bookmark)
}

func assertRecycleWatchStateRestored(t *testing.T, db *gorm.DB, want models.WatchState) {
	t.Helper()
	var got models.WatchState
	if err := db.Where("space_id = ? AND media_id = ?", want.SpaceID, want.MediaID).First(&got).Error; err != nil {
		t.Fatalf("观看状态未恢复: %v", err)
	}
	completedAtEqual := got.CompletedAt != nil && want.CompletedAt != nil && got.CompletedAt.Equal(*want.CompletedAt)
	if got.SpaceID != want.SpaceID || got.MediaID != want.MediaID || got.PositionSeconds != want.PositionSeconds ||
		got.Completed != want.Completed || !got.LastWatchedAt.Equal(want.LastWatchedAt) || !completedAtEqual ||
		got.Revision != want.Revision || got.LastSessionID != want.LastSessionID || got.LastEventSeq != want.LastEventSeq ||
		!got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("观看状态未逐字段恢复: got=%+v want=%+v", got, want)
	}
}

func assertRecycleMetadataRestored(t *testing.T, db *gorm.DB, want models.MediaMetadata) {
	t.Helper()
	var got models.MediaMetadata
	if err := db.Where("id = ?", want.ID).First(&got).Error; err != nil {
		t.Fatalf("媒体元数据未恢复: %v", err)
	}
	if got != want {
		t.Fatalf("媒体元数据未逐字段恢复: got=%+v want=%+v", got, want)
	}
}

func assertRecycleChapterRestored(t *testing.T, db *gorm.DB, want models.MediaChapter) {
	t.Helper()
	var got models.MediaChapter
	if err := db.Where("id = ?", want.ID).First(&got).Error; err != nil {
		t.Fatalf("媒体章节未恢复: %v", err)
	}
	if got != want {
		t.Fatalf("媒体章节未逐字段恢复: got=%+v want=%+v", got, want)
	}
}

func assertRecycleBookmarkRestored(t *testing.T, db *gorm.DB, want models.MediaBookmark) {
	t.Helper()
	var got models.MediaBookmark
	if err := db.Where("id = ?", want.ID).First(&got).Error; err != nil {
		t.Fatalf("媒体书签未恢复: %v", err)
	}
	noteEqual := got.Note != nil && want.Note != nil && *got.Note == *want.Note
	if got.ID != want.ID || got.SpaceID != want.SpaceID || got.MediaID != want.MediaID ||
		got.PositionMS != want.PositionMS || got.Title != want.Title || !noteEqual || got.Revision != want.Revision ||
		!got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("媒体书签未逐字段恢复: got=%+v want=%+v", got, want)
	}
}

func assertRecycleFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("回收补偿文件内容不正确: path=%s content=%q err=%v", path, got, err)
	}
}

func TestCleanupRecycle_PostCASFailureRestoresFullSnapshot(t *testing.T) {
	svc, db := newTestService(t)
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "post-cas-完整恢复.mkv")
	original := []byte("post-CAS 补偿必须恢复的原始字节")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now().UTC())
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).
		Updates(map[string]any{"display_name": "原始显示名", "notes": "原始备注"}).Error; err != nil {
		t.Fatalf("更新媒体前像失败: %v", err)
	}
	if err := db.First(media, media.ID).Error; err != nil {
		t.Fatalf("读取媒体前像失败: %v", err)
	}
	fixture := createRecycleRelationFixture(t, db, media)
	recycleDir := filepath.Join(sourceDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	var hookErr error
	svc.afterRecycleDelete = func() {
		hookErr = os.WriteFile(recycleManifestPath(target), []byte("{}"), 0o644)
	}

	completed, err := svc.cleanupRecycleItem(media.SpaceID, map[string]string{driveOfPath(media.FilePath): recycleDir}, *media)
	if hookErr != nil {
		t.Fatalf("注入 post-CAS 归属清单篡改失败: %v", hookErr)
	}
	var recoveryErr *RecycleRecoveryError
	if completed || !errors.As(err, &recoveryErr) || recoveryErr.State != RecycleRecoveryStateTargetStaged {
		t.Fatalf("post-CAS 复核失败必须返回恢复错误: completed=%v err=%v", completed, err)
	}
	assertRecycleFileBytes(t, source, original)
	assertRecycleFileBytes(t, target+recycleRecoverySuffix, original)
	if _, statErr := os.Lstat(recycleManifestPath(target)); statErr != nil {
		t.Fatalf("补偿成功后必须保留归属清单: %v", statErr)
	}
	assertRecycleSnapshotRestored(t, db, media, fixture)
}

func TestCleanupRecycle_PostCASFailureSourceOccupiedDoesNotOverwrite(t *testing.T) {
	svc, db := newTestService(t)
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "post-cas-源占用.mkv")
	original := []byte("应保留在 recovery 的原始字节")
	occupied := []byte("用户新建文件不得覆盖")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now().UTC())
	recycleDir := filepath.Join(sourceDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	var hookErr error
	svc.afterRecycleDelete = func() {
		if hookErr = os.WriteFile(source, occupied, 0o644); hookErr == nil {
			hookErr = os.WriteFile(recycleManifestPath(target), []byte("{}"), 0o644)
		}
	}

	completed, err := svc.cleanupRecycleItem(media.SpaceID, map[string]string{driveOfPath(media.FilePath): recycleDir}, *media)
	if hookErr != nil {
		t.Fatalf("注入 source occupied 失败: %v", hookErr)
	}
	var recoveryErr *RecycleRecoveryError
	if completed || !errors.As(err, &recoveryErr) || recoveryErr.State != RecycleRecoveryStateSourceOccupied {
		t.Fatalf("源路径占用必须返回不覆盖恢复冲突: completed=%v err=%v", completed, err)
	}
	assertRecycleFileBytes(t, source, occupied)
	assertRecycleFileBytes(t, target+recycleRecoverySuffix, original)
	var stored models.MediaFile
	if err := db.First(&stored, media.ID).Error; err != nil {
		t.Fatalf("源路径占用时数据库前像仍必须恢复: %v", err)
	}
	if stored.FileState != media.FileState || stored.DeletedAt == nil || !stored.DeletedAt.Equal(*media.DeletedAt) {
		t.Fatalf("源路径占用时不得遗留 cleanup claim: %+v", stored)
	}
}

func TestCleanupRecycle_PostCASRestoreConflictDoesNotOverwriteNewRelation(t *testing.T) {
	svc, db := newTestService(t)
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "post-cas-关联冲突.mkv")
	original := []byte("数据库冲突时仍应尽力物化的原始字节")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now().UTC())
	fixture := createRecycleRelationFixture(t, db, media)
	recycleDir := filepath.Join(sourceDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	newNote := "并发新书签备注"
	conflict := models.MediaBookmark{
		ID: fixture.bookmark.ID, SpaceID: "space-new", MediaID: media.ID + 100,
		PositionMS: 9999, Title: "并发新书签", Note: &newNote, Revision: 11,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	var hookErr error
	svc.afterRecycleDelete = func() {
		if hookErr = db.Create(&conflict).Error; hookErr == nil {
			hookErr = os.WriteFile(recycleManifestPath(target), []byte("{}"), 0o644)
		}
	}

	completed, err := svc.cleanupRecycleItem(media.SpaceID, map[string]string{driveOfPath(media.FilePath): recycleDir}, *media)
	if hookErr != nil {
		t.Fatalf("注入关联主键冲突失败: %v", hookErr)
	}
	var recoveryErr *RecycleRecoveryError
	if completed || !errors.As(err, &recoveryErr) || recoveryErr.State != RecycleRecoveryStateRestoreFailed || recoveryErr.DatabaseError == nil {
		t.Fatalf("关联主键冲突必须返回高优先级数据库恢复错误: completed=%v err=%v", completed, err)
	}
	assertRecycleFileBytes(t, source, original)
	assertRecycleFileBytes(t, target+recycleRecoverySuffix, original)
	var mediaCount int64
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Count(&mediaCount).Error; err != nil || mediaCount != 0 {
		t.Fatalf("关联冲突时父记录恢复必须整体回滚: count=%d err=%v", mediaCount, err)
	}
	for name, model := range map[string]any{"watch": &models.WatchState{}, "metadata": &models.MediaMetadata{}, "chapter": &models.MediaChapter{}} {
		var count int64
		if err := db.Model(model).Where("space_id = ? AND media_id = ?", media.SpaceID, media.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("关联冲突时 %s 恢复必须整体回滚: count=%d err=%v", name, count, err)
		}
	}
	var storedConflict models.MediaBookmark
	if err := db.Where("id = ?", conflict.ID).First(&storedConflict).Error; err != nil {
		t.Fatalf("并发新书签不得被覆盖: %v", err)
	}
	if storedConflict.SpaceID != conflict.SpaceID || storedConflict.MediaID != conflict.MediaID || storedConflict.Title != conflict.Title ||
		storedConflict.Note == nil || *storedConflict.Note != *conflict.Note || storedConflict.Revision != conflict.Revision {
		t.Fatalf("并发新书签被补偿覆盖: got=%+v want=%+v", storedConflict, conflict)
	}
}

func TestCleanupRecycle_PostCommitTargetReplacementKeepsOriginalBytes(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	source := filepath.Join(srcDir, "提交后替换.mkv")
	original := []byte("数据库提交后仍必须保留的原始媒体")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(source))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过提交后 finalize 竞态用例")
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now())
	recycleDir := filepath.Join(srcDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	recovery := target + recycleRecoverySuffix
	foreign := []byte("提交后替换的新内容")

	var replacementErr error
	var recoveryIdentityErr error
	svc.afterRecycleDelete = func() {
		targetInfo, targetErr := os.Lstat(target)
		recoveryInfo, recoveryErr := os.Lstat(recovery)
		if targetErr != nil || recoveryErr != nil {
			recoveryIdentityErr = errors.Join(targetErr, recoveryErr)
		} else if os.SameFile(targetInfo, recoveryInfo) {
			recoveryIdentityErr = errors.New("恢复副本仍与目标共享 inode")
		}
		if err := os.Remove(target); err != nil {
			replacementErr = err
			return
		}
		replacementErr = os.WriteFile(target, foreign, 0o644)
	}

	result, err := svc.CleanupRecycle(map[string]string{drive: recycleDir})
	if err != nil {
		t.Fatalf("受保护 finalize 应成功: %v", err)
	}
	if result.Moved != 1 || result.Failed != 0 {
		t.Fatalf("清理结果异常: %+v", result)
	}
	if recoveryIdentityErr != nil {
		t.Fatalf("finalize 前必须保留独立 recovery 字节快照: %v", recoveryIdentityErr)
	}
	if replacementErr == nil {
		t.Fatal("数据库提交后、恢复副本删除前的目标替换必须被保护句柄阻止")
	}
	if got, readErr := os.ReadFile(target); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("finalize 竞态后目标必须保留原始字节: content=%q err=%v", got, readErr)
	}
	if _, statErr := os.Lstat(recovery); !os.IsNotExist(statErr) {
		t.Fatalf("成功 finalize 后独立恢复副本应删除: %v", statErr)
	}
	var count int64
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Count(&count).Error; err != nil {
		t.Fatalf("统计媒体记录失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("成功 finalize 后媒体记录应删除, 实际 %d", count)
	}
}

func TestCleanupRecycle_PreheldWritableHandleCannotMutateAfterFinalHash(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	source := filepath.Join(srcDir, "预持写句柄.mkv")
	original := []byte("最终哈希后的原始固定内容")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(source))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过预持写句柄竞态用例")
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now())
	recycleDir := filepath.Join(srcDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	tampered := bytes.Repeat([]byte("z"), len(original))

	var held *os.File
	var heldInfo os.FileInfo
	var openErr error
	svc.beforeRecycleFinalLock = func(path string) {
		held, openErr = os.OpenFile(path, os.O_RDWR, 0)
		if openErr == nil {
			heldInfo, openErr = held.Stat()
		}
	}
	var callbackCalled atomic.Bool
	var mutationErr error
	const callbackName = "test:mutate_preheld_handle_after_final_hash"
	if err := db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media_files" || !callbackCalled.CompareAndSwap(false, true) {
			return
		}
		if held == nil {
			mutationErr = errors.New("最终锁建立前未取得可写句柄")
			return
		}
		_, mutationErr = held.WriteAt(tampered, 0)
		if mutationErr == nil {
			mutationErr = os.Chtimes(target, heldInfo.ModTime(), heldInfo.ModTime())
		}
	}); err != nil {
		t.Fatalf("注册预持写句柄篡改失败注入失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Delete().Remove(callbackName) })
	t.Cleanup(func() {
		if held != nil {
			_ = held.Close()
		}
	})

	result, err := svc.CleanupRecycle(map[string]string{drive: recycleDir})
	if openErr != nil {
		t.Fatalf("最终锁建立前预持可写句柄失败: %v", openErr)
	}
	if err != nil {
		t.Fatalf("强制文件锁保护下清理应成功: %v", err)
	}
	if result.Moved != 1 || result.Failed != 0 {
		t.Fatalf("清理结果异常: %+v", result)
	}
	if !callbackCalled.Load() {
		t.Fatal("未在最终哈希后、数据库 CAS 前执行预持句柄篡改")
	}
	if mutationErr == nil {
		t.Fatal("最终文件锁必须阻止预持可写句柄对同一 inode 的原地篡改")
	}
	if got, readErr := os.ReadFile(target); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("预持句柄篡改被阻止后目标内容必须保持不变: content=%q err=%v", got, readErr)
	}
}

func TestCleanupRecycle_TargetReplacementBeforeFinalDeleteIsBlocked(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	source := filepath.Join(srcDir, "目标替换.mkv")
	original := []byte("原始媒体内容")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(source))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过最终目标锁定用例")
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now())
	recycleDir := filepath.Join(srcDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	foreign := []byte("外部替换内容")

	var replacementErr error
	const callbackName = "test:replace_target_before_final_delete"
	if err := db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media_files" || replacementErr != nil {
			return
		}
		if err := os.Remove(target); err != nil {
			replacementErr = err
			return
		}
		replacementErr = os.WriteFile(target, foreign, 0o644)
	}); err != nil {
		t.Fatalf("注册目标替换回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Delete().Remove(callbackName) })

	result, err := svc.CleanupRecycle(map[string]string{drive: recycleDir})
	if err != nil {
		t.Fatalf("最终提交前目标锁定后清理应成功: %v", err)
	}
	if result.Moved != 1 || result.Failed != 0 {
		t.Fatalf("清理结果异常: %+v", result)
	}
	if replacementErr == nil {
		t.Fatal("最终数据库删除期间目标路径替换必须被稳定文件引用阻止")
	}
	if got, readErr := os.ReadFile(target); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("目标替换被阻止后必须保留原始内容: content=%q err=%v", got, readErr)
	}
}

func TestCleanupRecycle_DeleteFailureAfterTargetReplacementRestoresOriginal(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	source := filepath.Join(srcDir, "替换后补偿.mkv")
	original := []byte("必须可恢复的原始媒体")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(source))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过稳定引用补偿用例")
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now())
	recycleDir := filepath.Join(srcDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	foreign := bytes.Repeat([]byte("x"), len(original))
	injectedErr := errors.New("注入最终数据库删除失败")

	var replacementErr error
	const callbackName = "test:replace_target_and_fail_final_delete"
	if err := db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media_files" {
			return
		}
		if err := os.Remove(target); err != nil {
			replacementErr = err
		} else {
			replacementErr = os.WriteFile(target, foreign, 0o644)
		}
		injectGORMCallbackError(t, tx, injectedErr)
	}); err != nil {
		t.Fatalf("注册目标替换与删除失败回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Delete().Remove(callbackName) })

	result, err := svc.CleanupRecycle(map[string]string{drive: recycleDir})
	if err != nil {
		t.Fatalf("数据库删除失败但原始文件补偿成功时不应返回整体错误: %v", err)
	}
	if result.Moved != 0 || result.Failed != 1 {
		t.Fatalf("补偿结果异常: %+v", result)
	}
	if replacementErr == nil {
		t.Fatal("删除 source 后到最终 CAS 期间必须锁定原始 inode 的可恢复引用")
	}
	if got, readErr := os.ReadFile(source); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("最终删除失败后必须从原始 inode 恢复 source: content=%q err=%v", got, readErr)
	}
	var stored models.MediaFile
	if err := db.Where("id = ?", media.ID).First(&stored).Error; err != nil {
		t.Fatalf("最终删除失败后数据库记录必须保留: %v", err)
	}
}

func TestCleanupRecycle_InPlaceMutationBeforeFinalDeleteIsBlocked(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	source := filepath.Join(srcDir, "原地篡改.mkv")
	original := []byte("原始内容-长度固定")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(source))
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过最终内容锁定用例")
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now())
	recycleDir := filepath.Join(srcDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	tampered := bytes.Repeat([]byte("z"), len(original))

	var mutationErr error
	const callbackName = "test:mutate_target_before_final_delete"
	if err := db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media_files" || mutationErr != nil {
			return
		}
		info, err := os.Stat(target)
		if err != nil {
			mutationErr = err
			return
		}
		file, err := os.OpenFile(target, os.O_WRONLY, 0)
		if err != nil {
			mutationErr = err
			return
		}
		_, mutationErr = file.WriteAt(tampered, 0)
		if closeErr := file.Close(); mutationErr == nil {
			mutationErr = closeErr
		}
		if mutationErr == nil {
			mutationErr = os.Chtimes(target, info.ModTime(), info.ModTime())
		}
	}); err != nil {
		t.Fatalf("注册同 inode 篡改回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Delete().Remove(callbackName) })

	result, err := svc.CleanupRecycle(map[string]string{drive: recycleDir})
	if err != nil {
		t.Fatalf("最终内容锁定后清理应成功: %v", err)
	}
	if result.Moved != 1 || result.Failed != 0 {
		t.Fatalf("清理结果异常: %+v", result)
	}
	if mutationErr == nil {
		t.Fatal("最终内容哈希到数据库 CAS 之间必须阻止同 inode 原地写入")
	}
	if got, readErr := os.ReadFile(target); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("同 inode 篡改被阻止后必须保留原始内容: content=%q err=%v", got, readErr)
	}
}

// TestCleanupRecycle_EmptyRecycle 回收站为空时返回零结果且不报错。
func TestCleanupRecycle_EmptyRecycle(t *testing.T) {
	svc, _ := newTestService(t)
	result, err := svc.CleanupRecycle(map[string]string{"D": "D:/.recycle"})
	if err != nil {
		t.Fatalf("空回收站不应报错, 实际: %v", err)
	}
	if result.Moved != 0 || result.Failed != 0 {
		t.Fatalf("空回收站期望零结果, 实际 %+v", result)
	}
}

// TestCleanupRecycle_SMBRejected SMB 软删项无盘符，整体拒绝清理。
func TestCleanupRecycle_SMBRejected(t *testing.T) {
	svc, _ := newTestService(t)
	createSoftDeletedMedia(t, svc, 1, "smb://host/share/远程.mp4", time.Now())

	_, err := svc.CleanupRecycle(map[string]string{"D": "D:/.recycle"})
	if !errors.Is(err, ErrRecycleBinPathUnset) {
		t.Fatalf("SMB 项无盘符应拒绝清理, 实际 %v", err)
	}
}

// TestDriveOfPath 盘符解析纯函数：Windows 盘符路径取盘符字母，SMB/无盘符返回空。
func TestDriveOfPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"D:/a/b.mp4", "D"},
		{"c:/x.png", "C"},
		{"smb://host/share/x.mp4", ""},
		{"/home/user/x.mp4", ""},
		{"relative/x.mp4", ""},
	}
	for _, c := range cases {
		if got := driveOfPath(c.path); got != c.want {
			t.Errorf("driveOfPath(%q)=%q, 期望 %q", c.path, got, c.want)
		}
	}
}

// TestUniqueDestPath 目标已存在同名文件时加序号避免覆盖。
func TestUniqueDestPath(t *testing.T) {
	dir := t.TempDir()
	name := "a.mp4"
	first := uniqueDestPath(dir, name)
	if filepath.Base(first) != name {
		t.Fatalf("首个目标应为原名, 实际 %s", filepath.Base(first))
	}
	// 占位首个目标后，应让出带序号的名字
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatalf("占位失败: %v", err)
	}
	second := uniqueDestPath(dir, name)
	if second == first || !strings.Contains(filepath.Base(second), "a") {
		t.Fatalf("第二个目标应换名避免覆盖, 实际 %s", filepath.Base(second))
	}
}

// TestListExpiredDeleted_SelectsOnlyExpired 仅选中 deleted_at 早于阈值的项（FR2-054）。
func TestListExpiredDeleted_SelectsOnlyExpired(t *testing.T) {
	svc, _ := newTestService(t)
	oldAt := time.Now().Add(-48 * time.Hour)
	newAt := time.Now().Add(-1 * time.Hour)
	old := createSoftDeletedMedia(t, svc, 1, "/tmp/old.mp4", oldAt)
	_ = createSoftDeletedMedia(t, svc, 1, "/tmp/new.mp4", newAt)
	before := time.Now().Add(-24 * time.Hour)
	items, err := svc.ListExpiredDeletedMediaInSpace(models.DefaultSpaceID, before, 50)
	if err != nil {
		t.Fatalf("查询过期失败: %v", err)
	}
	if len(items) != 1 || items[0].ID != old.ID {
		t.Fatalf("应仅含过期项 %d, 实际 %+v", old.ID, items)
	}
}

// TestAutoCleanupExpired_SkipsMissingDrive 缺盘符路径时跳过该项，不整轮拒绝，记录仍在回收站。
func TestAutoCleanupExpired_SkipsMissingDrive(t *testing.T) {
	svc, _ := newTestService(t)
	deletedAt := time.Now().Add(-48 * time.Hour)
	mf := createSoftDeletedMedia(t, svc, 1, "D:/media/gone.mkv", deletedAt)
	before := time.Now().Add(-24 * time.Hour)
	result, err := svc.AutoCleanupExpiredInSpace(models.DefaultSpaceID, map[string]string{}, before, 50)
	if err != nil {
		t.Fatalf("自动清理不应因缺路径整体失败: %v", err)
	}
	if result.Candidate != 1 || result.Skipped != 1 || result.Moved != 0 {
		t.Fatalf("期望 candidate=1 skipped=1 moved=0, 实际 %+v", result)
	}
	deleted, err := svc.ListDeletedMediaFilesInSpace(models.DefaultSpaceID)
	if err != nil || len(deleted) != 1 || deleted[0].ID != mf.ID {
		t.Fatalf("跳过后软删记录应仍在, deleted=%+v err=%v", deleted, err)
	}
}

// TestAutoCleanupExpired_MovesExpiredWithDrive 配置盘符后清理过期项。
func TestAutoCleanupExpired_MovesExpiredWithDrive(t *testing.T) {
	svc, _ := newTestService(t)
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "old.mkv")
	if err := os.WriteFile(srcFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	srcSlash := filepath.ToSlash(srcFile)
	drive := driveOfPath(srcSlash)
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过真实移动用例")
	}
	deletedAt := time.Now().Add(-48 * time.Hour)
	mf := createSoftDeletedMedia(t, svc, 1, srcSlash, deletedAt)
	// 未过期项不应被清
	fresh := filepath.Join(srcDir, "fresh.mkv")
	_ = os.WriteFile(fresh, []byte("fresh"), 0o644)
	_ = createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(fresh), time.Now().Add(-1*time.Hour))

	recycleDir := filepath.Join(srcDir, ".recycle")
	before := time.Now().Add(-24 * time.Hour)
	result, err := svc.AutoCleanupExpiredInSpace(models.DefaultSpaceID, map[string]string{drive: filepath.ToSlash(recycleDir)}, before, 50)
	if err != nil {
		t.Fatalf("自动清理失败: %v", err)
	}
	if result.Moved != 1 || result.Candidate != 1 {
		t.Fatalf("期望 candidate=1 moved=1, 实际 %+v", result)
	}
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatalf("过期源文件应已移走")
	}
	deleted, _ := svc.ListDeletedMediaFilesInSpace(models.DefaultSpaceID)
	if len(deleted) != 1 {
		t.Fatalf("未过期项应仍在回收站, 实际 %d", len(deleted))
	}
	_ = mf
}

// TestPreviewAutoCleanup_CountsMissingDrives preview 与候选一致且不改数据。
func TestPreviewAutoCleanup_CountsMissingDrives(t *testing.T) {
	svc, _ := newTestService(t)
	deletedAt := time.Now().Add(-72 * time.Hour)
	mf := createSoftDeletedMedia(t, svc, 1, "E:/x/a.mp4", deletedAt)
	before := time.Now().Add(-24 * time.Hour)
	preview, err := svc.PreviewAutoCleanupInSpace(models.DefaultSpaceID, map[string]string{}, before, 50)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Candidate != 1 || preview.Skipped != 1 || len(preview.MediaIDs) != 1 || preview.MediaIDs[0] != mf.ID {
		t.Fatalf("preview 不符: %+v", preview)
	}
	deleted, _ := svc.ListDeletedMediaFilesInSpace(models.DefaultSpaceID)
	if len(deleted) != 1 {
		t.Fatal("preview 不得删除记录")
	}
}

// TestPreviewAutoCleanup_TwoSpacesDifferentRetention 两 Space 不同保留天数阈值互不影响（FR2-054）。
func TestPreviewAutoCleanup_TwoSpacesDifferentRetention(t *testing.T) {
	svc, db := newTestService(t)
	const spaceA = "space-a"
	const spaceB = "space-b"
	// 各 Space 一条：软删 10 天前
	deletedAt := time.Now().Add(-10 * 24 * time.Hour)
	mfA, err := svc.CreateMediaFileInSpace(spaceA, 1, "D:/a/old.mp4", 100)
	if err != nil {
		t.Fatalf("创建 SpaceA 媒体失败: %v", err)
	}
	if err := db.Model(&models.MediaFile{}).Where("id = ?", mfA.ID).Update("deleted_at", deletedAt).Error; err != nil {
		t.Fatal(err)
	}
	mfB, err := svc.CreateMediaFileInSpace(spaceB, 1, "D:/b/old.mp4", 100)
	if err != nil {
		t.Fatalf("创建 SpaceB 媒体失败: %v", err)
	}
	if err := db.Model(&models.MediaFile{}).Where("id = ?", mfB.ID).Update("deleted_at", deletedAt).Error; err != nil {
		t.Fatal(err)
	}

	// Space A 保留 7 天 → before = now-7d，10 天前已过期
	beforeA := time.Now().Add(-7 * 24 * time.Hour)
	// Space B 保留 30 天 → before = now-30d，10 天前未过期
	beforeB := time.Now().Add(-30 * 24 * time.Hour)

	prevA, err := svc.PreviewAutoCleanupInSpace(spaceA, map[string]string{}, beforeA, 50)
	if err != nil {
		t.Fatal(err)
	}
	if prevA.Candidate != 1 || prevA.MediaIDs[0] != mfA.ID {
		t.Fatalf("SpaceA 短保留期应选中 1 项, 实际 %+v", prevA)
	}
	prevB, err := svc.PreviewAutoCleanupInSpace(spaceB, map[string]string{}, beforeB, 50)
	if err != nil {
		t.Fatal(err)
	}
	if prevB.Candidate != 0 {
		t.Fatalf("SpaceB 长保留期不应选中, 实际 %+v", prevB)
	}
	// 交叉：用 B 的阈值查 A 也不应选中（同 deleted_at）
	cross, err := svc.PreviewAutoCleanupInSpace(spaceA, map[string]string{}, beforeB, 50)
	if err != nil {
		t.Fatal(err)
	}
	if cross.Candidate != 0 {
		t.Fatalf("长阈值下 SpaceA 也不应过期, 实际 %+v", cross)
	}
}
