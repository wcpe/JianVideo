package player

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// HLSManager 管理所有媒体文件的 HLS 会话。
type HLSManager struct {
	mu      sync.Mutex
	baseDir string
	writers map[int64]*HLSSegmentWriter
}

// NewHLSManager 创建 HLS 会话管理器。
func NewHLSManager(baseDir string) *HLSManager {
	return &HLSManager{
		baseDir: baseDir,
		writers: make(map[int64]*HLSSegmentWriter),
	}
}

// GetOrCreateWriter 获取或创建指定媒体文件的切片写入器。
func (m *HLSManager) GetOrCreateWriter(mediaID int64) (*HLSSegmentWriter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if w, ok := m.writers[mediaID]; ok {
		return w, nil
	}

	w, err := NewHLSSegmentWriter(m.baseDir, mediaID)
	if err != nil {
		return nil, fmt.Errorf("创建 Writer 失败: %w", err)
	}
	m.writers[mediaID] = w
	return w, nil
}

// RemoveWriter 移除并关闭指定媒体文件的切片写入器。
func (m *HLSManager) RemoveWriter(mediaID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if w, ok := m.writers[mediaID]; ok {
		_ = w.Close()
		delete(m.writers, mediaID)
	}
}

// GetM3U8 读取指定媒体文件的 m3u8 索引内容。
func (m *HLSManager) GetM3U8(mediaID int64) (string, error) {
	m.mu.Lock()
	if _, ok := m.writers[mediaID]; !ok {
		m.mu.Unlock()
		return "", fmt.Errorf("媒体 %d 没有活跃的 HLS 会话", mediaID)
	}
	m3u8Path := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID), "index.m3u8")
	m.mu.Unlock()

	data, err := os.ReadFile(m3u8Path)
	if err != nil {
		return "", fmt.Errorf("读取 m3u8 失败: %w", err)
	}
	return string(data), nil
}

// GetSegment 读取指定媒体文件的切片内容。
func (m *HLSManager) GetSegment(mediaID int64, name string) ([]byte, error) {
	m.mu.Lock()
	if _, ok := m.writers[mediaID]; !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("媒体 %d 没有活跃的 HLS 会话", mediaID)
	}
	segPath := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID), name)
	m.mu.Unlock()

	data, err := os.ReadFile(segPath)
	if err != nil {
		return nil, fmt.Errorf("读取切片 %s 失败: %w", name, err)
	}
	return data, nil
}
