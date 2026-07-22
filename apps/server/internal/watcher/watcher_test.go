package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

// newTestWatcher 创建带内存数据库的测试监听器。
func newTestWatcher(t *testing.T) (*Watcher, *library.Service, *gorm.DB, func()) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层数据库失败: %v", err)
	}
	// SQLite 内存库按连接隔离，watcher 异步写入必须复用同一连接
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}, &models.ScanTask{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	svc := library.NewService(gdb)
	w, err := New(svc)
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}

	cleanup := func() {
		w.Stop()
		_ = sqlDB.Close()
	}

	return w, svc, gdb, cleanup
}

// createTestLibrary 创建测试用的临时目录并注册为媒体库。
func createTestLibrary(t *testing.T, svc *library.Service) (int64, string) {
	t.Helper()
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "测试目录")
	if err != nil {
		t.Fatalf("创建媒体库目录失败: %v", err)
	}
	return lp.ID, dir
}

// waitForCondition 等待条件满足（最多 10 秒）。
func waitForCondition(t *testing.T, name string, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	attempts := 0
	for time.Now().Before(deadline) {
		attempts++
		if fn() {
			t.Logf("[waitForCondition] %s: 第 %d 次尝试后满足", name, attempts)
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("[waitForCondition] %s: 超时，共尝试 %d 次", name, attempts)
	return false
}

func TestWatcher_StartIncludesNonDefaultSpaceLibraries(t *testing.T) {
	w, svc, _, cleanup := newTestWatcher(t)
	defer cleanup()
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPathInSpace("space-other", dir, "local", "其他 Space")
	if err != nil {
		t.Fatalf("创建非默认 Space 媒体库失败: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}
	videoPath := filepath.Join(dir, "other.mp4")
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("写入媒体失败: %v", err)
	}
	if !waitForCondition(t, "非默认 Space 媒体入库", func() bool {
		mf, getErr := svc.GetMediaFileByPathInSpace("space-other", videoPath)
		return getErr == nil && mf.LibraryID == lp.ID && mf.SpaceID == "space-other"
	}) {
		t.Fatal("watcher 未监听非默认 Space 媒体库")
	}
}

func TestWatcher_FansOutSamePhysicalPathAcrossSpaces(t *testing.T) {
	w, svc, gdb, cleanup := newTestWatcher(t)
	defer cleanup()
	dir := t.TempDir()
	libA, err := svc.CreateLibraryPathInSpace("space-a", dir, "local", "Space A")
	if err != nil {
		t.Fatalf("创建 Space A 媒体库失败: %v", err)
	}
	libB, err := svc.CreateLibraryPathInSpace("space-b", dir, "local", "Space B")
	if err != nil {
		t.Fatalf("创建 Space B 媒体库失败: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}

	videoPath := filepath.Join(dir, "shared.mp4")
	if err := os.WriteFile(videoPath, []byte("v1"), 0o644); err != nil {
		t.Fatalf("写入共享视频失败: %v", err)
	}
	w.handleEvent(fsnotify.Event{Name: videoPath, Op: fsnotify.Create})
	if !waitForCondition(t, "同路径新增事件 fan-out", func() bool {
		mediaA, errA := svc.GetMediaFileByPathInSpace("space-a", videoPath)
		mediaB, errB := svc.GetMediaFileByPathInSpace("space-b", videoPath)
		return errA == nil && errB == nil && mediaA.LibraryID == libA.ID && mediaB.LibraryID == libB.ID
	}) {
		t.Fatal("同一物理路径的新增事件未 fan-out 到全部 Space 绑定")
	}

	if err := os.WriteFile(videoPath, []byte("version-two"), 0o644); err != nil {
		t.Fatalf("修改共享视频失败: %v", err)
	}
	w.handleEvent(fsnotify.Event{Name: videoPath, Op: fsnotify.Write})
	if !waitForCondition(t, "同路径修改事件 fan-out", func() bool {
		mediaA, errA := svc.GetMediaFileByPathInSpace("space-a", videoPath)
		mediaB, errB := svc.GetMediaFileByPathInSpace("space-b", videoPath)
		return errA == nil && errB == nil && mediaA.FileSize == int64(len("version-two")) && mediaB.FileSize == int64(len("version-two"))
	}) {
		t.Fatal("同一物理路径的修改事件未 fan-out 到全部 Space 绑定")
	}

	if err := os.Remove(videoPath); err != nil {
		t.Fatalf("删除共享视频失败: %v", err)
	}
	w.handleEvent(fsnotify.Event{Name: videoPath, Op: fsnotify.Remove})
	if !waitForCondition(t, "同路径删除事件 fan-out", func() bool {
		var count int64
		err := gdb.Model(&models.MediaFile{}).
			Where("file_path = ? AND file_state = ? AND space_id IN ?", filepath.ToSlash(videoPath), models.MediaFileStateMissing, []string{"space-a", "space-b"}).
			Count(&count).Error
		return err == nil && count == 2
	}) {
		t.Fatal("同一物理路径的删除事件未 fan-out 到全部 Space 绑定")
	}
}

func TestWatcher_CreatesMediaFile(t *testing.T) {
	w, svc, gdb, cleanup := newTestWatcher(t)
	defer cleanup()

	libID, dir := createTestLibrary(t, svc)

	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}

	// 创建视频文件
	videoPath := filepath.Join(dir, "test_video.mp4")
	if err := os.WriteFile(videoPath, []byte("fake video data"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	// 等待入库
	if !waitForCondition(t, "视频文件入库", func() bool {
		mf, err := svc.GetMediaFileByPath(videoPath)
		return err == nil && mf != nil && mf.FileName == "test_video.mp4"
	}) {
		t.Fatal("等待视频文件入库超时")
	}

	// 验证数据库记录
	mf, err := svc.GetMediaFileByPath(videoPath)
	if err != nil {
		t.Fatalf("查询媒体文件失败: %v", err)
	}
	if mf.LibraryID != libID {
		t.Fatalf("library_id 期望 %d, 实际 %d", libID, mf.LibraryID)
	}
	if mf.Format != "mp4" {
		t.Fatalf("format 期望 mp4, 实际 %s", mf.Format)
	}
	_ = gdb
}

func TestWatcher_CreatesImageFile(t *testing.T) {
	w, svc, _, cleanup := newTestWatcher(t)
	defer cleanup()

	libID, dir := createTestLibrary(t, svc)

	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}

	imagePath := filepath.Join(dir, "cover.jpg")
	if err := os.WriteFile(imagePath, []byte("fake image data"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	if !waitForCondition(t, "图片文件入库", func() bool {
		mf, err := svc.GetMediaFileByPath(imagePath)
		return err == nil && mf != nil && mf.FileName == "cover.jpg"
	}) {
		t.Fatal("等待图片文件入库超时")
	}

	mf, err := svc.GetMediaFileByPath(imagePath)
	if err != nil {
		t.Fatalf("查询媒体文件失败: %v", err)
	}
	if mf.LibraryID != libID {
		t.Fatalf("library_id 期望 %d, 实际 %d", libID, mf.LibraryID)
	}
	if mf.Format != "jpg" {
		t.Fatalf("format 期望 jpg, 实际 %s", mf.Format)
	}
}

func TestWatcher_CreatesCustomExtensionFile(t *testing.T) {
	w, svc, _, cleanup := newTestWatcher(t)
	defer cleanup()

	libID, dir := createTestLibrary(t, svc)
	if err := svc.AddMediaExtension(libID, ".foo", library.MediaTypeImage); err != nil {
		t.Fatalf("添加自定义后缀失败: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}

	customPath := filepath.Join(dir, "cover.foo")
	if err := os.WriteFile(customPath, []byte("fake image data"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	if !waitForCondition(t, "自定义后缀文件入库", func() bool {
		mf, err := svc.GetMediaFileByPath(customPath)
		return err == nil && mf != nil && mf.FileName == "cover.foo"
	}) {
		t.Fatal("等待自定义后缀文件入库超时")
	}
}

func TestWatcher_IgnoresNonMediaFiles(t *testing.T) {
	w, svc, _, cleanup := newTestWatcher(t)
	defer cleanup()

	_, dir := createTestLibrary(t, svc)

	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}

	// 创建非媒体文件
	txtPath := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(txtPath, []byte("not a video"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	// 等待 1 秒确认不会入库
	time.Sleep(1 * time.Second)

	_, err := svc.GetMediaFileByPath(txtPath)
	if err == nil {
		t.Fatal("非媒体文件不应被入库")
	}
}

func TestWatcher_RemovesMediaFile(t *testing.T) {
	w, svc, gdb, cleanup := newTestWatcher(t)
	defer cleanup()

	_, dir := createTestLibrary(t, svc)

	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}

	// 创建视频文件
	videoPath := filepath.Join(dir, "to_delete.mp4")
	if err := os.WriteFile(videoPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	// 等待入库
	if !waitForCondition(t, "视频文件入库", func() bool {
		mf, err := svc.GetMediaFileByPath(videoPath)
		return err == nil && mf != nil
	}) {
		t.Fatal("等待视频文件入库超时")
	}

	// 删除文件
	if err := os.Remove(videoPath); err != nil {
		t.Fatalf("删除文件失败: %v", err)
	}

	// 等待常规查询隐藏缺失记录
	if !waitForCondition(t, "媒体文件记录隐藏", func() bool {
		_, err := svc.GetMediaFileByPath(videoPath)
		return err != nil
	}) {
		t.Fatal("等待媒体文件记录隐藏超时")
	}

	var mf models.MediaFile
	if err := gdb.Where("file_path = ?", filepath.ToSlash(videoPath)).First(&mf).Error; err != nil {
		t.Fatalf("查询缺失记录失败: %v", err)
	}
	if mf.FileState != models.MediaFileStateMissing {
		t.Fatalf("删除事件应标记 missing，实际 %q", mf.FileState)
	}
	if mf.DeletedAt != nil {
		t.Fatal("删除事件不应把记录放入回收站")
	}
}

func TestWatcher_EnqueuesChangeWhenQueueInjected(t *testing.T) {
	w, svc, gdb, cleanup := newTestWatcher(t)
	defer cleanup()

	libID, dir := createTestLibrary(t, svc)
	var enqueued int32
	q := library.NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) {
		t.Fatal("watcher 事件不应触发整库扫描")
		return 0, nil
	}).WithChangeExec(func(change library.ScanChange) (int, error) {
		if change.SpaceID != models.DefaultSpaceID || change.LibraryID != libID || change.Op != library.ScanChangeModified {
			t.Fatalf("变更参数不正确: %+v", change)
		}
		atomic.AddInt32(&enqueued, 1)
		return 1, nil
	})
	q.Start()
	defer q.Stop()
	w.WithScanQueue(q)

	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}
	videoPath := filepath.Join(dir, "queued.mp4")
	if err := os.WriteFile(videoPath, []byte("fake video data"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	if !waitForCondition(t, "watcher 变更入队", func() bool {
		return atomic.LoadInt32(&enqueued) == 1
	}) {
		t.Fatal("等待 watcher 变更入队超时")
	}
}

func TestWatcher_EnqueuesRemoveWhenQueueInjected(t *testing.T) {
	w, svc, gdb, cleanup := newTestWatcher(t)
	defer cleanup()

	libID, dir := createTestLibrary(t, svc)
	q := library.NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) {
		t.Fatal("删除事件不应触发整库扫描")
		return 0, nil
	})
	w.WithScanQueue(q)
	w.addBinding(filepath.ToSlash(dir), pathBinding{libraryID: libID, spaceID: models.DefaultSpaceID})

	videoPath := filepath.Join(dir, "removed.mp4")
	w.removeRecord(videoPath)

	var task models.ScanTask
	if err := gdb.First(&task).Error; err != nil {
		t.Fatalf("查询扫描任务失败: %v", err)
	}
	if task.ScanType != models.ScanTypeIncremental {
		t.Fatalf("删除事件应入队为增量任务，实际 %s", task.ScanType)
	}
	if !strings.Contains(task.PayloadJSON, string(library.ScanChangeRemoved)) || !strings.Contains(task.PayloadJSON, filepath.ToSlash(videoPath)) {
		t.Fatalf("删除事件 payload 不正确: %s", task.PayloadJSON)
	}
}

func TestWatcher_PollSMBEnqueuesIncrementalScanWhenQueueInjected(t *testing.T) {
	w, _, gdb, cleanup := newTestWatcher(t)
	defer cleanup()

	q := library.NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) {
		t.Fatal("SMB 轮询不应绕过队列直接扫描")
		return 0, nil
	})
	w.WithScanQueue(q)
	w.smbLibs = []models.LibraryPath{
		{ID: 8, SpaceID: models.DefaultSpaceID, Path: "smb://server/share", Type: "smb"},
	}

	w.pollAllSMB()

	var task models.ScanTask
	if err := gdb.First(&task).Error; err != nil {
		t.Fatalf("查询扫描任务失败: %v", err)
	}
	if task.LibraryID != 8 || task.ScanType != models.ScanTypeIncremental {
		t.Fatalf("SMB 轮询任务不正确: %+v", task)
	}
	if !strings.Contains(task.PayloadJSON, "smb://server/share") {
		t.Fatalf("SMB 轮询 payload 不正确: %s", task.PayloadJSON)
	}
}

func TestWatcher_Debounce(t *testing.T) {
	w, svc, gdb, cleanup := newTestWatcher(t)
	defer cleanup()

	_, dir := createTestLibrary(t, svc)

	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}

	videoPath := filepath.Join(dir, "debounce_test.mp4")

	// 快速多次写入同一文件
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(videoPath, []byte("data"), 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 等待去抖完成 + 入库
	time.Sleep(1 * time.Second)

	// 验证只有一条记录（统一为正斜杠查询）
	var count int64
	normalizedPath := filepath.ToSlash(videoPath)
	gdb.Model(&models.MediaFile{}).Where("file_path = ?", normalizedPath).Count(&count)
	if gdb.Error != nil && !errors.Is(gdb.Error, gorm.ErrRecordNotFound) {
		t.Fatalf("查询记录数失败: %v", gdb.Error)
	}
	if count != 1 {
		t.Fatalf("去抖后期望 1 条记录, 实际 %d", count)
	}
	if count != 1 {
		t.Fatalf("去抖后期望 1 条记录, 实际 %d", count)
	}
}
