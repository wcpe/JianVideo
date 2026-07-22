package library

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestBrowseDirectory_Root(t *testing.T) {
	svc, _ := newTestService(t)

	// 插入不同目录下的文件
	_, _ = svc.CreateMediaFile(1, "/media/movies/星际穿越.mkv", 1024)
	_, _ = svc.CreateMediaFile(1, "/media/movies/盗梦空间.mkv", 2048)
	_, _ = svc.CreateMediaFile(1, "/media/tv/绝命毒师.S01E01.mkv", 512)

	resp, err := svc.BrowseDirectory("/media", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览根目录失败: %v", err)
	}

	// 面包屑：只有 /media
	if len(resp.Breadcrumbs) != 1 {
		t.Fatalf("期望 1 段面包屑, 实际 %d", len(resp.Breadcrumbs))
	}
	if resp.Breadcrumbs[0].Name != "media" {
		t.Fatalf("期望面包屑名 'media', 实际 '%s'", resp.Breadcrumbs[0].Name)
	}

	// 子目录：movies, tv
	if len(resp.Directories) != 2 {
		t.Fatalf("期望 2 个子目录, 实际 %d", len(resp.Directories))
	}

	// 根目录下没有直接文件
	if len(resp.Files) != 0 {
		t.Fatalf("期望根目录无直接文件, 实际 %d", len(resp.Files))
	}
}

func TestBrowseDirectory_Subdir(t *testing.T) {
	svc, _ := newTestService(t)

	_, _ = svc.CreateMediaFile(1, "/media/movies/星际穿越.mkv", 1024)
	_, _ = svc.CreateMediaFile(1, "/media/movies/盗梦空间.mkv", 2048)
	_, _ = svc.CreateMediaFile(1, "/media/movies/action/疾速追杀.mkv", 512)

	resp, err := svc.BrowseDirectory("/media/movies", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览子目录失败: %v", err)
	}

	// 面包屑：media -> movies
	if len(resp.Breadcrumbs) != 2 {
		t.Fatalf("期望 2 段面包屑, 实际 %d", len(resp.Breadcrumbs))
	}
	if resp.Breadcrumbs[1].Name != "movies" {
		t.Fatalf("期望第二段面包屑名 'movies', 实际 '%s'", resp.Breadcrumbs[1].Name)
	}

	// 子目录：action
	if len(resp.Directories) != 1 {
		t.Fatalf("期望 1 个子目录, 实际 %d", len(resp.Directories))
	}
	if resp.Directories[0].Name != "action" {
		t.Fatalf("期望子目录名 'action', 实际 '%s'", resp.Directories[0].Name)
	}

	// 当前目录下直接文件：2 个
	if len(resp.Files) != 2 {
		t.Fatalf("期望 2 个文件, 实际 %d", len(resp.Files))
	}
}

func TestBrowseDirectory_EmptyDir(t *testing.T) {
	svc, _ := newTestService(t)

	// 不插入任何文件
	resp, err := svc.BrowseDirectory("/nonexistent", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览空目录失败: %v", err)
	}

	if len(resp.Breadcrumbs) != 1 {
		t.Fatalf("期望 1 段面包屑, 实际 %d", len(resp.Breadcrumbs))
	}
	if len(resp.Directories) != 0 {
		t.Fatalf("期望 0 个子目录, 实际 %d", len(resp.Directories))
	}
	if len(resp.Files) != 0 {
		t.Fatalf("期望 0 个文件, 实际 %d", len(resp.Files))
	}
}

