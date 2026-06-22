package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}); err != nil {
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
	w, svc, _, cleanup := newTestWatcher(t)
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

	// 等待删除记录
	if !waitForCondition(t, "媒体文件记录删除", func() bool {
		_, err := svc.GetMediaFileByPath(videoPath)
		return err != nil
	}) {
		t.Fatal("等待媒体文件记录删除超时")
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
