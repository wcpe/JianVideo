package player

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHLSSegmentWriter_CreatesDirectoryAndM3U8 验证写入器自动创建目录和初始 m3u8。
func TestHLSSegmentWriter_CreatesDirectoryAndM3U8(t *testing.T) {
	tDir := t.TempDir()

	w, err := NewHLSSegmentWriter(tDir, 42, "1080p")
	if err != nil {
		t.Fatalf("NewHLSSegmentWriter 失败: %v", err)
	}
	defer func() { _ = w.Close() }()

	// 子目录应已创建
	mediaDir := filepath.Join(tDir, "42")
	if _, err := os.Stat(mediaDir); os.IsNotExist(err) {
		t.Fatal("HLS 目录未被创建")
	}

	// m3u8 文件应已创建
	m3u8Path := filepath.Join(mediaDir, "1080p.m3u8")
	if _, err := os.Stat(m3u8Path); os.IsNotExist(err) {
		t.Fatal("1080p.m3u8 未被创建")
	}

	// 验证 m3u8 头部内容
	content, err := os.ReadFile(m3u8Path)
	if err != nil {
		t.Fatalf("读取 m3u8 失败: %v", err)
	}
	if !strings.Contains(string(content), "#EXTM3U") {
		t.Fatal("m3u8 缺少 #EXTM3U")
	}
	if !strings.Contains(string(content), "#EXT-X-VERSION:3") {
		t.Fatal("m3u8 缺少 #EXT-X-VERSION:3")
	}
	if !strings.Contains(string(content), "#EXT-X-TARGETDURATION:3") {
		t.Fatal("m3u8 缺少 #EXT-X-TARGETDURATION:3")
	}
	if !strings.Contains(string(content), "#EXT-X-MEDIA-SEQUENCE:0") {
		t.Fatal("m3u8 缺少 #EXT-X-MEDIA-SEQUENCE:0")
	}
}

// TestHLSSegmentWriter_WriteSegment 验证写入切片后 m3u8 正确更新。
func TestHLSSegmentWriter_WriteSegment(t *testing.T) {
	tDir := t.TempDir()

	w, err := NewHLSSegmentWriter(tDir, 1, "720p")
	if err != nil {
		t.Fatalf("NewHLSSegmentWriter 失败: %v", err)
	}
	defer func() { _ = w.Close() }()

	mediaDir := filepath.Join(tDir, "1")

	// 写入第一个切片
	if err := w.WriteSegment([]byte("fake ts data 0")); err != nil {
		t.Fatalf("写入切片 0 失败: %v", err)
	}

	// 验证切片文件存在
	seg0 := filepath.Join(mediaDir, "720p_segment_000.ts")
	if _, err := os.Stat(seg0); os.IsNotExist(err) {
		t.Fatal("720p_segment_000.ts 未被创建")
	}

	// 验证 m3u8 包含第一个切片
	content, _ := os.ReadFile(filepath.Join(mediaDir, "720p.m3u8"))
	if !strings.Contains(string(content), "#EXTINF:3.000,") {
		t.Fatalf("m3u8 缺少 EXTINF 标签, 内容: %s", string(content))
	}
	if !strings.Contains(string(content), "720p_segment_000.ts") {
		t.Fatal("m3u8 缺少 720p_segment_000.ts 引用")
	}

	// 写入第二个切片
	if err := w.WriteSegment([]byte("fake ts data 1")); err != nil {
		t.Fatalf("写入切片 1 失败: %v", err)
	}

	seg1 := filepath.Join(mediaDir, "720p_segment_001.ts")
	if _, err := os.Stat(seg1); os.IsNotExist(err) {
		t.Fatal("720p_segment_001.ts 未被创建")
	}

	// 验证 m3u8 包含两个切片，且没有 ENDLIST
	content, _ = os.ReadFile(filepath.Join(mediaDir, "720p.m3u8"))
	if !strings.Contains(string(content), "720p_segment_001.ts") {
		t.Fatal("m3u8 缺少 720p_segment_001.ts 引用")
	}
	if strings.Contains(string(content), "#EXT-X-ENDLIST") {
		t.Fatal("追播模式下不应包含 #EXT-X-ENDLIST")
	}
}

