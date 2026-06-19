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
func (m *HLSManager) RemoveWriter(mediaID int64, quality string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if mediaWriters, ok := m.writers[mediaID]; ok {
		if w, ok := mediaWriters[quality]; ok {
			_ = w.Close()
			delete(mediaWriters, quality)
		}
		// 如果该媒体的所有 writer 都移除了，清理 map
		if len(mediaWriters) == 0 {
			delete(m.writers, mediaID)
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
func (m *HLSManager) GetM3U8(mediaID int64, quality string) (string, error) {
	m.mu.Lock()
	mediaWriters, ok := m.writers[mediaID]
	if !ok {
		m.mu.Unlock()
		return "", fmt.Errorf("媒体 %d 没有活跃的 HLS 会话", mediaID)
	}
	if _, ok := mediaWriters[quality]; !ok {
		m.mu.Unlock()
		return "", fmt.Errorf("媒体 %d 没有码率 %s 的 HLS 会话", mediaID, quality)
	}
	m3u8Path := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID), quality+".m3u8")
	m.mu.Unlock()

	data, err := os.ReadFile(m3u8Path)
	if err != nil {
		return "", fmt.Errorf("读取 m3u8 失败: %w", err)
	}
	return string(data), nil
}

// GetSegment 读取指定媒体文件和码率的切片内容。
func (m *HLSManager) GetSegment(mediaID int64, quality string, name string) ([]byte, error) {
	m.mu.Lock()
	mediaWriters, ok := m.writers[mediaID]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("媒体 %d 没有活跃的 HLS 会话", mediaID)
	}
	if _, ok := mediaWriters[quality]; !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("媒体 %d 没有码率 %s 的 HLS 会话", mediaID, quality)
	}
	segPath := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID), name)
	m.mu.Unlock()

	data, err := os.ReadFile(segPath)
	if err != nil {
		return nil, fmt.Errorf("读取切片 %s 失败: %w", name, err)
	}
	return data, nil
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
