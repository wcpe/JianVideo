package player

import (
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
