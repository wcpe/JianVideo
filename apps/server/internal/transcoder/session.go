package transcoder

import (
	"context"
	"sync"
)

// 转码会话状态常量。
const (
	SessionStatusStopped = "stopped"
	SessionStatusRunning = "running"
	SessionStatusSeeking = "seeking"
)

// TranscodeSession 管理单个转码会话的生命周期。
// 每个会话关联一个媒体文件的转码 goroutine，通过 context.CancelFunc 控制中断。
type TranscodeSession struct {
	mu       sync.Mutex
	id       string
	cancelFn context.CancelFunc
	status   string  // "running" | "stopped" | "seeking"
	seekPos  float64 // 最近一次 Seek 的时间位置（秒）
}

// NewTranscodeSession 创建新的转码会话。
func NewTranscodeSession(id string) *TranscodeSession {
	return &TranscodeSession{
		id:     id,
		status: SessionStatusStopped,
	}
}

// ID 返回会话 ID。
func (s *TranscodeSession) ID() string {
	return s.id
}

// Status 返回当前状态。
func (s *TranscodeSession) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Start 启动转码会话，使用给定的 context 和 cancel 函数。
// 若会话已在运行，先停止旧会话再启动，防止旧 context 无法被 cancel。
func (s *TranscodeSession) Start(ctx context.Context, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == SessionStatusRunning && s.cancelFn != nil {
		s.cancelFn()
	}
	s.cancelFn = cancel
	s.status = SessionStatusRunning
	_ = ctx // context 由调用方持有，通过 cancel 控制
}

// Seek 执行 Seek 操作：cancel 旧 context，启动新的转码 goroutine。
// 并发安全：多个 Seek 只保留最新的，旧 goroutine 被 cancel 后自动退出。
func (s *TranscodeSession) Seek(ctx context.Context, cancel context.CancelFunc, positionSec float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 中断旧的转码 goroutine
	if s.cancelFn != nil {
		s.cancelFn()
	}

	s.cancelFn = cancel
	s.seekPos = positionSec
	s.status = SessionStatusSeeking
	_ = ctx
}

// Stop 停止转码会话。
func (s *TranscodeSession) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancelFn != nil {
		s.cancelFn()
		s.cancelFn = nil
	}
	s.status = SessionStatusStopped
}

// SeekPosition 返回最近一次 Seek 的时间位置（秒）。
func (s *TranscodeSession) SeekPosition() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seekPos
}
