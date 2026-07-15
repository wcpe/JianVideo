package player

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrInvalidHLSPath 表示 HLS 相对路径无效或目标不是普通文件。
var ErrInvalidHLSPath = errors.New("非法 HLS 路径")

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

	relPath := fmt.Sprintf("%d/%s.m3u8", mediaID, quality)
	// 追播模式：内存里有 writer 就走受限文件句柄读取。
	if hasMedia && hasQuality {
		data, err := readHLSFile(m.baseDir, relPath)
		if err != nil {
			return "", fmt.Errorf("读取 m3u8 失败: %w", err)
		}
		return string(data), nil
	}

	// 预切片模式：直接从受限文件句柄读取 ffmpeg 产物。
	data, pathErr := readHLSFile(m.baseDir, relPath)
	if pathErr == nil {
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

	relPath := fmt.Sprintf("%d/%s", mediaID, name)
	// 追播模式：必须内存里还有对应 writer 才允许读。
	if hasMedia && hasQuality {
		return readHLSFile(m.baseDir, relPath)
	}

	// 预切片模式：直接从受限文件句柄读取。
	if data, err := readHLSFile(m.baseDir, relPath); err == nil {
		return data, nil
	}

	if hasMedia {
		return nil, fmt.Errorf("媒体 %d 没有码率 %s 的活跃 HLS 会话", mediaID, quality)
	}
	return nil, fmt.Errorf("切片 %s 不存在", name)
}

// GetMasterM3U8 读取指定媒体文件的 master.m3u8 索引内容。
func (m *HLSManager) GetMasterM3U8(mediaID int64) (string, error) {
	data, err := readHLSFile(m.baseDir, fmt.Sprintf("%d/master.m3u8", mediaID))
	if err != nil {
		return "", fmt.Errorf("读取 master.m3u8 失败: %w", err)
	}
	return string(data), nil
}

// SaveMasterM3U8 保存指定媒体文件的 master.m3u8 内容。
func (m *HLSManager) SaveMasterM3U8(mediaID int64, content string) error {
	mediaDir := filepath.Join(m.baseDir, fmt.Sprintf("%d", mediaID))
	if err := os.MkdirAll(mediaDir, 0o750); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	masterPath := filepath.Join(mediaDir, "master.m3u8")
	return os.WriteFile(masterPath, []byte(content), 0o644)
}

// OpenHLSFile 在根目录约束内打开 HLS 普通文件，并返回同一句柄的文件信息。
func OpenHLSFile(baseDir, relPath string) (*os.File, os.FileInfo, error) {
	if err := validateHLSRelativePath(relPath); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenInRoot(filepath.Clean(baseDir), filepath.FromSlash(relPath))
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, ErrInvalidHLSPath
	}
	return file, info, nil
}

func readHLSFile(baseDir, relPath string) ([]byte, error) {
	file, _, err := OpenHLSFile(baseDir, relPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(file)
}

func validateHLSRelativePath(relPath string) error {
	if relPath == "" || filepath.IsAbs(relPath) || strings.Contains(relPath, "\\") {
		return ErrInvalidHLSPath
	}
	for _, part := range strings.Split(filepath.ToSlash(relPath), "/") {
		if part == "" || part == "." || part == ".." {
			return ErrInvalidHLSPath
		}
	}
	return nil
}

// ResolveContainedPath 解析 HLS 相对路径，并拒绝绝对路径、平台分隔符混用、路径穿越与越界符号链接。
func ResolveContainedPath(baseDir, relPath string) (string, error) {
	if err := validateHLSRelativePath(relPath); err != nil {
		return "", err
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("解析 HLS 根目录失败: %w", err)
	}
	candidate := filepath.Join(baseAbs, filepath.FromSlash(filepath.ToSlash(relPath)))
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("解析 HLS 文件路径失败: %w", err)
	}
	if err := ensureContained(baseAbs, candidateAbs); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(candidateAbs)
	if err != nil {
		return "", err
	}
	baseResolved, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return "", err
	}
	if err := ensureContained(baseResolved, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func ensureContained(baseDir, candidate string) error {
	rel, err := filepath.Rel(baseDir, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("HLS 路径越界")
	}
	return nil
}

// HasSession 检查指定媒体文件是否有活跃的 HLS 会话。
func (m *HLSManager) HasSession(mediaID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.writers[mediaID]
	return ok
}