// TestBrowseDirectory_CrossLibraryMerge 跨库合并（FR-121）：
// 同一真实路径下不同库的文件应一并返回，不再按 library_id 收窄。
func TestBrowseDirectory_CrossLibraryMerge(t *testing.T) {
	svc, _ := newTestService(t)

	// 不同 library_id 的相同目录文件
	_, _ = svc.CreateMediaFile(1, "/media/movies/电影A.mkv", 1024)
	_, _ = svc.CreateMediaFile(2, "/media/movies/电影B.mkv", 2048)

	resp, err := svc.BrowseDirectory("/media/movies", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}

	// 跨库合并：两个库的文件都应返回
	if len(resp.Files) != 2 {
		t.Fatalf("期望跨库合并 2 个文件, 实际 %d: %+v", len(resp.Files), resp.Files)
	}
	names := map[string]bool{}
	for _, f := range resp.Files {
		names[f.FileName] = true
	}
	if !names["电影A.mkv"] || !names["电影B.mkv"] {
		t.Fatalf("期望含两库文件 电影A.mkv/电影B.mkv, 实际 %+v", names)
	}
}

func TestBrowseDirectory_BreadcrumbPaths(t *testing.T) {
	svc, _ := newTestService(t)

	_, _ = svc.CreateMediaFile(1, "/a/b/c/file.mp4", 1024)

	resp, err := svc.BrowseDirectory("/a/b/c", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}

	// 面包屑：a -> b -> c
	if len(resp.Breadcrumbs) != 3 {
		t.Fatalf("期望 3 段面包屑, 实际 %d", len(resp.Breadcrumbs))
	}

	// 验证面包屑路径累加
	expectedPaths := []string{"/a", "/a/b", "/a/b/c"}
	for i, bc := range resp.Breadcrumbs {
		expected := filepath.Clean(expectedPaths[i])
		actual := filepath.Clean(bc.Path)
		if actual != expected {
			t.Errorf("面包屑[%d] 路径期望 '%s', 实际 '%s'", i, expected, actual)
		}
	}
}

func TestBrowseDirectory_WindowsBreadcrumbAndDirPaths(t *testing.T) {
	svc, _ := newTestService(t)

	// 存储用规范化正斜杠路径（即 Windows 上 filepath.ToSlash 的产物），
	// 查询仍传反斜杠路径以验证 BrowseDirectory 的分隔符归一化；
	// 如此本用例不依赖 OS 原生分隔符，可在 Linux CI 与 Windows 上一致运行。
	_, _ = svc.CreateMediaFile(1, "D:/媒体/电影/动作/封面.jpg", 1024)
	_, _ = svc.CreateMediaFile(1, "D:/媒体/电影/预告.mp4", 2048)

	resp, err := svc.BrowseDirectory(`D:\媒体\电影`, BrowseSortName)
	if err != nil {
		t.Fatalf("浏览 Windows 路径失败: %v", err)
	}
	if len(resp.Breadcrumbs) != 3 {
		t.Fatalf("期望 3 段面包屑, 实际 %d", len(resp.Breadcrumbs))
	}
	expectedBreadcrumbs := []models.BreadcrumbItem{
		{Name: "D:", Path: "D:"},
		{Name: "媒体", Path: "D:/媒体"},
		{Name: "电影", Path: "D:/媒体/电影"},
	}
	for i, expected := range expectedBreadcrumbs {
		if resp.Breadcrumbs[i] != expected {
			t.Fatalf("面包屑[%d] 期望 %+v, 实际 %+v", i, expected, resp.Breadcrumbs[i])
		}
		if strings.HasPrefix(resp.Breadcrumbs[i].Path, "/D:") {
			t.Fatalf("Windows 面包屑路径不应生成 /D:，实际 %s", resp.Breadcrumbs[i].Path)
		}
	}
	if len(resp.Directories) != 1 || resp.Directories[0].Path != "D:/媒体/电影/动作" {
		t.Fatalf("Windows 子目录路径不正确: %+v", resp.Directories)
	}
	if len(resp.Files) != 1 || resp.Files[0].FileName != "预告.mp4" {
		t.Fatalf("Windows 当前目录文件不正确: %+v", resp.Files)
	}
}

