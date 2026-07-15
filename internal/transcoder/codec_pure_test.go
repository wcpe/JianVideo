package transcoder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractPrimaryFormat 测试 extractPrimaryFormat 纯函数。
func TestExtractPrimaryFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"单一格式", "mp4", "mp4"},
		{"多格式取第一个", "mov,mp4,m4a", "mov"},
		{"空字符串", "", ""},
		{"带空格需 TrimSpace", "  matroska  ", "matroska"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPrimaryFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFormatContainerForStorage 测试 formatContainerForStorage 纯函数。
func TestFormatContainerForStorage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"mov 映射为 mp4", "mov", "mp4"},
		{"mp4 保持不变", "mp4", "mp4"},
		{"m4a 映射为 mp4", "m4a", "mp4"},
		{"matroska 映射为 mkv", "matroska", "mkv"},
		{"matroska,webm 先取主格式再映射为 mkv", "matroska,webm", "mkv"},
		{"avi 保持不变", "avi", "avi"},
		{"mpegts 映射为 ts", "mpegts", "ts"},
		{"未知格式原样返回小写", "unknown_format", "unknown_format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatContainerForStorage(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDetectContainerFormat 测试 detectContainerFormat 纯函数（使用临时文件）。
func TestDetectContainerFormat(t *testing.T) {
	tests := []struct {
		name     string
		ext      string
		expected string
	}{
		{"mp4 文件", "mp4", "mp4"},
		{"mkv 文件", "mkv", "mkv"},
		{"avi 文件", "avi", "avi"},
		{"mov 文件", "mov", "mov"},
		{"webm 文件", "webm", "webm"},
		{"ts 文件", "ts", "ts"},
		{"rmvb 文件", "rmvb", "rmvb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			videoPath := filepath.Join(tmp, "video."+tt.ext)
			err := os.WriteFile(videoPath, []byte("fake"), 0o644)
			require.NoError(t, err)

			result := detectContainerFormat(videoPath, "")
			assert.Equal(t, tt.expected, result)
		})
	}

	// 未知扩展名：回退到 formatContainerForStorage
	t.Run("未知扩展名回退到 ffprobe 结果", func(t *testing.T) {
		tmp := t.TempDir()
		videoPath := filepath.Join(tmp, "video.xyz")
		err := os.WriteFile(videoPath, []byte("fake"), 0o644)
		require.NoError(t, err)

		result := detectContainerFormat(videoPath, "matroska,webm")
		// 扩展名 .xyz 不在映射中，回退到 formatContainerForStorage → "mkv"
		assert.Equal(t, "mkv", result)
	})
}

// TestIsBrowserCompatible_Nil 测试 nil 输入返回 false。
func TestIsBrowserCompatible_Nil(t *testing.T) {
	result := IsBrowserCompatible(nil)
	assert.False(t, result)
}

type symlinkDirEntry struct{}

func (symlinkDirEntry) Name() string               { return "video.srt" }
func (symlinkDirEntry) IsDir() bool                { return false }
func (symlinkDirEntry) Type() os.FileMode          { return os.ModeSymlink }
func (symlinkDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func TestSidecarFormatRejectsSymlinkDirEntry(t *testing.T) {
	_, ok := sidecarFormat(symlinkDirEntry{}, map[string]string{"srt": "srt"})
	assert.False(t, ok, "符号链接目录项不得被枚举为外挂字幕")
}

// TestFindSubtitleFiles 测试查找同名字幕文件。
func TestFindSubtitleFiles(t *testing.T) {
	tmp := t.TempDir()

	// 创建视频文件
	videoPath := filepath.Join(tmp, "video.mp4")
	require.NoError(t, os.WriteFile(videoPath, []byte("fake"), 0o644))

	// 创建匹配的字幕文件，允许语言等后缀并要求稳定排序
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "video.zh.vtt"), []byte("vtt"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "video.srt"), []byte("srt"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "video.ass"), []byte("ass"), 0o644))

	// 创建不匹配的字幕文件
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "other.srt"), []byte("other"), 0o644))

	// 创建不支持的格式
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "video.txt"), []byte("txt"), 0o644))

	subs, err := FindSubtitleFiles(videoPath)
	require.NoError(t, err)
	assert.Len(t, subs, 3)
	assert.Equal(t, "video.ass", filepath.Base(subs[0].Path))
	assert.Equal(t, "video.srt", filepath.Base(subs[1].Path))
	assert.Equal(t, "video.zh.vtt", filepath.Base(subs[2].Path))

	// 验证格式正确
	formatSet := make(map[string]bool)
	for _, sub := range subs {
		formatSet[sub.Format] = true
	}
	assert.True(t, formatSet["srt"], "应包含 srt 字幕")
	assert.True(t, formatSet["ass"], "应包含 ass 字幕")
}

// TestFindSubtitleFiles_NoMatch 测试目录中没有同名字幕时返回空列表。
func TestFindSubtitleFiles_NoMatch(t *testing.T) {
	tmp := t.TempDir()

	videoPath := filepath.Join(tmp, "video.mp4")
	require.NoError(t, os.WriteFile(videoPath, []byte("fake"), 0o644))

	// 只有不匹配的字幕
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "other.srt"), []byte("other"), 0o644))

	subs, err := FindSubtitleFiles(videoPath)
	require.NoError(t, err)
	assert.Len(t, subs, 0)
}

// TestFindSubtitleFiles_EmptyDir 测试空目录返回空列表。
func TestFindSubtitleFiles_EmptyDir(t *testing.T) {
	tmp := t.TempDir()

	videoPath := filepath.Join(tmp, "video.mp4")
	require.NoError(t, os.WriteFile(videoPath, []byte("fake"), 0o644))

	subs, err := FindSubtitleFiles(videoPath)
	require.NoError(t, err)
	assert.Len(t, subs, 0)
}

// TestEnsureDir 测试 EnsureDir 创建嵌套目录。
func TestEnsureDir(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "sub1", "sub2")

	err := EnsureDir(target)
	require.NoError(t, err)

	// 验证目录已创建
	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
