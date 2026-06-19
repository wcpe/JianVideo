package player

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// m3u8Header 是 m3u8 文件的固定头部（追播模式，不写 ENDLIST）。
const m3u8Header = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:3\n#EXT-X-MEDIA-SEQUENCE:0\n"

// HLSSegmentWriter 管理单个媒体文件单个码率的 HLS 切片写入。
type HLSSegmentWriter struct {
	mu       sync.Mutex
	mediaDir string
	mediaID  int64
	quality  string
	seq      int
	f       *os.File
	closed   bool
}

// NewHLSSegmentWriter 创建 HLS 切片写入器，自动创建目录和 m3u8 文件。
// quality 为码率档位名（如 "1080p"、"720p"、"480p"）。
func NewHLSSegmentWriter(baseDir string, mediaID int64, quality string) (*HLSSegmentWriter, error) {
	mediaDir := filepath.Join(baseDir, fmt.Sprintf("%d", mediaID))
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 HLS 目录失败: %w", err)
	}

	m3u8Name := fmt.Sprintf("%s.m3u8", quality)
	m3u8Path := filepath.Join(mediaDir, m3u8Name)
	f, err := os.Create(m3u8Path)
	if err != nil {
		return nil, fmt.Errorf("创建 m3u8 文件失败: %w", err)
	}

	if _, err := f.WriteString(m3u8Header); err != nil {
		f.Close()
		return nil, fmt.Errorf("写入 m3u8 头部失败: %w", err)
	}

	return &HLSSegmentWriter{
		mediaDir: mediaDir,
		mediaID:  mediaID,
		quality:  quality,
		seq:      0,
		f:        f,
	}, nil
}

// WriteSegment 写入一个 TS 切片并追加 m3u8 记录（追播模式）。
func (w *HLSSegmentWriter) WriteSegment(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	segName := fmt.Sprintf("%s_segment_%03d.ts", w.quality, w.seq)
	segPath := filepath.Join(w.mediaDir, segName)

	// 写入切片文件
	if err := os.WriteFile(segPath, data, 0o644); err != nil {
		return fmt.Errorf("写入切片 %s 失败: %w", segName, err)
	}

	// 追加 m3u8 记录
	line := fmt.Sprintf("#EXTINF:3.000,\n%s\n", segName)
	if _, err := w.f.WriteString(line); err != nil {
		return fmt.Errorf("追加 m3u8 记录失败: %w", err)
	}
	w.seq++
	return nil
}

// Close 关闭写入器，追加 EXT-X-ENDLIST 标记。
func (w *HLSSegmentWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true

	// 写入 ENDLIST 标记，即使失败也继续关闭文件句柄以防泄露
	_, _ = w.f.WriteString("#EXT-X-ENDLIST\n")
	if err := w.f.Sync(); err != nil {
		_ = w.f.Close()
		return fmt.Errorf("同步 m3u8 失败: %w", err)
	}
	return w.f.Close()
}