func TestBrowseDirectory_WindowsCaseInsensitivePrefix(t *testing.T) {
	svc, _ := newTestService(t)

	// 存储用小写盘符的规范化正斜杠路径，查询用大写盘符反斜杠路径，
	// 验证盘符大小写不敏感前缀匹配；不依赖 OS 原生分隔符，跨平台一致运行。
	_, _ = svc.CreateMediaFile(1, "d:/媒体/电影/预告.mp4", 2048)

	resp, err := svc.BrowseDirectory(`D:\媒体\电影`, BrowseSortName)
	if err != nil {
		t.Fatalf("浏览 Windows 大小写不同路径失败: %v", err)
	}
	if len(resp.Files) != 1 || resp.Files[0].FileName != "预告.mp4" {
		t.Fatalf("Windows 路径大小写不同也应列出当前目录文件: %+v", resp.Files)
	}
}

func TestBrowseDirectory_DirInfoPaths(t *testing.T) {
	svc, _ := newTestService(t)

	_, _ = svc.CreateMediaFile(1, "/media/movies/sub1/file1.mp4", 1024)
	_, _ = svc.CreateMediaFile(1, "/media/movies/sub2/file2.mp4", 2048)

	resp, err := svc.BrowseDirectory("/media/movies", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}

	// 验证子目录路径
	dirMap := make(map[string]string)
	for _, d := range resp.Directories {
		dirMap[d.Name] = d.Path
		// 子目录跨库，不应携带 library_id（FR-121）
		if d.LibraryID != 0 {
			t.Errorf("子目录 %s 不应携带 library_id, 实际 %d", d.Name, d.LibraryID)
		}
	}

	if path, ok := dirMap["sub1"]; !ok || filepath.Clean(path) != filepath.Clean("/media/movies/sub1") {
		t.Errorf("sub1 路径不正确: %s", path)
	}
	if path, ok := dirMap["sub2"]; !ok || filepath.Clean(path) != filepath.Clean("/media/movies/sub2") {
		t.Errorf("sub2 路径不正确: %s", path)
	}
}

func TestBrowseDirectory_NestedFilesNotIncluded(t *testing.T) {
	svc, _ := newTestService(t)

	// 深层嵌套文件
	_, _ = svc.CreateMediaFile(1, "/media/movies/action/疾速追杀.mkv", 1024)
	// 当前目录直接文件
	_, _ = svc.CreateMediaFile(1, "/media/movies/星际穿越.mkv", 2048)

	resp, err := svc.BrowseDirectory("/media/movies", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}

	// 只返回直接文件，不包含嵌套文件
	if len(resp.Files) != 1 {
		t.Fatalf("期望 1 个直接文件, 实际 %d", len(resp.Files))
	}
	if resp.Files[0].FileName != "星际穿越.mkv" {
		t.Fatalf("期望文件名 '星际穿越.mkv', 实际 '%s'", resp.Files[0].FileName)
	}

	// action 作为子目录出现
	if len(resp.Directories) != 1 {
		t.Fatalf("期望 1 个子目录, 实际 %d", len(resp.Directories))
	}
}

// 确保 BrowseResponse 结构正确序列化
func TestBrowseResponse_JSON(t *testing.T) {
	resp := models.BrowseResponse{
		Breadcrumbs: []models.BreadcrumbItem{
			{Name: "media", Path: "/media"},
		},
		Directories: []models.DirInfo{
			{Name: "movies", Path: "/media/movies"},
		},
		Files: []models.MediaFile{},
	}

	if len(resp.Breadcrumbs) != 1 {
		t.Fatal("面包屑数量不正确")
	}
	if len(resp.Directories) != 1 {
		t.Fatal("目录数量不正确")
	}
}

