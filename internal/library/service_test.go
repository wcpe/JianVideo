package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
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
	// 内存库每条连接是独立数据库，并发写会落到不同连接而互相不可见；
	// 限单连接使内存库表现为共享，并与生产 WAL「写串行」语义一致（同 watcher 测试做法）。
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewService(gdb), gdb
}

func TestCreateLibraryPath(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()

	lp, err := svc.CreateLibraryPath(dir, "local", "测试目录")
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
	_, _ = svc.CreateLibraryPath(t.TempDir(), "local", "目录1")
	_, _ = svc.CreateLibraryPath(t.TempDir(), "local", "目录2")

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
	dir := t.TempDir()

	lp, _ := svc.CreateLibraryPath(dir, "local", "待删除")
	// 添加关联媒体文件和自定义后缀
	_, _ = svc.CreateMediaFile(lp.ID, filepath.Join(dir, "video.mp4"), 1024)
	_ = svc.AddMediaExtension(lp.ID, ".foo", "video")

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

	db.Model(&models.MediaExtension{}).Where("library_id = ?", lp.ID).Count(&count)
	if count != 0 {
		t.Fatalf("关联自定义后缀应已删除, 仍有 %d 条", count)
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
	svc, gdb := newTestService(t)

	mf, _ := svc.CreateMediaFile(1, "/tmp/to_delete.mp4", 1024)
	if err := svc.DeleteMediaFile(mf.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 软删除：数据库记录仍在，仅 deleted_at 被置上（源文件不动，由不调用文件删除保证）
	var got models.MediaFile
	if err := gdb.First(&got, mf.ID).Error; err != nil {
		t.Fatalf("软删除后记录应仍存在: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatal("软删除后 deleted_at 应被置上, 实际为空")
	}
}

func TestDeleteMediaFile_ExcludedFromList(t *testing.T) {
	svc, _ := newTestService(t)

	keep, _ := svc.CreateMediaFile(1, "/tmp/keep.mp4", 1024)
	del, _ := svc.CreateMediaFile(1, "/tmp/gone.mp4", 1024)
	if err := svc.DeleteMediaFile(del.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}

	items, total, err := svc.ListMediaFilesFiltered(MediaFilter{LibraryID: 1}, 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != keep.ID {
		t.Fatalf("常规列表应仅含未软删项, 实得 total=%d items=%d", total, len(items))
	}
}

func TestListDeletedMediaFiles(t *testing.T) {
	svc, _ := newTestService(t)

	keep, _ := svc.CreateMediaFile(1, "/tmp/keep.mp4", 1024)
	del, _ := svc.CreateMediaFile(1, "/tmp/gone.mp4", 1024)
	if err := svc.DeleteMediaFile(del.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}

	deleted, err := svc.ListDeletedMediaFiles()
	if err != nil {
		t.Fatalf("查询回收站失败: %v", err)
	}
	if len(deleted) != 1 || deleted[0].ID != del.ID {
		t.Fatalf("回收站应仅含软删项 %d, 实得 %d 条", del.ID, len(deleted))
	}
	// 未软删项不应出现在回收站
	for _, m := range deleted {
		if m.ID == keep.ID {
			t.Fatal("未软删项不应出现在回收站")
		}
	}
}

func TestRestoreMediaFile(t *testing.T) {
	svc, _ := newTestService(t)

	mf, _ := svc.CreateMediaFile(1, "/tmp/restore.mp4", 1024)
	if err := svc.DeleteMediaFile(mf.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	if err := svc.RestoreMediaFile(mf.ID); err != nil {
		t.Fatalf("还原失败: %v", err)
	}

	// 还原后回到常规列表、且不在回收站
	items, total, _ := svc.ListMediaFilesFiltered(MediaFilter{LibraryID: 1}, 1, 20)
	if total != 1 || len(items) != 1 || items[0].ID != mf.ID {
		t.Fatalf("还原后应回到常规列表, 实得 total=%d", total)
	}
	deleted, _ := svc.ListDeletedMediaFiles()
	if len(deleted) != 0 {
		t.Fatalf("还原后回收站应为空, 实得 %d 条", len(deleted))
	}
}

func TestRestoreMediaFile_NotFound(t *testing.T) {
	svc, _ := newTestService(t)

	err := svc.RestoreMediaFile(99999)
	if err == nil {
		t.Fatal("还原不存在的媒体应返回错误")
	}
}

func TestGetMediaFileByID_NotFound(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.GetMediaFileByID(99999)
	if err == nil {
		t.Fatal("不存在的媒体文件应返回错误")
	}
}

// TestGetMediaFileByID_ExcludesSoftDeleted 软删项不应经详情/播放路径取到（FR-25 访问隔离）。
func TestGetMediaFileByID_ExcludesSoftDeleted(t *testing.T) {
	svc, _ := newTestService(t)

	mf, _ := svc.CreateMediaFile(1, "/tmp/soft_deleted.mp4", 1024)
	if err := svc.DeleteMediaFile(mf.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}

	if _, err := svc.GetMediaFileByID(mf.ID); err == nil {
		t.Fatal("软删项应被 GetMediaFileByID 视为不存在, 实际仍可取到")
	}
}

// TestRestoreMediaFile_AfterTightenedGet 收紧 GetMediaFileByID 排除软删后，还原仍须正常。
func TestRestoreMediaFile_AfterTightenedGet(t *testing.T) {
	svc, _ := newTestService(t)

	mf, _ := svc.CreateMediaFile(1, "/tmp/restore_flow.mp4", 1024)
	if err := svc.DeleteMediaFile(mf.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	// 软删后详情不可达
	if _, err := svc.GetMediaFileByID(mf.ID); err == nil {
		t.Fatal("软删后详情应不可达")
	}
	// 还原后重新可达且回常规列表
	if err := svc.RestoreMediaFile(mf.ID); err != nil {
		t.Fatalf("还原失败: %v", err)
	}
	if _, err := svc.GetMediaFileByID(mf.ID); err != nil {
		t.Fatalf("还原后详情应可达: %v", err)
	}
}

func TestDeleteMediaFile_NotFound(t *testing.T) {
	svc, _ := newTestService(t)

	err := svc.DeleteMediaFile(99999)
	if err == nil {
		t.Fatal("删除不存在的媒体文件应返回错误")
	}
}

func TestCreateMediaFile_EmptyPath(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.CreateMediaFile(1, "", 1024)
	if err == nil {
		t.Fatal("空路径应返回错误")
	}
}

func TestCreateLibraryPath_EmptyPath(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.CreateLibraryPath("", "local", "")
	if err == nil {
		t.Fatal("空路径应返回错误")
	}
}

func TestDeleteLibraryPath_NotFound(t *testing.T) {
	svc, _ := newTestService(t)

	err := svc.DeleteLibraryPath(99999)
	if err == nil {
		t.Fatal("删除不存在的目录应返回错误")
	}
}

func TestCreateLibraryPath_LocalPathMustExistAndBeDirectory(t *testing.T) {
	svc, _ := newTestService(t)
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := svc.CreateLibraryPath(missing, "local", "不存在"); err == nil {
		t.Fatal("不存在的本地路径应返回错误")
	}

	file := filepath.Join(t.TempDir(), "file.txt")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	if _, err := svc.CreateLibraryPath(file, "local", "文件"); err == nil {
		t.Fatal("本地文件路径应返回错误")
	}
}

func TestCreateLibraryPath_NormalizesSMBPath(t *testing.T) {
	svc, _ := newTestService(t)
	lp, err := svc.CreateLibraryPath(`\\192.168.1.10\Share\Movies`, "smb", "NAS")
	if err != nil {
		t.Fatalf("创建 SMB 路径失败: %v", err)
	}
	if lp.Path != "192.168.1.10/Share/Movies" {
		t.Fatalf("SMB 路径期望规范化为 host/share/path，实际 %s", lp.Path)
	}
}

func TestListMediaFiles_EmptyResult(t *testing.T) {

	svc, _ := newTestService(t)

	items, total, err := svc.ListMediaFiles(1, "time_desc", "nonexistent", 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 0 {
		t.Fatalf("期望 total=0, 实际 %d", total)
	}
	if len(items) != 0 {
		t.Fatalf("期望空列表, 实际 %d 条", len(items))
	}
}

func TestScanLibrary(t *testing.T) {
	svc, _ := newTestService(t)

	// 创建临时目录和文件
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "movie1.mp4"), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "movie2.mkv"), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "poster.jpg"), []byte("fake"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "nested"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "nested", "clip.mov"), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "nested", "cover.png"), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not media"), 0o644)

	svc.ScanLibrary(1, dir)

	// 等待异步扫描完成
	var status ScanStatus
	for i := 0; i < 50; i++ {
		status = GetScanStatus()
		if status.Status == "completed" || status.Status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status.Status != "completed" {
		t.Fatalf("扫描未完成, 当前状态: %s, err=%s", status.Status, status.Error)
	}
	if status.ScannedFiles != 5 {
		t.Fatalf("期望递归扫描到 5 个媒体文件, 实际 %d", status.ScannedFiles)
	}

	// 验证数据库
	items, total, err := svc.ListMediaFiles(1, "time_desc", "", 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 5 {
		t.Fatalf("期望 5 条记录, 实际 %d", total)
	}
	_ = items

	// 重复扫描不应重复入库
	svc.ScanLibrary(1, dir)
	for i := 0; i < 50; i++ {
		status = GetScanStatus()
		if status.Status == "completed" || status.Status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status.ScannedFiles != 0 {
		t.Fatalf("重复扫描期望 0, 实际 %d", status.ScannedFiles)
	}
}

func TestScanLibrary_AllowsSamePathInDifferentLibraries(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "shared.mp4"), []byte("fake"), 0o644)

	// 第一次扫描
	svc.ScanLibrary(1, dir)
	var status ScanStatus
	for i := 0; i < 50; i++ {
		status = GetScanStatus()
		if status.Status == "completed" || status.Status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status.ScannedFiles != 1 {
		t.Fatalf("首次扫描期望 1, 实际 %d", status.ScannedFiles)
	}

	// 第二次扫描（不同媒体库）
	svc.ScanLibrary(2, dir)
	for i := 0; i < 50; i++ {
		status = GetScanStatus()
		if status.Status == "completed" || status.Status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status.ScannedFiles != 1 {
		t.Fatalf("不同媒体库扫描同一路径期望 1, 实际 %d", status.ScannedFiles)
	}
}

func TestDeleteMediaFileByLibraryAndPath_OnlyDeletesCurrentLibrary(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.mp4")
	if _, err := svc.CreateMediaFile(1, path, 1024); err != nil {
		t.Fatalf("创建媒体文件失败: %v", err)
	}
	if _, err := svc.CreateMediaFile(2, path, 1024); err != nil {
		t.Fatalf("创建第二个媒体库媒体文件失败: %v", err)
	}

	if err := svc.DeleteMediaFileByLibraryAndPath(1, path); err != nil {
		t.Fatalf("删除当前媒体库文件失败: %v", err)
	}
	if _, err := svc.GetMediaFileByLibraryAndPath(1, path); err == nil {
		t.Fatal("当前媒体库记录应已删除")
	}
	if _, err := svc.GetMediaFileByLibraryAndPath(2, path); err != nil {
		t.Fatalf("其他媒体库同路径记录不应被删除: %v", err)
	}
}

func TestBuiltInMediaExtensions(t *testing.T) {
	svc, _ := newTestService(t)

	for _, ext := range []string{"mp4", "mkv", "avi", "mov", "webm", "flv", "wmv", "ts", "m4v", "mpg", "mpeg", "3gp", "rmvb", "rm", "jpg", "jpeg", "png", "gif", "webp", "bmp", "tif", "tiff", "heic", "heif"} {
		if !svc.IsMediaFile("file." + ext) {
			t.Fatalf("内置后缀 %s 应被识别为媒体文件", ext)
		}
	}
	if svc.IsMediaFile("file.txt") {
		t.Fatal("txt 不应被识别为媒体文件")
	}
}

func TestCustomMediaExtensionsPersistAndScan(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "自定义后缀")
	if err != nil {
		t.Fatalf("创建媒体库目录失败: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "custom.foo"), []byte("fake"), 0o644)

	svc.ScanLibrary(lp.ID, dir)
	// 等待异步扫描完成
	for i := 0; i < 50; i++ {
		st := GetScanStatus()
		if st.Status == "completed" || st.Status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, total, err := svc.ListMediaFiles(lp.ID, "time_desc", "", 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 0 {
		t.Fatalf("添加自定义后缀前不应入库, 实际 %d", total)
	}

	if err := svc.AddMediaExtension(lp.ID, ".foo", "video"); err != nil {
		t.Fatalf("添加自定义后缀失败: %v", err)
	}
	if svc.IsMediaFile("custom.FOO") {
		t.Fatal("未绑定媒体库时自定义后缀不应被全局识别")
	}
	if !svc.IsMediaFileForLibrary(lp.ID, "custom.FOO") {
		t.Fatal("自定义后缀应按媒体库绑定且大小写不敏感")
	}

	items, err := svc.ListMediaExtensions(lp.ID)
	if err != nil {
		t.Fatalf("查询后缀失败: %v", err)
	}
	found := false
	for _, item := range items {
		if item.LibraryID == lp.ID && item.Extension == "foo" && item.Type == "video" && item.IsBuiltIn == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("自定义后缀应绑定媒体库持久化")
	}

	svc.ScanLibrary(lp.ID, dir)
	// 等待异步扫描完成
	var status ScanStatus
	for i := 0; i < 50; i++ {
		status = GetScanStatus()
		if status.Status == "completed" || status.Status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status.ScannedFiles != 1 {
		t.Fatalf("添加自定义后缀后期望扫描 1 个文件, 实际 %d", status.ScannedFiles)
	}
}

// TestDeleteMediaExtension 删除自定义后缀（FR-64）：只删自定义、内置不可删、删不存在报错。
func TestDeleteMediaExtension(t *testing.T) {
	svc, _ := newTestService(t)
	lp, _ := svc.CreateLibraryPath(t.TempDir(), "local", "后缀删除")

	if err := svc.AddMediaExtension(lp.ID, ".foo", "video"); err != nil {
		t.Fatalf("添加自定义后缀失败: %v", err)
	}

	// 删除自定义后缀成功，后续不再识别
	if err := svc.DeleteMediaExtension(lp.ID, ".foo"); err != nil {
		t.Fatalf("删除自定义后缀失败: %v", err)
	}
	if svc.IsMediaFileForLibrary(lp.ID, "x.foo") {
		t.Fatal("删除后自定义后缀不应再被识别")
	}
	items, _ := svc.ListMediaExtensions(lp.ID)
	for _, item := range items {
		if item.Extension == "foo" && item.IsBuiltIn == 0 {
			t.Fatal("删除后列表不应再含该自定义后缀")
		}
	}

	// 删除不存在的自定义后缀报错
	if err := svc.DeleteMediaExtension(lp.ID, ".nope"); err == nil {
		t.Fatal("删除不存在的自定义后缀应报错")
	}

	// 删除内置后缀被拒绝，且内置仍可识别
	if err := svc.DeleteMediaExtension(lp.ID, ".mp4"); err == nil {
		t.Fatal("删除内置后缀应被拒绝")
	}
	if !svc.IsMediaFileForLibrary(lp.ID, "x.mp4") {
		t.Fatal("内置后缀不应因尝试删除而失效")
	}
}

func TestRenameMediaFile(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.mp4")
	if err := os.WriteFile(oldPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	mf, err := svc.CreateMediaFile(1, oldPath, 4)
	if err != nil {
		t.Fatalf("创建媒体记录失败: %v", err)
	}

	renamed, err := svc.RenameMediaFile(mf.ID, "new.mkv")
	if err != nil {
		t.Fatalf("重命名失败: %v", err)
	}

	// 磁盘：旧文件不存在、新文件存在
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("旧磁盘文件应已不存在")
	}
	newPath := filepath.Join(dir, "new.mkv")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("新磁盘文件应存在: %v", err)
	}

	// 返回值与数据库均更新
	if renamed.FileName != "new.mkv" || renamed.Format != "mkv" {
		t.Fatalf("返回值未更新: file_name=%s format=%s", renamed.FileName, renamed.Format)
	}
	if renamed.FilePath != filepath.ToSlash(newPath) {
		t.Fatalf("返回值 file_path 未更新: %s", renamed.FilePath)
	}
	got, err := svc.GetMediaFileByID(mf.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.FileName != "new.mkv" || got.FilePath != filepath.ToSlash(newPath) {
		t.Fatalf("数据库记录未更新: %+v", got)
	}
}

func TestRenameMediaFile_TargetExists(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "a.mp4")
	occupied := filepath.Join(dir, "b.mp4")
	_ = os.WriteFile(oldPath, []byte("fake"), 0o644)
	_ = os.WriteFile(occupied, []byte("fake"), 0o644)
	mf, _ := svc.CreateMediaFile(1, oldPath, 4)

	if _, err := svc.RenameMediaFile(mf.ID, "b.mp4"); !errors.Is(err, ErrRenameTargetExists) {
		t.Fatalf("期望 ErrRenameTargetExists, 实际 %v", err)
	}
	// 失败时磁盘旧文件应保持不变
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatal("失败后旧文件不应被改名")
	}
}

func TestRenameMediaFile_InvalidName(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "a.mp4")
	_ = os.WriteFile(oldPath, []byte("fake"), 0o644)
	mf, _ := svc.CreateMediaFile(1, oldPath, 4)

	for _, bad := range []string{"", "  ", "..", "sub/x.mp4", "sub\\x.mp4"} {
		if _, err := svc.RenameMediaFile(mf.ID, bad); !errors.Is(err, ErrInvalidNewName) {
			t.Fatalf("名称 %q 期望 ErrInvalidNewName, 实际 %v", bad, err)
		}
	}
}

func TestRenameMediaFile_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.RenameMediaFile(9999, "x.mp4"); err == nil {
		t.Fatal("不存在的记录应返回错误")
	}
}

func TestUpdateDisplayName(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	diskPath := filepath.Join(dir, "real.mp4")
	if err := os.WriteFile(diskPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	mf, err := svc.CreateMediaFile(1, diskPath, 4)
	if err != nil {
		t.Fatalf("创建媒体记录失败: %v", err)
	}

	// 设置显示名：仅库内 display_name 变更，磁盘与 file_name/file_path 不变
	updated, err := svc.UpdateDisplayName(mf.ID, "  我的影片  ")
	if err != nil {
		t.Fatalf("更新显示名失败: %v", err)
	}
	if updated.DisplayName != "我的影片" {
		t.Fatalf("显示名应去首尾空白, 实际 %q", updated.DisplayName)
	}
	if updated.FileName != "real.mp4" || updated.FilePath != filepath.ToSlash(diskPath) {
		t.Fatalf("显示名修改不应改动真实文件名/路径: %+v", updated)
	}
	// 磁盘文件保持原名
	if _, err := os.Stat(diskPath); err != nil {
		t.Fatalf("磁盘文件应保持原名: %v", err)
	}
	// 数据库已落库
	got, _ := svc.GetMediaFileByID(mf.ID)
	if got.DisplayName != "我的影片" || got.FileName != "real.mp4" {
		t.Fatalf("数据库记录未按预期更新: %+v", got)
	}

	// 清空显示名：置空表示清除，回退展示交由前端处理
	cleared, err := svc.UpdateDisplayName(mf.ID, "   ")
	if err != nil {
		t.Fatalf("清空显示名失败: %v", err)
	}
	if cleared.DisplayName != "" {
		t.Fatalf("清空后显示名应为空, 实际 %q", cleared.DisplayName)
	}
}

func TestUpdateDisplayName_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.UpdateDisplayName(9999, "x"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("不存在的记录期望 gorm.ErrRecordNotFound, 实际 %v", err)
	}
}

func TestListLibraryPathViews(t *testing.T) {
	svc, db := newTestService(t)

	lp1, _ := svc.CreateLibraryPath(t.TempDir(), "local", "库1")
	lp2, _ := svc.CreateLibraryPath(t.TempDir(), "local", "库2")

	// 库1：2 个媒体文件，其中 1 个软删；库2：1 个媒体文件；空库不计
	_, _ = svc.CreateMediaFile(lp1.ID, "/tmp/lib1/a.mp4", 1)
	soft, _ := svc.CreateMediaFile(lp1.ID, "/tmp/lib1/b.mp4", 1)
	_, _ = svc.CreateMediaFile(lp2.ID, "/tmp/lib2/c.mp4", 1)

	// 直接标记软删（FR-25 口径），计数应排除
	now := time.Now()
	if err := db.Model(&models.MediaFile{}).Where("id = ?", soft.ID).
		Update("deleted_at", now).Error; err != nil {
		t.Fatalf("标记软删失败: %v", err)
	}

	views, err := svc.ListLibraryPathViews()
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("期望 2 条记录, 实际 %d", len(views))
	}

	counts := map[int64]int64{}
	for _, v := range views {
		counts[v.ID] = v.MediaCount
	}
	if counts[lp1.ID] != 1 {
		t.Fatalf("库1 媒体数量应为 1（软删排除）, 实际 %d", counts[lp1.ID])
	}
	if counts[lp2.ID] != 1 {
		t.Fatalf("库2 媒体数量应为 1, 实际 %d", counts[lp2.ID])
	}
}

func TestListLibraryPathViews_EmptyLibraryCountsZero(t *testing.T) {
	svc, _ := newTestService(t)
	lp, _ := svc.CreateLibraryPath(t.TempDir(), "local", "空库")

	views, err := svc.ListLibraryPathViews()
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(views) != 1 || views[0].ID != lp.ID {
		t.Fatalf("期望返回该空库, 实际 %+v", views)
	}
	if views[0].MediaCount != 0 {
		t.Fatalf("空库媒体数量应为 0, 实际 %d", views[0].MediaCount)
	}
}

// TestBrowseDirectory_AggregateRoot 聚合虚拟根（FR-66）：parent_path=__root__ 时
// 列出所有启用库作为顶层目录、各项携带 library_id，禁用库不列。
func TestBrowseDirectory_AggregateRoot(t *testing.T) {
	svc, db := newTestService(t)

	lp1, _ := svc.CreateLibraryPath(t.TempDir(), "local", "电影库")
	lp2, _ := svc.CreateLibraryPath(t.TempDir(), "local", "动漫库")
	lp3, _ := svc.CreateLibraryPath(t.TempDir(), "local", "已禁用库")
	// 直接禁用第三个库
	if err := db.Model(&models.LibraryPath{}).Where("id = ?", lp3.ID).Update("enabled", 0).Error; err != nil {
		t.Fatalf("禁用库失败: %v", err)
	}

	// library_id 传 0（聚合根忽略 library_id）
	resp, err := svc.BrowseDirectory(0, BrowseRootMarker)
	if err != nil {
		t.Fatalf("聚合根浏览失败: %v", err)
	}

	// 仅返回 2 个启用库作为顶层目录，禁用库不列
	if len(resp.Directories) != 2 {
		t.Fatalf("聚合根期望 2 个启用库目录, 实际 %d: %+v", len(resp.Directories), resp.Directories)
	}
	if len(resp.Files) != 0 {
		t.Fatalf("聚合根顶层不应有文件, 实际 %d", len(resp.Files))
	}
	// 各目录项携带对应 library_id 与 label
	byID := map[int64]models.DirInfo{}
	for _, d := range resp.Directories {
		byID[d.LibraryID] = d
	}
	if d, ok := byID[lp1.ID]; !ok || d.Name != "电影库" {
		t.Fatalf("聚合根缺电影库或 name 不符: %+v", byID)
	}
	if d, ok := byID[lp2.ID]; !ok || d.Name != "动漫库" {
		t.Fatalf("聚合根缺动漫库或 name 不符: %+v", byID)
	}
	if _, ok := byID[lp3.ID]; ok {
		t.Fatal("聚合根不应包含已禁用库")
	}
	// 面包屑单段虚拟根
	if len(resp.Breadcrumbs) != 1 || resp.Breadcrumbs[0].Path != BrowseRootMarker {
		t.Fatalf("聚合根面包屑应为单段虚拟根, 实际 %+v", resp.Breadcrumbs)
	}
}

// TestBrowseDirectory_SingleLibraryUnchanged 单库下钻分支（FR-66 不回归）：
// 带 library_id + 真实 parent_path 时仍按原前缀聚合逻辑返回子目录与文件。
func TestBrowseDirectory_SingleLibraryUnchanged(t *testing.T) {
	svc, _ := newTestService(t)
	lp, _ := svc.CreateLibraryPath(t.TempDir(), "local", "电影库")

	base := strings.ReplaceAll(lp.Path, `\`, `/`)
	_, _ = svc.CreateMediaFile(lp.ID, base+"/动作片/a.mp4", 1)
	_, _ = svc.CreateMediaFile(lp.ID, base+"/b.mp4", 1)

	resp, err := svc.BrowseDirectory(lp.ID, base)
	if err != nil {
		t.Fatalf("单库浏览失败: %v", err)
	}
	if len(resp.Directories) != 1 || resp.Directories[0].Name != "动作片" {
		t.Fatalf("单库应列出子目录 动作片, 实际 %+v", resp.Directories)
	}
	// 子目录项不应携带 library_id（仅聚合根项填充）
	if resp.Directories[0].LibraryID != 0 {
		t.Fatalf("单库子目录项不应携带 library_id, 实际 %d", resp.Directories[0].LibraryID)
	}
	if len(resp.Files) != 1 || filepath.Base(resp.Files[0].FilePath) != "b.mp4" {
		t.Fatalf("单库应列出直接文件 b.mp4, 实际 %+v", resp.Files)
	}
}
