// Package playback 管理媒体播放会话与边下边播、Seek 等播放控制能力。
package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/smb"
)

// ProgressInfo 播放进度信息。
type ProgressInfo struct {
	CurrentPosition float64    `json:"current_position"`
	Duration        float64    `json:"duration"`
	FileSize        int64      `json:"file_size"`
	BufferedRanges  [][2]int64 `json:"buffered_ranges"`
}

// SeekRequest Seek 请求。
type SeekRequest struct {
	Position float64 `json:"position" binding:"required"`
}

// SeekResponse Seek 响应。
type SeekResponse struct {
	Status   string  `json:"status"`
	Position float64 `json:"position"`
}

// BufferReport 缓冲区间上报。
type BufferReport struct {
	CurrentPosition float64    `json:"current_position"`
	FileSize        int64      `json:"file_size"`
	BufferedRanges  [][2]int64 `json:"buffered_ranges"`
}

// Service 播放流控服务。
type Service struct {
	sessions map[int64]*models.PlaybackSession // key: media_id
	mu       sync.RWMutex
	stopCh   chan struct{}
}

// NewService 创建播放服务。
func NewService() *Service {
	s := &Service{
		sessions: make(map[int64]*models.PlaybackSession),
		stopCh:   make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// Stop 停止服务，清理后台 goroutine。
func (s *Service) Stop() {
	close(s.stopCh)
}

// ActiveSessions 返回当前活跃会话数（FR-119）：只读计数，供系统指标采样器经 provider 取转码并发。
func (s *Service) ActiveSessions() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// cleanupLoop 定期清理超过 24 小时未更新的会话。
func (s *Service) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.cleanupExpiredSessions()
		case <-s.stopCh:
			return
		}
	}
}

// cleanupExpiredSessions 删除超过 24 小时未更新的会话。
func (s *Service) cleanupExpiredSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	threshold := time.Now().Add(-24 * time.Hour)
	for id, sess := range s.sessions {
		if sess.UpdatedAt.Before(threshold) {
			delete(s.sessions, id)
		}
	}
}

// StreamFile 处理带 Range 请求的文件流。
func (s *Service) StreamFile(w http.ResponseWriter, r *http.Request, mediaID int64, filePath string, fileSize int64, duration float64) {
	// 打开文件
	var f io.ReadSeeker
	var err error

	if strings.HasPrefix(filePath, "smb://") {
		f, err = openSMBFile(filePath)
	} else {
		f, err = os.Open(filePath)
	}

	if err != nil {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}
	if closer, ok := f.(io.Closer); ok {
		// 资源清理，关闭错误可忽略
		defer func() { _ = closer.Close() }()
	}

	// 更新或创建播放会话
	s.mu.Lock()
	if _, exists := s.sessions[mediaID]; !exists {
		s.sessions[mediaID] = &models.PlaybackSession{
			MediaID:  mediaID,
			ClientIP: getClientIP(r),
			Duration: duration,
			FileSize: fileSize,
		}
	}
	s.mu.Unlock()

	// 记录访问日志
	log.Printf("[INFO] 播放请求: mediaID=%d", mediaID)

	// 使用 http.ServeContent 自动处理 Range 请求
	http.ServeContent(w, r, filepath.Base(filePath), time.Now(), f)
}

// openSMBFile 打开 SMB 路径对应的文件。
// 路径格式: smb://host/share/path/to/file.mp4
func openSMBFile(smbPath string) (*smbReadSeeker, error) {
	// 解析 smb://host/share/path
	path := strings.TrimPrefix(smbPath, "smb://")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("SMB 路径格式错误: %s", smbPath)
	}

	host := parts[0]
	share := parts[1]
	filePath := parts[2]

	// 加载凭据
	store := smb.NewCredentialStore("data")
	masterPwd, err := smb.MasterPassword()
	if err != nil {
		return nil, fmt.Errorf("加载 SMB 凭据失败: %w", err)
	}
	creds, err := store.Load(masterPwd)
	if err != nil {
		return nil, fmt.Errorf("加载 SMB 凭据失败: %w", err)
	}
	if creds == nil {
		return nil, fmt.Errorf("未找到 SMB 凭据，请先通过 POST /api/smb/credentials 配置凭据")
	}
	creds.Host = host
	creds.Share = share

	client := smb.NewClient(*creds)
	shareFS, err := client.EnsureConnected(context.Background())
	if err != nil {
		return nil, fmt.Errorf("连接 SMB 失败: %w", err)
	}

	smbFile, err := shareFS.Open(filePath)
	if err != nil {
		_ = client.Disconnect()
		return nil, fmt.Errorf("打开 SMB 文件失败: %w", err)
	}

	// *smb2.File 实现了 io.ReadSeeker
	rs := io.ReadSeeker(smbFile)

	return &smbReadSeeker{
		ReadSeeker: rs,
		client:     client,
	}, nil
}

