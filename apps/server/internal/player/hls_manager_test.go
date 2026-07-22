package player

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHLSManager(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)
	require.NotNil(t, mgr)
}

func TestGetOrCreateWriter_New(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	w1, err := mgr.GetOrCreateWriter(1, "1080p")
	require.NoError(t, err)
	require.NotNil(t, w1)
	t.Cleanup(func() { _ = w1.Close() })

	w2, err := mgr.GetOrCreateWriter(1, "1080p")
	require.NoError(t, err)
	assert.Equal(t, w1, w2)
}

func TestGetOrCreateWriter_MultipleQualities(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	w1080, err := mgr.GetOrCreateWriter(1, "1080p")
	require.NoError(t, err)
	require.NotNil(t, w1080)
	t.Cleanup(func() { _ = w1080.Close() })

	w720, err := mgr.GetOrCreateWriter(1, "720p")
	require.NoError(t, err)
	t.Cleanup(func() { _ = w720.Close() })
	assert.NotEqual(t, w1080, w720)
}

func TestGetM3U8(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	w, err := mgr.GetOrCreateWriter(1, "1080p")
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	content, err := mgr.GetM3U8(1, "1080p")
	require.NoError(t, err)
	assert.Contains(t, content, "#EXTM3U")
}

func TestGetM3U8_NoSession(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	_, err := mgr.GetM3U8(999, "1080p")
	assert.Error(t, err)
}

func TestGetSegment(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	w, err := mgr.GetOrCreateWriter(1, "1080p")
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	err = w.WriteSegment([]byte("test data"))
	require.NoError(t, err)

	data, err := mgr.GetSegment(1, "1080p", "1080p_segment_000.ts")
	require.NoError(t, err)
	assert.Equal(t, []byte("test data"), data)
}

func TestGetSegment_RejectsTraversalAndAbsolutePaths(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewHLSManager(baseDir)
	outside := filepath.Join(baseDir, "outside.ts")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("创建越界文件失败: %v", err)
	}
	if data, err := mgr.GetSegment(1, "1080p", filepath.Join("..", "outside.ts")); err == nil {
		t.Fatalf("不得通过 .. 读取越界文件，读到 %q", data)
	}
	if data, err := mgr.GetSegment(1, "1080p", outside); err == nil {
		t.Fatalf("不得通过绝对路径读取文件，读到 %q", data)
	}
}

func TestGetSegment_RejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, "hls")
	mediaDir := filepath.Join(baseDir, "1")
	if err := os.MkdirAll(mediaDir, 0o750); err != nil {
		t.Fatalf("创建媒体目录失败: %v", err)
	}
	secret := filepath.Join(root, "secret.ts")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatalf("创建越界文件失败: %v", err)
	}
	link := filepath.Join(mediaDir, "link.ts")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("当前环境无法创建符号链接: %v", err)
	}
	mgr := NewHLSManager(baseDir)
	if data, err := mgr.GetSegment(1, "1080p", "link.ts"); err == nil {
		t.Fatalf("不得通过越界符号链接读取文件，读到 %q", data)
	}
}

func TestOpenHLSFileRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	mediaDir := filepath.Join(root, "1")
	if err := os.MkdirAll(mediaDir, 0o750); err != nil {
		t.Fatalf("创建 HLS 目录失败: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "secret.ts")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("创建越界文件失败: %v", err)
	}
	link := filepath.Join(mediaDir, "link.ts")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境无法创建符号链接: %v", err)
	}
	if file, _, err := OpenHLSFile(root, "1/link.ts"); err == nil {
		_ = file.Close()
		t.Fatal("受限打开不得跟随越界符号链接")
	}
}

func TestOpenHLSFileKeepsOpenedHandleAfterPathReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "1", "segment.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("创建 HLS 目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatalf("写入原始 HLS 文件失败: %v", err)
	}
	file, _, err := OpenHLSFile(root, "1/segment.ts")
	if err != nil {
		t.Fatalf("打开 HLS 文件失败: %v", err)
	}
	defer func() { _ = file.Close() }()
	if err := os.Rename(path, path+".old"); err != nil {
		t.Skipf("当前环境无法替换已打开文件: %v", err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatalf("写入替换 HLS 文件失败: %v", err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("读取已打开 HLS 句柄失败: %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("已打开句柄被路径替换影响: %q", data)
	}
}

func TestGetSegment_NoSession(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	_, err := mgr.GetSegment(999, "1080p", "test.ts")
	assert.Error(t, err)
}

func TestSaveAndGetMasterM3U8(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	content := "#EXTM3U\n#EXT-X-VERSION:3\n"
	err := mgr.SaveMasterM3U8(1, content)
	require.NoError(t, err)

	got, err := mgr.GetMasterM3U8(1)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestGetMasterM3U8_NoSession(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	_, err := mgr.GetMasterM3U8(999)
	assert.Error(t, err)
}

func TestHasSession(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	assert.False(t, mgr.HasSession(1))

	w, err := mgr.GetOrCreateWriter(1, "1080p")
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	assert.True(t, mgr.HasSession(1))
}

func TestRemoveWriter(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	_, err := mgr.GetOrCreateWriter(1, "1080p")
	require.NoError(t, err)

	mgr.RemoveWriter(1, "1080p")
	assert.False(t, mgr.HasSession(1))
}

func TestRemoveAllWriters(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	_, err := mgr.GetOrCreateWriter(1, "1080p")
	require.NoError(t, err)
	_, err = mgr.GetOrCreateWriter(1, "720p")
	require.NoError(t, err)

	mgr.RemoveAllWriters(1)
	assert.False(t, mgr.HasSession(1))
}

func TestRemoveWriter_NotExist(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	// 不存在的 writer 不应 panic
	mgr.RemoveWriter(999, "nonexist")
}

func TestWriteSegment_VerifyM3U8(t *testing.T) {
	dir := t.TempDir()
	mgr := NewHLSManager(dir)

	w, err := mgr.GetOrCreateWriter(1, "1080p")
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	err = w.WriteSegment([]byte("segment data"))
	require.NoError(t, err)

	content, err := mgr.GetM3U8(1, "1080p")
	require.NoError(t, err)
	assert.Contains(t, content, "#EXTINF:3.000,")
	assert.Contains(t, content, "1080p_segment_000.ts")
}