// TestHLSSegmentWriter_Close 验证 Close 写入 ENDLIST。
func TestHLSSegmentWriter_Close(t *testing.T) {
	tDir := t.TempDir()

	w, err := NewHLSSegmentWriter(tDir, 2, "480p")
	if err != nil {
		t.Fatalf("NewHLSSegmentWriter 失败: %v", err)
	}

	_ = w.WriteSegment([]byte("data"))
	if err := w.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	mediaDir := filepath.Join(tDir, "2")
	content, _ := os.ReadFile(filepath.Join(mediaDir, "480p.m3u8"))
	if !strings.Contains(string(content), "#EXT-X-ENDLIST") {
		t.Fatalf("Close 后 m3u8 应包含 ENDLIST, 内容: %s", string(content))
	}
}

// TestHLSManager_GetOrCreateWriter 验证管理器的创建和获取。
func TestHLSManager_GetOrCreateWriter(t *testing.T) {
	tDir := t.TempDir()
	mgr := NewHLSManager(tDir)

	w1, err := mgr.GetOrCreateWriter(100, "1080p")
	if err != nil {
		t.Fatalf("GetOrCreateWriter 失败: %v", err)
	}

	// 再次获取应返回同一个实例
	w2, err := mgr.GetOrCreateWriter(100, "1080p")
	if err != nil {
		t.Fatalf("第二次 GetOrCreateWriter 失败: %v", err)
	}
	if w1 != w2 {
		t.Fatal("同一 media_id+quality 应返回同一个 Writer 实例")
	}

	// 不同 quality 应返回不同实例
	w3, err := mgr.GetOrCreateWriter(100, "720p")
	if err != nil {
		t.Fatalf("GetOrCreateWriter(720p) 失败: %v", err)
	}
	if w1 == w3 {
		t.Fatal("不同 quality 应返回不同 Writer 实例")
	}

	// 不同 media_id 应返回不同实例
	w4, err := mgr.GetOrCreateWriter(200, "1080p")
	if err != nil {
		t.Fatalf("GetOrCreateWriter(200) 失败: %v", err)
	}
	if w1 == w4 {
		t.Fatal("不同 media_id 应返回不同 Writer 实例")
	}

	_ = w1.Close()
	_ = w3.Close()
	_ = w4.Close()
}

// TestHLSManager_GetM3U8 验证读取 m3u8。
func TestHLSManager_GetM3U8(t *testing.T) {
	tDir := t.TempDir()
	mgr := NewHLSManager(tDir)

	w, _ := mgr.GetOrCreateWriter(10, "480p")
	_ = w.WriteSegment([]byte("data"))

	content, err := mgr.GetM3U8(10, "480p")
	if err != nil {
		t.Fatalf("GetM3U8 失败: %v", err)
	}
	if !strings.Contains(content, "#EXTM3U") {
		t.Fatalf("m3u8 内容异常: %s", content)
	}

	// 不存在的 media_id 应返回错误
	_, err = mgr.GetM3U8(999, "480p")
	if err == nil {
		t.Fatal("不存在的 media_id 应返回错误")
	}

	// 不存在的 quality 应返回错误
	_, err = mgr.GetM3U8(10, "1080p")
	if err == nil {
		t.Fatal("不存在的 quality 应返回错误")
	}

	_ = w.Close()
}