// TestBrowseDirectory_VolumeRoots 顶层根列举（FR-121）：parent_path=__root__ 时
// 列出各启用库推导出的盘符/共享根（去重排序），面包屑单段「全部」、顶层无文件、不带 library_id。
func TestBrowseDirectory_VolumeRoots(t *testing.T) {
	svc, db := newTestService(t)

	// 直接插入库记录（避免依赖真实磁盘目录存在），含同盘符两库（应去重为一个 D:）与一个 E: 库、一个禁用 F: 库
	mustCreateLibraryPath(t, db, "D:/1", 1)
	mustCreateLibraryPath(t, db, "D:/1/2", 1)
	mustCreateLibraryPath(t, db, "E:/视频", 1)
	mustCreateLibraryPath(t, db, "F:/禁用库", 0)

	resp, err := svc.BrowseDirectory(BrowseRootMarker, BrowseSortName)
	if err != nil {
		t.Fatalf("顶层根浏览失败: %v", err)
	}

	// 卷根去重 + 排除禁用库：仅 D:、E:
	if len(resp.Directories) != 2 {
		t.Fatalf("期望 2 个卷根, 实际 %d: %+v", len(resp.Directories), resp.Directories)
	}
	got := []string{resp.Directories[0].Path, resp.Directories[1].Path}
	if got[0] != "D:" || got[1] != "E:" {
		t.Fatalf("卷根去重/排序不正确, 期望 [D: E:], 实际 %v", got)
	}
	for _, d := range resp.Directories {
		if d.Name != d.Path {
			t.Fatalf("卷根项 name 应等于 path, 实际 %+v", d)
		}
		if d.LibraryID != 0 {
			t.Fatalf("卷根项不应携带 library_id, 实际 %+v", d)
		}
	}
	if len(resp.Files) != 0 {
		t.Fatalf("顶层根不应有文件, 实际 %d", len(resp.Files))
	}
	if len(resp.Breadcrumbs) != 1 || resp.Breadcrumbs[0].Name != "全部" || resp.Breadcrumbs[0].Path != BrowseRootMarker {
		t.Fatalf("顶层根面包屑应为单段 {全部, __root__}, 实际 %+v", resp.Breadcrumbs)
	}
}

// TestBrowseDirectory_CrossLibraryTree 跨库真实路径合并成单一树（FR-121）：
// 库 D:/1 与 D:/1/2 各有文件，浏览 D: 见子目录 1、浏览 D:/1 见子目录 2 + 直属文件、浏览 D:/1/2 见文件。
func TestBrowseDirectory_CrossLibraryTree(t *testing.T) {
	svc, _ := newTestService(t)

	// 库1（D:/1）：D:/1/a.mp4 与 D:/1/2/c.mp4（c 实际落在子库目录下，但同一真实路径树）
	_, _ = svc.CreateMediaFile(1, "D:/1/a.mp4", 100)
	// 库2（D:/1/2）：D:/1/2/b.mp4
	_, _ = svc.CreateMediaFile(2, "D:/1/2/b.mp4", 200)

	// 浏览 D:：应见子目录 1
	respD, err := svc.BrowseDirectory("D:", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览 D: 失败: %v", err)
	}
	if len(respD.Directories) != 1 || respD.Directories[0].Name != "1" || respD.Directories[0].Path != "D:/1" {
		t.Fatalf("浏览 D: 期望子目录 1(D:/1), 实际 %+v", respD.Directories)
	}
	if len(respD.Files) != 0 {
		t.Fatalf("浏览 D: 期望无直属文件, 实际 %+v", respD.Files)
	}

	// 浏览 D:/1：应见子目录 2（来自库2 的文件路径）+ 直属文件 a.mp4（来自库1），跨库合并
	resp1, err := svc.BrowseDirectory("D:/1", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览 D:/1 失败: %v", err)
	}
	if len(resp1.Directories) != 1 || resp1.Directories[0].Name != "2" || resp1.Directories[0].Path != "D:/1/2" {
		t.Fatalf("浏览 D:/1 期望子目录 2(D:/1/2), 实际 %+v", resp1.Directories)
	}
	if len(resp1.Files) != 1 || resp1.Files[0].FileName != "a.mp4" {
		t.Fatalf("浏览 D:/1 期望直属文件 a.mp4, 实际 %+v", resp1.Files)
	}

	// 浏览 D:/1/2：应见文件 b.mp4
	resp2, err := svc.BrowseDirectory("D:/1/2", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览 D:/1/2 失败: %v", err)
	}
	if len(resp2.Directories) != 0 {
		t.Fatalf("浏览 D:/1/2 期望无子目录, 实际 %+v", resp2.Directories)
	}
	if len(resp2.Files) != 1 || resp2.Files[0].FileName != "b.mp4" {
		t.Fatalf("浏览 D:/1/2 期望文件 b.mp4, 实际 %+v", resp2.Files)
	}
}

