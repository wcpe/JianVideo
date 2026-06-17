package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
)

// newTestWatcher 创建带内存数据库的测试监听器。
func newTestWatcher(t *testing.T) (*Watcher, *library.Service, *gorm.DB, func()) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	svc := library.NewService(gdb)
	w, err := New(svc)
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}

	cleanup := func() {
		w.Stop()
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

// waitForCondition 等待条件满足（最多 3 秒）。
func waitForCondition(t *testing.T, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
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
	if !waitForCondition(t, func() bool {
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

func TestWatcher_IgnoresNonVideoFiles(t *testing.T) {
	w, svc, _, cleanup := newTestWatcher(t)
	defer cleanup()

	_, dir := createTestLibrary(t, svc)

	if err := w.Start(); err != nil {
		t.Fatalf("启动监听器失败: %v", err)
	}

	// 创建非视频文件
	txtPath := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(txtPath, []byte("not a video"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	// 等待 1 秒确认不会入库
	time.Sleep(1 * time.Second)

	_, err := svc.GetMediaFileByPath(txtPath)
	if err == nil {
		t.Fatal("非视频文件不应被入库")
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
	if !waitForCondition(t, func() bool {
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
	if !waitForCondition(t, func() bool {
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

	// 验证只有一条记录
	var count int64
	if err := gdb.Model(&models.MediaFile{}).Where("file_path = ?", videoPath).Count(&count).Error; err != nil {
		t.Fatalf("查询记录数失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("去抖后期望 1 条记录, 实际 %d", count)
	}
}

func TestIsVideoFile(t *testing.T) {
	tests := []struct {
		path   string
		expect bool
	}{
		{"movie.mp4", true},
		{"movie.MKV", true},
		{"movie.AvI", true},
		{"movie.webm", true},
		{"movie.rmvb", true},
		{"movie.txt", false},
		{"movie.jpg", false},
		{"movie", false},
		{"movie.doc", false},
	}
	for _, tt := range tests {
		if got := isVideoFile(tt.path); got != tt.expect {
			t.Errorf("isVideoFile(%q) = %v, 期望 %v", tt.path, got, tt.expect)
		}
	}
}
