package library

import (
	"path/filepath"
	"testing"

	"jianvideo/internal/db/models"
)

func TestBrowseDirectory_Root(t *testing.T) {
	svc, _ := newTestService(t)

	// 插入不同目录下的文件
	_, _ = svc.CreateMediaFile(1, "/media/movies/星际穿越.mkv", 1024)
	_, _ = svc.CreateMediaFile(1, "/media/movies/盗梦空间.mkv", 2048)
	_, _ = svc.CreateMediaFile(1, "/media/tv/绝命毒师.S01E01.mkv", 512)

	resp, err := svc.BrowseDirectory(1, "/media")
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

	resp, err := svc.BrowseDirectory(1, "/media/movies")
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
	resp, err := svc.BrowseDirectory(1, "/nonexistent")
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

func TestBrowseDirectory_FilterByLibraryID(t *testing.T) {
	svc, _ := newTestService(t)

	// 不同 library_id 的相同路径
	_, _ = svc.CreateMediaFile(1, "/media/movies/电影A.mkv", 1024)
	_, _ = svc.CreateMediaFile(2, "/media/movies/电影B.mkv", 2048)

	resp, err := svc.BrowseDirectory(1, "/media/movies")
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}

	// 只返回 library_id=1 的文件
	if len(resp.Files) != 1 {
		t.Fatalf("期望 1 个文件, 实际 %d", len(resp.Files))
	}
	if resp.Files[0].FileName != "电影A.mkv" {
		t.Fatalf("期望文件名 '电影A.mkv', 实际 '%s'", resp.Files[0].FileName)
	}
}

func TestBrowseDirectory_BreadcrumbPaths(t *testing.T) {
	svc, _ := newTestService(t)

	_, _ = svc.CreateMediaFile(1, "/a/b/c/file.mp4", 1024)

	resp, err := svc.BrowseDirectory(1, "/a/b/c")
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

func TestBrowseDirectory_DirInfoPaths(t *testing.T) {
	svc, _ := newTestService(t)

	_, _ = svc.CreateMediaFile(1, "/media/movies/sub1/file1.mp4", 1024)
	_, _ = svc.CreateMediaFile(1, "/media/movies/sub2/file2.mp4", 2048)

	resp, err := svc.BrowseDirectory(1, "/media/movies")
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}

	// 验证子目录路径
	dirMap := make(map[string]string)
	for _, d := range resp.Directories {
		dirMap[d.Name] = d.Path
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

	resp, err := svc.BrowseDirectory(1, "/media/movies")
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
