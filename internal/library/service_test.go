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

func TestGetMediaFileByID_NotFound(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.GetMediaFileByID(99999)
	if err == nil {
		t.Fatal("不存在的媒体文件应返回错误")
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