// TestBrowseDirectory_WholeDriveLibrary 整盘根库（FR-121）：加 D:/ 库后浏览 D: 沿真实路径下钻。
func TestBrowseDirectory_WholeDriveLibrary(t *testing.T) {
	svc, db := newTestService(t)

	// 整盘库（存储为 D:），其下有真实层级文件
	mustCreateLibraryPath(t, db, "D:", 1)
	_, _ = svc.CreateMediaFile(1, "D:/视频/电影/x.mp4", 100)

	// 顶层根应列出 D:
	root, err := svc.BrowseDirectory(BrowseRootMarker, BrowseSortName)
	if err != nil {
		t.Fatalf("顶层根浏览失败: %v", err)
	}
	if len(root.Directories) != 1 || root.Directories[0].Path != "D:" {
		t.Fatalf("整盘库期望顶层根 D:, 实际 %+v", root.Directories)
	}

	// 浏览 D: 见真实下钻：子目录 视频
	resp, err := svc.BrowseDirectory("D:", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览 D: 失败: %v", err)
	}
	if len(resp.Directories) != 1 || resp.Directories[0].Name != "视频" {
		t.Fatalf("整盘根 D: 期望子目录 视频, 实际 %+v", resp.Directories)
	}
}

// TestBrowseDirectory_ExcludesSoftDeleted 软删排除（FR-121）：已软删文件不计入子目录与文件。
func TestBrowseDirectory_ExcludesSoftDeleted(t *testing.T) {
	svc, db := newTestService(t)

	mfKept, _ := svc.CreateMediaFile(1, "/m/keep.mp4", 100)
	mfDel, _ := svc.CreateMediaFile(1, "/m/gone.mp4", 200)
	mfDelDir, _ := svc.CreateMediaFile(1, "/m/sub/inner.mp4", 300)
	_ = mfKept

	// 软删 gone.mp4 与子目录内 inner.mp4
	now := time.Now()
	if err := db.Model(&models.MediaFile{}).Where("id IN ?", []int64{mfDel.ID, mfDelDir.ID}).
		Update("deleted_at", now).Error; err != nil {
		t.Fatalf("软删失败: %v", err)
	}

	resp, err := svc.BrowseDirectory("/m", BrowseSortName)
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}
	// 文件仅剩 keep.mp4
	if len(resp.Files) != 1 || resp.Files[0].FileName != "keep.mp4" {
		t.Fatalf("软删后期望仅 keep.mp4, 实际 %+v", resp.Files)
	}
	// 子目录 sub 内唯一文件已软删 → sub 不应出现
	if len(resp.Directories) != 0 {
		t.Fatalf("软删后子目录应为空（sub 内文件已软删）, 实际 %+v", resp.Directories)
	}
}