// smbReadSeeker 包装 SMB 文件，实现 io.ReadSeeker 并管理连接生命周期。
type smbReadSeeker struct {
	io.ReadSeeker
	client *smb.Client
}

// Close 只关闭底层文件读取器，不断开 SMB 会话。
// SMB 连接的生命周期由 openSMBFile 中的 client 变量管理。
func (s *smbReadSeeker) Close() error {
	if closer, ok := s.ReadSeeker.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// 确保 smbReadSeeker 实现 io.ReadSeeker 和 io.Closer。
var _ io.ReadSeeker = (*smbReadSeeker)(nil)
var _ io.Closer = (*smbReadSeeker)(nil)

// HandleSeek 处理 Seek 请求。
func (s *Service) HandleSeek(mediaID int64, position float64) (*SeekResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, exists := s.sessions[mediaID]
	if !exists {
		// 创建新会话
		sess = &models.PlaybackSession{
			MediaID:  mediaID,
			Duration: 0,
			FileSize: 0,
		}
		s.sessions[mediaID] = sess
	}

	sess.CurrentPosition = position
	sess.UpdatedAt = time.Now()

	return &SeekResponse{
		Status:   "ok",
		Position: position,
	}, nil
}

// HandleBufferReport 处理缓冲区间上报。
func (s *Service) HandleBufferReport(mediaID int64, report BufferReport) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, exists := s.sessions[mediaID]
	if !exists {
		sess = &models.PlaybackSession{
			MediaID:  mediaID,
			FileSize: report.FileSize,
		}
		s.sessions[mediaID] = sess
	}

	sess.CurrentPosition = report.CurrentPosition
	if report.FileSize > 0 {
		sess.FileSize = report.FileSize
	}
	sess.UpdatedAt = time.Now()

	// 序列化缓冲区间
	if data, err := json.Marshal(report.BufferedRanges); err == nil {
		sess.BufferedRanges = string(data)
	} else {
		log.Printf("[WARN] 序列化缓冲区间失败: mediaID=%d, err=%v", mediaID, err)
	}
}

// GetProgress 获取播放进度信息。
func (s *Service) GetProgress(mediaID int64) (*ProgressInfo, error) {
	s.mu.RLock()
	sess, exists := s.sessions[mediaID]
	if !exists {
		s.mu.RUnlock()
		return &ProgressInfo{}, nil
	}
	// 在锁内复制所需字段，避免解锁后并发读写
	bufferedRanges := sess.BufferedRanges
	currentPosition := sess.CurrentPosition
	duration := sess.Duration
	fileSize := sess.FileSize
	s.mu.RUnlock()

	var ranges [][2]int64
	if bufferedRanges != "" {
		_ = json.Unmarshal([]byte(bufferedRanges), &ranges)
	}

	return &ProgressInfo{
		CurrentPosition: currentPosition,
		Duration:        duration,
		FileSize:        fileSize,
		BufferedRanges:  ranges,
	}, nil
}

// GetOrCreateSession 获取或创建播放会话。
func (s *Service) GetOrCreateSession(mediaID int64, duration float64, fileSize int64) *models.PlaybackSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, exists := s.sessions[mediaID]
	if !exists {
		sess = &models.PlaybackSession{
			MediaID:  mediaID,
			Duration: duration,
			FileSize: fileSize,
			ClientIP: "",
		}
		s.sessions[mediaID] = sess
	}
	return sess
}

// RecordNegotiation 记录本次播放协商出的实际编码与播放路径到会话（FR-53）。
// 会话不存在则创建；仅写内存会话（与现状一致，不持久化），刷新 UpdatedAt。
func (s *Service) RecordNegotiation(mediaID int64, codec, outputPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, exists := s.sessions[mediaID]
	if !exists {
		sess = &models.PlaybackSession{MediaID: mediaID}
		s.sessions[mediaID] = sess
	}
	sess.TargetCodec = codec
	sess.OutputPath = outputPath
	sess.UpdatedAt = time.Now()
}

func getClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
