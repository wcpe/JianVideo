package playback

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewService 验证 NewService 返回非 nil 实例且可正常停止。
func TestNewService(t *testing.T) {
	s := NewService()
	require.NotNil(t, s)
	t.Cleanup(s.Stop)
}

// TestHandleSeek_NewSession 验证 HandleSeek 创建新会话并返回正确响应。
func TestHandleSeek_NewSession(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	resp, err := s.HandleSeek(1, 10.5)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 10.5, resp.Position)
}

// TestHandleSeek_ExistingSession 验证 HandleSeek 更新已有会话的 Position。
func TestHandleSeek_ExistingSession(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	resp1, err := s.HandleSeek(1, 5.0)
	require.NoError(t, err)
	assert.Equal(t, 5.0, resp1.Position)

	resp2, err := s.HandleSeek(1, 15.0)
	require.NoError(t, err)
	assert.Equal(t, 15.0, resp2.Position)
}

// TestHandleBufferReport_NewSession 验证 HandleBufferReport 创建新会话并可通过 GetProgress 读取。
func TestHandleBufferReport_NewSession(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	s.HandleBufferReport(1, BufferReport{
		CurrentPosition: 30,
		FileSize:        1000,
		BufferedRanges:  [][2]int64{{0, 500}},
	})

	progress, err := s.GetProgress(1)
	require.NoError(t, err)
	assert.Equal(t, float64(30), progress.CurrentPosition)
	assert.Equal(t, int64(1000), progress.FileSize)
}

// TestHandleBufferReport_UpdateExisting 验证 HandleBufferReport 更新已有会话的状态。
func TestHandleBufferReport_UpdateExisting(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	s.HandleBufferReport(1, BufferReport{
		CurrentPosition: 30,
		FileSize:        1000,
		BufferedRanges:  [][2]int64{{0, 500}},
	})

	s.HandleBufferReport(1, BufferReport{
		CurrentPosition: 60,
		FileSize:        2000,
		BufferedRanges:  [][2]int64{{0, 1000}},
	})

	progress, err := s.GetProgress(1)
	require.NoError(t, err)
	assert.Equal(t, float64(60), progress.CurrentPosition)
	assert.Equal(t, int64(2000), progress.FileSize)
}

// TestGetProgress_NoSession 验证未创建会话时 GetProgress 返回零值 ProgressInfo。
func TestGetProgress_NoSession(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	progress, err := s.GetProgress(999)
	require.NoError(t, err)
	require.NotNil(t, progress)
	assert.Equal(t, float64(0), progress.CurrentPosition)
	assert.Equal(t, float64(0), progress.Duration)
	assert.Equal(t, int64(0), progress.FileSize)
}

// TestGetProgress_WithSession 验证 HandleSeek 后 GetProgress 返回正确的 CurrentPosition 和 Duration。
func TestGetProgress_WithSession(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	// 通过 GetOrCreateSession 创建带 Duration 的会话
	s.GetOrCreateSession(1, 120.0, 5000)
	// 通过 HandleSeek 设置 CurrentPosition
	_, err := s.HandleSeek(1, 45.0)
	require.NoError(t, err)

	progress, err := s.GetProgress(1)
	require.NoError(t, err)
	assert.Equal(t, float64(45), progress.CurrentPosition)
	assert.Equal(t, float64(120), progress.Duration)
}

// TestGetOrCreateSession_New 验证 GetOrCreateSession 创建新会话并设置正确字段。
func TestGetOrCreateSession_New(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	sess := s.GetOrCreateSession(1, 120.0, 5000)
	require.NotNil(t, sess)
	assert.Equal(t, float64(120), sess.Duration)
	assert.Equal(t, int64(5000), sess.FileSize)
}

// TestGetOrCreateSession_Existing 验证 GetOrCreateSession 对同一 mediaID 返回同一指针。
func TestGetOrCreateSession_Existing(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	sess1 := s.GetOrCreateSession(1, 120.0, 5000)
	sess2 := s.GetOrCreateSession(1, 200.0, 9999)
	assert.True(t, sess1 == sess2, "同一 mediaID 应返回同一指针")
	// 已有会话不应被覆盖
	assert.Equal(t, float64(120), sess1.Duration)
	assert.Equal(t, int64(5000), sess1.FileSize)
}

// TestRecordNegotiation 验证协商结果（实际编码与路径）记到会话（FR-53）。
func TestRecordNegotiation(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	// 首次记录：会话不存在则创建
	s.RecordNegotiation(5, "av1", "fmp4")
	sess := s.GetOrCreateSession(5, 0, 0)
	assert.Equal(t, "av1", sess.TargetCodec)
	assert.Equal(t, "fmp4", sess.OutputPath)

	// 再次记录：同一会话被更新（如重新协商回退 h264）
	s.RecordNegotiation(5, "h264", "ts")
	assert.Equal(t, "h264", sess.TargetCodec)
	assert.Equal(t, "ts", sess.OutputPath)
}

// TestCleanupExpiredSessions 验证 cleanupExpiredSessions 清除超过 24 小时的旧会话。
func TestCleanupExpiredSessions(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	// 手动添加一个 UpdatedAt 超过 24 小时的旧会话
	s.mu.Lock()
	s.sessions[1] = &models.PlaybackSession{
		MediaID:   1,
		UpdatedAt: time.Now().Add(-25 * time.Hour),
	}
	s.mu.Unlock()

	s.cleanupExpiredSessions()

	s.mu.RLock()
	_, exists := s.sessions[1]
	s.mu.RUnlock()
	assert.False(t, exists, "旧会话应被清除")
}

// TestStreamFile_InvalidSMBPath 验证 StreamFile 传入格式错误的 SMB 路径时返回 404。
func TestStreamFile_InvalidSMBPath(t *testing.T) {
	s := NewService()
	t.Cleanup(s.Stop)

	// smb://host/share 只有两段，少于 3 段，openSMBFile 会返回错误
	req := httptest.NewRequest(http.MethodGet, "/stream/1", nil)
	w := httptest.NewRecorder()
	s.StreamFile(w, req, 1, "smb://host/share", 1000, 120.0)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
