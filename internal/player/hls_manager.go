package player

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// HLSManager 管理所有媒体文件的 HLS 会话，每个媒体可包含多个码率 writer。
type HLSManager struct {
	mu      sync.Mutex
	baseDir string
	writers map[int64]map[string]*HLSSegmentWriter // mediaID -> quality -> writer
}

// NewHLSManager 创建 HLS 会话管理器。
func NewHLSManager(baseDir string) *HLSManager {
	return &HLSManager{
		baseDir: baseDir,
		writers: make(map[int64]map[string]*HLSSegmentWriter),
	}
}

// GetOrCreateWriter 获取或创建指定媒体文件和码率的切片写入器。
func (m *HLSManager) GetOrCreateWriter(mediaID int64, quality string) (*HLSSegmentWriter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mediaWriters, ok := m.writers[mediaID]
	if !ok {
		mediaWriters = make(map[string]*HLSSegmentWriter)
		m.writers[mediaID] = mediaWriters
	}

	if w, ok := mediaWriters[quality]; ok {
		return w, nil
	}

	w, err := NewHLSSegmentWriter(m.baseDir, mediaID, quality)
	if err != nil {
		return nil, fmt.Errorf("创建 Writer 失败: %w", err)
	}
	mediaWriters[quality] = w
	return w, nil
}

// RemoveWriter 移除并关闭指定媒体文件和码率的切片写入器。
// 同时清理该码率下的 m3u8 + 切片文件，让追播会话彻底结束。
func (m *HLSManager) RemoveWriter(mediaID int64, quality string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if mediaWriters, ok := m.writers[mediaID]; ok {
		if w, ok := mediaWriters[quality]; ok {
			_ = w.Close()
			delete(mediaWriters, quality)
		}
		// 如果该媒体的所有 writer 都移除了，清理 map 并删掉 m3u8 + 切片
		if len(mediaWriters) == 0 {
			delete(m.writers, mediaID)
			mediaDir := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID))
			_ = os.RemoveAll(mediaDir)
		}
	}
}

// RemoveAllWriters 移除并关闭指定媒体文件的所有切片写入器。
func (m *HLSManager) RemoveAllWriters(mediaID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if mediaWriters, ok := m.writers[mediaID]; ok {
		for _, w := range mediaWriters {
			_ = w.Close()
		}
		delete(m.writers, mediaID)
	}
}

// GetM3U8 读取指定媒体文件和码率的 m3u8 索引内容。
// 优先从内存 writer 读取（追播模式），否则直接从文件系统读取（预切片模式）。
// 追播模式下若 writer 已被移除（RemoveWriter），即便文件还在也返回错误，
// 避免前端拿到过期的 ENDLIST 索引。
func (m *HLSManager) GetM3U8(mediaID int64, quality string) (string, error) {
	m.mu.Lock()
	mediaWriters, hasMedia := m.writers[mediaID]
	_, hasQuality := mediaWriters[quality]
	m.mu.Unlock()

	// 追播模式：内存里有 writer 就走 in-memory 路径（writer 维护 m3u8 文本）
	if hasMedia && hasQuality {
		m3u8Path := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID), quality+".m3u8")
		data, err := os.ReadFile(m3u8Path)
		if err != nil {
			return "", fmt.Errorf("读取 m3u8 失败: %w", err)
		}
		return string(data), nil
	}

	// 预切片模式：直接读文件系统（ffmpeg 写出的 m3u8）
	m3u8Path := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID), quality+".m3u8")
	if _, err := os.Stat(m3u8Path); err == nil {
		data, err := os.ReadFile(m3u8Path)
		if err != nil {
			return "", fmt.Errorf("读取 m3u8 失败: %w", err)
		}
		return string(data), nil
	}

	if hasMedia {
		return "", fmt.Errorf("媒体 %d 没有码率 %s 的 HLS 会话", mediaID, quality)
	}
	return "", fmt.Errorf("媒体 %d 没有码率 %s 的 HLS 产物", mediaID, quality)
}

// GetSegment 读取指定媒体文件和码率的切片内容。
// 追播模式走内存 writer；预切片模式直接读文件系统。
// 追播模式下若 writer 已被移除（RemoveWriter），即便文件还在也返回错误。
func (m *HLSManager) GetSegment(mediaID int64, quality string, name string) ([]byte, error) {
	m.mu.Lock()
	mediaWriters, hasMedia := m.writers[mediaID]
	_, hasQuality := mediaWriters[quality]
	m.mu.Unlock()

	segPath := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID), name)

	// 追播模式：必须内存里还有对应 writer 才允许读
	if hasMedia && hasQuality {
		return os.ReadFile(segPath)
	}

	// 预切片模式：直接从文件系统读
	if _, err := os.Stat(segPath); err == nil {
		return os.ReadFile(segPath)
	}

	if hasMedia {
		return nil, fmt.Errorf("媒体 %d 没有码率 %s 的活跃 HLS 会话", mediaID, quality)
	}
	return nil, fmt.Errorf("切片 %s 不存在", name)
}

// GetMasterM3U8 读取指定媒体文件的 master.m3u8 索引内容。
func (m *HLSManager) GetMasterM3U8(mediaID int64) (string, error) {
	masterPath := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID), "master.m3u8")
	data, err := os.ReadFile(masterPath)
	if err != nil {
		return "", fmt.Errorf("读取 master.m3u8 失败: %w", err)
	}
	return string(data), nil
}

// SaveMasterM3U8 保存指定媒体文件的 master.m3u8 内容。
func (m *HLSManager) SaveMasterM3U8(mediaID int64, content string) error {
	mediaDir := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID))
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	masterPath := filepath.Join(mediaDir, "master.m3u8")
	return os.WriteFile(masterPath, []byte(content), 0o644)
}

// HasSession 检查指定媒体文件是否有活跃的 HLS 会话。
func (m *HLSManager) HasSession(mediaID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.writers[mediaID]
	return ok
}
