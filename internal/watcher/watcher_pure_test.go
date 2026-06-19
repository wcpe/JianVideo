package watcher

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"jianvideo/internal/db/models"
)

// 路径统一使用正斜杠，与数据库存储和 pathToLib 的 key 格式一致
const sep = "/"

// buildBreadcrumbs 将路径拆分为面包屑段（与 library.Service 中相同的逻辑，供 watcher 包测试使用）。
func buildBreadcrumbs(path string) []models.BreadcrumbItem {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var items []models.BreadcrumbItem
	var current string
	for _, p := range parts {
		if p == "" {
			continue
		}
		current += "/" + p
		items = append(items, models.BreadcrumbItem{
			Name: p,
			Path: current,
		})
	}
	if len(items) == 0 {
		items = append(items, models.BreadcrumbItem{Name: "/", Path: "/"})
	}
	return items
}

// TestIsMediaFile 表驱动测试：验证各种文件路径是否为媒体文件。
func TestIsMediaFile(t *testing.T) {
	tests := []struct {
		path   string
		expect bool
	}{
		{"test.mp4", true},
		{"test.MP4", true},
		{"test.mkv", true},
		{"test.avi", true},
		{"test.mov", true},
		{"test.webm", true},
		{"test.rmvb", true},
		{"test.ts", true},
		{"test.flv", true},
		{"test.wmv", true},
		{"test.m4v", true},
		{"test.jpg", true},
		{"test.PNG", true},
		{"test.webp", true},
		{"test.txt", false},
		{"test", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expect, isMediaFile(tt.path))
		})
	}
}

// TestIsMediaFile_ExtCaseInsensitive 验证大写扩展名均返回 true。
func TestIsMediaFile_ExtCaseInsensitive(t *testing.T) {
	exts := []string{".MP4", ".MKV", ".AVI", ".JPG", ".PNG"}
	for _, ext := range exts {
		t.Run(ext, func(t *testing.T) {
			assert.True(t, isMediaFile("media"+ext))
		})
	}
}

// TestFindLibraryID 验证根据文件路径查找所属 library_id。
func TestFindLibraryID(t *testing.T) {
	w := &Watcher{
		pathToLib: map[string]int64{
			sep + "movies":                  1,
			sep + "movies" + sep + "action": 2,
			sep + "tv":                      3,
		},
	}

	tests := []struct {
		name     string
		filePath string
		expectID int64
	}{
		{
			name:     "精确匹配已知库目录",
			filePath: sep + "movies" + sep + "test.mp4",
			expectID: 1,
		},
		{
			name:     "匹配子目录中的文件",
			filePath: sep + "movies" + sep + "action" + sep + "film.mp4",
			expectID: 2,
		},
		{
			name:     "不匹配任何库目录",
			filePath: sep + "other" + sep + "test.mp4",
			expectID: 0,
		},
		{
			name:     "向上查找匹配父目录",
			filePath: sep + "movies" + sep + "action" + sep + "test.mp4",
			expectID: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectID, w.findLibraryID(tt.filePath))
		})
	}
}

// TestBuildBreadcrumbs 表驱动测试：验证路径拆分为面包屑的正确性。
func TestBuildBreadcrumbs(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []models.BreadcrumbItem
	}{
		{
			name: "两级路径",
			path: "/movies/action",
			expected: []models.BreadcrumbItem{
				{Name: "movies", Path: "/movies"},
				{Name: "action", Path: "/movies/action"},
			},
		},
		{
			name: "根路径",
			path: "/",
			expected: []models.BreadcrumbItem{
				{Name: "/", Path: "/"},
			},
		},
		{
			name: "三级路径",
			path: "/a/b/c",
			expected: []models.BreadcrumbItem{
				{Name: "a", Path: "/a"},
				{Name: "b", Path: "/a/b"},
				{Name: "c", Path: "/a/b/c"},
			},
		},
		{
			name: "空路径处理",
			path: "",
			expected: []models.BreadcrumbItem{
				{Name: "/", Path: "/"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildBreadcrumbs(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestBuildBreadcrumbs_SinglePart 验证单段路径的面包屑。
func TestBuildBreadcrumbs_SinglePart(t *testing.T) {
	result := buildBreadcrumbs("/movies")
	expected := []models.BreadcrumbItem{
		{Name: "movies", Path: "/movies"},
	}
	assert.Equal(t, expected, result)
}

// TestNewWatcher 验证 New 返回非 nil 的 Watcher 实例。
func TestNewWatcher(t *testing.T) {
	w, err := New(nil)
	assert.NoError(t, err)
	assert.NotNil(t, w)
}