// TestHLSManager_GetSegment 验证读取切片文件。
func TestHLSManager_GetSegment(t *testing.T) {
	tDir := t.TempDir()
	mgr := NewHLSManager(tDir)

	w, _ := mgr.GetOrCreateWriter(20, "720p")
	testData := []byte("hello ts segment")
	_ = w.WriteSegment(testData)

	data, err := mgr.GetSegment(20, "720p", "720p_segment_000.ts")
	if err != nil {
		t.Fatalf("GetSegment 失败: %v", err)
	}
	if string(data) != string(testData) {
		t.Fatalf("切片数据不匹配, 期望 %q, 实际 %q", testData, data)
	}

	// 不存在的切片应返回错误
	_, err = mgr.GetSegment(20, "720p", "720p_segment_999.ts")
	if err == nil {
		t.Fatal("不存在的切片应返回错误")
	}

	// 不存在的 media_id 应返回错误
	_, err = mgr.GetSegment(999, "720p", "720p_segment_000.ts")
	if err == nil {
		t.Fatal("不存在的 media_id 应返回错误")
	}

	_ = w.Close()
}

// TestHLSManager_RemoveWriter 验证移除清理。
func TestHLSManager_RemoveWriter(t *testing.T) {
	tDir := t.TempDir()
	mgr := NewHLSManager(tDir)

	w, _ := mgr.GetOrCreateWriter(30, "1080p")
	_ = w.WriteSegment([]byte("data"))
	_ = w.Close()

	// 移除后应无法获取 m3u8
	mgr.RemoveWriter(30, "1080p")
	_, err := mgr.GetM3U8(30, "1080p")
	if err == nil {
		t.Fatal("移除后 GetM3U8 应返回错误")
	}
}

// TestHLSManager_RemoveAllWriters 验证移除全部码率 writer。
func TestHLSManager_RemoveAllWriters(t *testing.T) {
	tDir := t.TempDir()
	mgr := NewHLSManager(tDir)

	w1, _ := mgr.GetOrCreateWriter(40, "1080p")
	w2, _ := mgr.GetOrCreateWriter(40, "720p")
	_ = w1.WriteSegment([]byte("data"))
	_ = w2.WriteSegment([]byte("data"))
	_ = w1.Close()
	_ = w2.Close()

	mgr.RemoveAllWriters(40)

	if mgr.HasSession(40) {
		t.Fatal("移除后不应再有活跃会话")
	}
}

// TestHLSManager_MasterM3U8 验证 master playlist 的保存和读取。
func TestHLSManager_MasterM3U8(t *testing.T) {
	tDir := t.TempDir()
	mgr := NewHLSManager(tDir)

	qualities := []QualityInfo{
		{Name: "1080p", Width: 1920, Height: 1080, Bandwidth: 5000000},
		{Name: "720p", Width: 1280, Height: 720, Bandwidth: 2500000},
		{Name: "480p", Width: 854, Height: 480, Bandwidth: 1000000},
	}

	content := GenerateMasterM3U8(qualities)
	if err := mgr.SaveMasterM3U8(50, content); err != nil {
		t.Fatalf("SaveMasterM3U8 失败: %v", err)
	}

	got, err := mgr.GetMasterM3U8(50)
	if err != nil {
		t.Fatalf("GetMasterM3U8 失败: %v", err)
	}
	if got != content {
		t.Fatalf("master.m3u8 内容不匹配")
	}

	// 不存在的 media_id 应返回错误
	_, err = mgr.GetMasterM3U8(999)
	if err == nil {
		t.Fatal("不存在的 media_id 应返回错误")
	}
}

// TestExtractQualityFromSegment 验证切片文件名解析。
func TestExtractQualityFromSegment(t *testing.T) {
	tests := []struct {
		segment string
		want    string
	}{
		{"1080p_segment_000.ts", "1080p"},
		{"720p_segment_001.ts", "720p"},
		{"480p_segment_002.ts", "480p"},
		{"segment_000.ts", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := ExtractQualityFromSegment(tt.segment)
		if got != tt.want {
			t.Errorf("ExtractQualityFromSegment(%q) = %q, want %q", tt.segment, got, tt.want)
		}
	}
}
