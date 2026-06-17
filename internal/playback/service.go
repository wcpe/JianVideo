package playback

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"jianvideo/internal/db/models"
)

// ProgressInfo 播放进度信息。
type ProgressInfo struct {
	CurrentPosition float64           `json:"current_position"`
	Duration       float64           `json:"duration"`
	FileSize        int64             `json:"file_size"`
	BufferedRanges  [][2]int64        `json:"buffered_ranges"`
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
}

// NewService 创建播放服务。
func NewService() *Service {
	return &Service{
		sessions: make(map[int64]*models.PlaybackSession),
	}
}

// StreamFile 处理带 Range 请求的文件流。
func (s *Service) StreamFile(w http.ResponseWriter, r *http.Request, mediaID int64, filePath string, fileSize int64, duration float64) {
	// 打开文件
	f, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}
	defer f.Close()

	// 更新或创建播放会话
	s.mu.Lock()
	sess, exists := s.sessions[mediaID]
	if !exists {
		sess = &models.PlaybackSession{
			MediaID:  mediaID,
			ClientIP: getClientIP(r),
			Duration: duration,
			FileSize: fileSize,
		}
		s.sessions[mediaID] = sess
	}
	s.mu.Unlock()

	// 记录访问日志
	log.Printf("[INFO] 播放请求: mediaID=%d, client=%s, range=%q", mediaID, sess.ClientIP, r.Header.Get("Range"))

	// 使用 http.ServeContent 自动处理 Range 请求
	http.ServeContent(w, r, filepath.Base(filePath), time.Now(), f)
}

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
	}
}

// GetProgress 获取播放进度信息。
func (s *Service) GetProgress(mediaID int64) (*ProgressInfo, error) {
	s.mu.RLock()
	sess, exists := s.sessions[mediaID]
	s.mu.RUnlock()

	if !exists {
		return &ProgressInfo{}, nil
	}

	var ranges [][2]int64
	if sess.BufferedRanges != "" {
		_ = json.Unmarshal([]byte(sess.BufferedRanges), &ranges)
	}

	return &ProgressInfo{
		CurrentPosition: sess.CurrentPosition,
		Duration:       sess.Duration,
		FileSize:        sess.FileSize,
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
			MediaID:   mediaID,
			Duration:  duration,
			FileSize:  fileSize,
			ClientIP: "",
		}
		s.sessions[mediaID] = sess
	}
	return sess
}

func getClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}




