//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package library

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestLockRecycleFile_RejectsCompetingAdvisoryLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock-target.mkv")
	if err := os.WriteFile(path, []byte("lock"), 0o644); err != nil {
		t.Fatalf("写锁测试文件失败: %v", err)
	}
	first, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开首个锁句柄失败: %v", err)
	}
	defer func() { _ = first.Close() }()
	second, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开竞争锁句柄失败: %v", err)
	}
	defer func() { _ = second.Close() }()

	if err := lockRecycleFile(first); err != nil {
		t.Fatalf("取得首个 advisory 锁失败: %v", err)
	}
	if err := lockRecycleFile(second); err == nil {
		t.Fatal("竞争句柄不得同时取得 advisory 锁")
	}
	if err := unlockRecycleFile(first); err != nil {
		t.Fatalf("释放首个 advisory 锁失败: %v", err)
	}
	if err := lockRecycleFile(second); err != nil {
		t.Fatalf("首个锁释放后竞争句柄应可取得锁: %v", err)
	}
	if err := unlockRecycleFile(second); err != nil {
		t.Fatalf("释放竞争 advisory 锁失败: %v", err)
	}
}

func TestCleanupRecycle_PostCASWriteAtRestoresSourceAndDatabase(t *testing.T) {
	svc, db := newTestService(t)
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "unix-post-cas-writeat.mkv")
	original := []byte("Unix advisory 锁下必须可恢复的原始字节")
	tampered := bytes.Repeat([]byte("x"), len(original))
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now().UTC())
	recycleDir := filepath.Join(sourceDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	var held *os.File
	var mutationErr error
	svc.afterRecycleDelete = func() {
		held, mutationErr = os.OpenFile(target, os.O_RDWR, 0)
		if mutationErr != nil {
			return
		}
		heldInfo, err := held.Stat()
		if err != nil {
			mutationErr = err
			return
		}
		targetInfo, err := os.Lstat(target)
		if err != nil {
			mutationErr = err
			return
		}
		if !os.SameFile(heldInfo, targetInfo) {
			mutationErr = errors.New("post-CAS 篡改句柄未指向最终回收目标")
			return
		}
		if _, mutationErr = held.WriteAt(tampered, 0); mutationErr == nil {
			mutationErr = held.Sync()
		}
		if mutationErr == nil {
			mutationErr = os.Chtimes(target, targetInfo.ModTime(), targetInfo.ModTime())
		}
	}
	t.Cleanup(func() {
		if held != nil {
			_ = held.Close()
		}
	})

	completed, err := svc.cleanupRecycleItem(media.SpaceID, map[string]string{"": recycleDir}, *media)
	if mutationErr != nil {
		t.Fatalf("Unix post-CAS WriteAt 注入失败: %v", mutationErr)
	}
	var recoveryErr *RecycleRecoveryError
	if completed || !errors.As(err, &recoveryErr) {
		t.Fatalf("advisory 锁下 post-CAS 篡改必须进入补偿: completed=%v err=%v", completed, err)
	}
	if got, readErr := os.ReadFile(source); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("post-CAS 篡改后必须从 held recovery FD 恢复源字节: content=%q err=%v", got, readErr)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || !bytes.Equal(got, tampered) {
		t.Fatalf("补偿不得信任或覆盖已篡改 target: content=%q err=%v", got, readErr)
	}
	if got, readErr := os.ReadFile(target + recycleRecoverySuffix); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("补偿后必须保留原始 recovery: content=%q err=%v", got, readErr)
	}
	var stored models.MediaFile
	if err := db.First(&stored, media.ID).Error; err != nil {
		t.Fatalf("post-CAS 篡改后数据库前像必须恢复: %v", err)
	}
	if stored.FileState != media.FileState || stored.DeletedAt == nil || !stored.DeletedAt.Equal(*media.DeletedAt) {
		t.Fatalf("post-CAS 篡改后不得遗留 cleanup claim: %+v", stored)
	}
}

func TestCleanupRecycle_PostCASReplaceRestoresWithoutTrustingTarget(t *testing.T) {
	svc, db := newTestService(t)
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "unix-post-cas-replace.mkv")
	original := []byte("Unix unlink replace 后必须恢复的原始字节")
	foreign := []byte("替换后的外部文件")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), time.Now().UTC())
	recycleDir := filepath.Join(sourceDir, ".recycle")
	target := expectedRecycleTarget(recycleDir, media)
	var replacementErr error
	svc.afterRecycleDelete = func() {
		if replacementErr = os.Remove(target); replacementErr == nil {
			replacementErr = os.WriteFile(target, foreign, 0o644)
		}
	}

	completed, err := svc.cleanupRecycleItem(media.SpaceID, map[string]string{"": recycleDir}, *media)
	if replacementErr != nil {
		t.Fatalf("Unix post-CAS unlink/replace 注入失败: %v", replacementErr)
	}
	var recoveryErr *RecycleRecoveryError
	if completed || !errors.As(err, &recoveryErr) {
		t.Fatalf("post-CAS unlink/replace 必须进入补偿: completed=%v err=%v", completed, err)
	}
	if got, readErr := os.ReadFile(source); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("post-CAS replace 后源路径必须恢复原始字节: content=%q err=%v", got, readErr)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || !bytes.Equal(got, foreign) {
		t.Fatalf("补偿不得信任或覆盖替换后的 target: content=%q err=%v", got, readErr)
	}
	if got, readErr := os.ReadFile(target + recycleRecoverySuffix); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("post-CAS replace 后必须保留原始 recovery: content=%q err=%v", got, readErr)
	}
	var stored models.MediaFile
	if err := db.First(&stored, media.ID).Error; err != nil {
		t.Fatalf("post-CAS replace 后数据库前像必须恢复: %v", err)
	}
	if stored.FileState != media.FileState || stored.DeletedAt == nil || !stored.DeletedAt.Equal(*media.DeletedAt) {
		t.Fatalf("post-CAS replace 后不得遗留 cleanup claim: %+v", stored)
	}
}
