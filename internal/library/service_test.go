package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
)

// sanitizeDBName 将测试名转为合法的文件名。
func sanitizeDBName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "*", "_")
	name = strings.ReplaceAll(name, "?", "_")
	return name
}

// newTestService 创建带内存数据库的测试服务。
func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewService(gdb), gdb
}

func TestCreateLibraryPath(t *testing.T) {
	svc, _ := newTestService(t)

	lp, err := svc.CreateLibraryPath("/tmp/test_media", "local", "测试目录")
	if err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if lp.ID == 0 {
		t.Fatal("返回的 ID 为 0")
	}
	if lp.Path == "" {
		t.Fatal("路径不应为空")
	}
	if lp.Type != "local" {
		t.Fatalf("类型期望 local, 实际 %s", lp.Type)
	}
	if lp.Label != "测试目录" {
		t.Fatalf("标签期望 '测试目录', 实际 '%s'", lp.Label)
	}
	if lp.Enabled != 1 {
		t.Fatalf("期望启用状态 1, 实际 %d", lp.Enabled)
	}
}

func TestListLibraryPaths(t *testing.T) {
	svc, _ := newTestService(t)

	// 创建两条记录
	_, _ = svc.CreateLibraryPath("/tmp/dir1", "local", "目录1")
	_, _ = svc.CreateLibraryPath("/tmp/dir2", "local", "目录2")

	items, err := svc.ListLibraryPaths()
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 条记录, 实际 %d", len(items))
	}
}

func TestDeleteLibraryPath(t *testing.T) {
	svc, db := newTestService(t)

	lp, _ := svc.CreateLibraryPath("/tmp/to_delete", "local", "待删除")
	// 添加关联媒体文件
	_, _ = svc.CreateMediaFile(lp.ID, "/tmp/to_delete/video.mp4", 1024)

	if err := svc.DeleteLibraryPath(lp.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 目录应已删除
	_, err := svc.GetLibraryPathByID(lp.ID)
	if err == nil {
		t.Fatal("目录应已删除, 但查询仍返回结果")
	}

	// 关联媒体文件也应删除
	var count int64
	db.Model(&models.MediaFile{}).Where("library_id = ?", lp.ID).Count(&count)
	if count != 0 {
		t.Fatalf("关联媒体文件应已删除, 仍有 %d 条", count)
	}
}

func TestCreateMediaFile(t *testing.T) {
	svc, _ := newTestService(t)

	mf, err := svc.CreateMediaFile(1, "/tmp/test_video.mkv", 10737418240)
	if err != nil {
		t.Fatalf("创建媒体文件失败: %v", err)
	}
	if mf.ID == 0 {
		t.Fatal("返回的 ID 为 0")
	}
	if mf.FileName != "test_video.mkv" {
		t.Fatalf("文件名期望 test_video.mkv, 实际 %s", mf.FileName)
	}
	if mf.FileSize != 10737418240 {
		t.Fatalf("文件大小不匹配")
	}
	if mf.Format != "mkv" {
		t.Fatalf("格式期望 mkv, 实际 %s", mf.Format)
	}
}

func TestListMediaFiles_Pagination(t *testing.T) {
	svc, _ := newTestService(t)

	// 插入 5 条记录
	for i := 0; i < 5; i++ {
		_, _ = svc.CreateMediaFile(1, filepath.Join("/tmp", "video"+string(rune('a'+i))+".mp4"), int64(1024*(i+1)))
	}

	// 每页 2 条, 第 1 页
	items, total, err := svc.ListMediaFiles(1, "time_desc", "", 1, 2)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 5 {
		t.Fatalf("总数期望 5, 实际 %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("每页期望 2 条, 实际 %d", len(items))
	}

	// 第 2 页
	items, _, err = svc.ListMediaFiles(1, "time_desc", "", 2, 2)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("第2页期望 2 条, 实际 %d", len(items))
	}

	// 第 3 页 (只有 1 条)
	items, _, err = svc.ListMediaFiles(1, "time_desc", "", 3, 2)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("第3页期望 1 条, 实际 %d", len(items))
	}
}

func TestListMediaFiles_Search(t *testing.T) {
	svc, _ := newTestService(t)

	_, _ = svc.CreateMediaFile(1, "/tmp/电影_A.mp4", 1024)
	_, _ = svc.CreateMediaFile(1, "/tmp/电影_B.mp4", 2048)
	_, _ = svc.CreateMediaFile(1, "/tmp/music_C.mp4", 512)

	items, total, err := svc.ListMediaFiles(1, "time_desc", "电影", 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 2 {
		t.Fatalf("搜索'电影'期望 2 条, 实际 %d", total)
	}
	_ = items
}

func TestGetMediaFileByID(t *testing.T) {
	svc, _ := newTestService(t)

	mf, _ := svc.CreateMediaFile(1, "/tmp/detail_test.avi", 5120)

	result, err := svc.GetMediaFileByID(mf.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if result.FileName != "detail_test.avi" {
		t.Fatalf("文件名不匹配: %s", result.FileName)
	}
	if result.Format != "avi" {
		t.Fatalf("格式不匹配: %s", result.Format)
	}
}

func TestDeleteMediaFile(t *testing.T) {
	svc, _ := newTestService(t)

	mf, _ := svc.CreateMediaFile(1, "/tmp/to_delete.mp4", 1024)
	if err := svc.DeleteMediaFile(mf.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	_, err := svc.GetMediaFileByID(mf.ID)
	if err == nil {
		t.Fatal("媒体文件应已删除, 但查询仍返回结果")
	}
}

func TestScanLibrary(t *testing.T) {
	svc, _ := newTestService(t)

	// 创建临时目录和文件
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "movie1.mp4"), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "movie2.mkv"), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not video"), 0o644)

	count, err := svc.ScanLibrary(1, dir)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("期望扫描到 2 个视频文件, 实际 %d", count)
	}

	// 验证数据库
	items, total, err := svc.ListMediaFiles(1, "time_desc", "", 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 2 {
		t.Fatalf("期望 2 条记录, 实际 %d", total)
	}
	_ = items

	// 重复扫描不应重复入库
	count2, err := svc.ScanLibrary(1, dir)
	if err != nil {
		t.Fatalf("重复扫描失败: %v", err)
	}
	if count2 != 0 {
		t.Fatalf("重复扫描期望 0, 实际 %d", count2)
	}
}