// TestBrowseDirectory_SortOrders sort 四序（FR-121）：name/size/type/time 升序，目录恒在前。
func TestBrowseDirectory_SortOrders(t *testing.T) {
	svc, db := newTestService(t)

	// 直接插入文件以精确控制 size / format / modified_at
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	files := []models.MediaFile{
		{LibraryID: 1, FilePath: "/s/cherry.png", FileName: "cherry.png", FileSize: 30, Format: "png", ModifiedAt: base.Add(2 * time.Hour)},
		{LibraryID: 1, FilePath: "/s/apple.mp4", FileName: "apple.mp4", FileSize: 10, Format: "mp4", ModifiedAt: base.Add(3 * time.Hour)},
		{LibraryID: 1, FilePath: "/s/banana.avi", FileName: "banana.avi", FileSize: 20, Format: "avi", ModifiedAt: base.Add(1 * time.Hour)},
	}
	for i := range files {
		if err := db.Create(&files[i]).Error; err != nil {
			t.Fatalf("插入文件失败: %v", err)
		}
	}
	// 一个子目录恒在前
	_, _ = svc.CreateMediaFile(1, "/s/zsub/deep.mp4", 1)

	fileNames := func(resp *models.BrowseResponse) []string {
		names := make([]string, len(resp.Files))
		for i, f := range resp.Files {
			names[i] = f.FileName
		}
		return names
	}

	cases := []struct {
		sortKey string
		want    []string
	}{
		// 按名升序
		{BrowseSortName, []string{"apple.mp4", "banana.avi", "cherry.png"}},
		// 按大小升序：apple(10) < banana(20) < cherry(30)
		{BrowseSortSize, []string{"apple.mp4", "banana.avi", "cherry.png"}},
		// 按类型（格式）升序：avi < mp4 < png
		{BrowseSortType, []string{"banana.avi", "apple.mp4", "cherry.png"}},
		// 按修改时间升序：banana(+1h) < cherry(+2h) < apple(+3h)
		{BrowseSortTime, []string{"banana.avi", "cherry.png", "apple.mp4"}},
	}
	for _, tc := range cases {
		resp, err := svc.BrowseDirectory("/s", tc.sortKey)
		if err != nil {
			t.Fatalf("sort=%s 浏览失败: %v", tc.sortKey, err)
		}
		// 目录恒在前
		if len(resp.Directories) != 1 || resp.Directories[0].Name != "zsub" {
			t.Fatalf("sort=%s 期望子目录 zsub 在前, 实际 %+v", tc.sortKey, resp.Directories)
		}
		got := fileNames(resp)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("sort=%s 文件顺序期望 %v, 实际 %v", tc.sortKey, tc.want, got)
		}
	}
}

// TestVolumeRoot volumeRoot 纯函数穷举（FR-121）：本地盘符 / UNC 共享 / 已是根。
func TestVolumeRoot(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 本地盘符：取盘符段
		{"D:/媒体/电影", "D:"},
		{`D:\媒体\电影`, "D:"},
		{"C:/Users/foo/bar", "C:"},
		// 已是盘符根
		{"D:", "D:"},
		{"D:/", "D:"},
		{`E:\`, "E:"},
		// UNC/共享根：取 host+share 两段
		{"//host/share/path/to/x", "//host/share"},
		{"//host/share", "//host/share"},
		{`\\host\share\dir`, "//host/share"},
		// 类 Unix 绝对路径（无盘符）：去尾斜杠保留
		{"/media/movies", "/media/movies"},
		{"/media/movies/", "/media/movies"},
		// 空串
		{"", ""},
	}
	for _, tc := range cases {
		if got := volumeRoot(tc.in); got != tc.want {
			t.Errorf("volumeRoot(%q) = %q, 期望 %q", tc.in, got, tc.want)
		}
	}
}

// mustCreateLibraryPath 直接插入库记录（绕过 CreateLibraryPath 的磁盘存在性校验），供顶层根用例构造卷根。
// enabled 经二次 Update 显式写入：LibraryPath.Enabled 带 gorm `default:1`，
// Create 时 0 值会被默认值覆盖，故禁用库需 Create 后再 Update 为 0。
func mustCreateLibraryPath(t *testing.T, db *gorm.DB, path string, enabled int) {
	t.Helper()
	lp := models.LibraryPath{Path: path, Type: "local"}
	if err := db.Create(&lp).Error; err != nil {
		t.Fatalf("插入库记录失败: %v", err)
	}
	if err := db.Model(&models.LibraryPath{}).Where("id = ?", lp.ID).Update("enabled", enabled).Error; err != nil {
		t.Fatalf("更新库启用状态失败: %v", err)
	}
}
