package update

import "sync"

// 进度状态机取值（FR-90）：贯穿自更新「下载→校验→替换」链路，供前端轮询展示。
const (
	progressIdle        = "idle"        // 空闲：未在更新
	progressDownloading = "downloading" // 下载二进制产物中
	progressVerifying   = "verifying"   // 下载完毕、校验 sha256 中
	progressDone        = "done"        // 替换已触发（进程即将重启）
	progressFailed      = "failed"      // 下载或校验失败
)

// progressState 自更新下载进度快照（FR-90），直接序列化给前端轮询。
// Total=0 表示总字节未知（响应无 Content-Length），此时 Percent 为 0、前端退化为展示已下载字节。
type progressState struct {
	State      string `json:"state"`      // 见 progress* 常量：idle/downloading/verifying/done/failed
	Downloaded int64  `json:"downloaded"` // 已下载字节数
	Total      int64  `json:"total"`      // 总字节数（0 表示未知）
	Percent    int    `json:"percent"`    // 下载百分比 0-100（total=0 时为 0）
}

// progressPercent 由已下载 / 总字节计算下载百分比（纯函数，FR-90）。
// total≤0（未知）时返回 0；否则返回 [0,100] 内的整数（钳制上界防超过 100）。
func progressPercent(downloaded, total int64) int {
	if total <= 0 || downloaded <= 0 {
		return 0
	}
	p := int(downloaded * 100 / total)
	if p > 100 {
		p = 100
	}
	return p
}

// progressTracker 进程内进度状态单例：互斥量保护，供下载链路写、进度端点读，并发安全。
// 与 FR-46 的 TTL 缓存单例同构，不落库（自更新本就用户显式触发、单次互斥，无需持久化）。
type progressTracker struct {
	mu sync.Mutex
	st progressState
}

// snapshot 返回进度副本（并发安全，外部改动不污染内部状态）。
// 零值（从未更新）归一化为 idle；Percent 由已下载 / 总字节即时算出，使进度端点恒返回有意义的状态。
func (t *progressTracker) snapshot() progressState {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.st
	if s.State == "" {
		s.State = progressIdle
	}
	s.Percent = progressPercent(s.Downloaded, s.Total)
	return s
}

// downloading 置为下载态并更新已下载/总字节。供 downloadToTemp 的进度回调直接调用。
func (t *progressTracker) downloading(downloaded, total int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.st.State = progressDownloading
	t.st.Downloaded = downloaded
	if total > 0 {
		t.st.Total = total
	}
}

// setState 仅切换状态阶段，保留已记录的字节计数。
func (t *progressTracker) setState(state string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.st.State = state
}

// reset 重置为下载初态（清零计数），用于一次新的 Apply 开始时。
func (t *progressTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.st = progressState{State: progressDownloading}
}

// Progress 返回当前自更新进度快照（FR-90），供进度查询端点。
func (s *Service) Progress() progressState {
	return s.progress.snapshot()
}

// setProgressDownloading 上报下载进度（已下载 / 总字节）。作为 downloadToTemp 的回调。
func (s *Service) setProgressDownloading(downloaded, total int64) {
	s.progress.downloading(downloaded, total)
}

// setProgressVerifying 进入校验阶段。
func (s *Service) setProgressVerifying() { s.progress.setState(progressVerifying) }

// setProgressDone 替换已触发（进程即将重启）。
func (s *Service) setProgressDone() { s.progress.setState(progressDone) }

// setProgressFailed 标记失败。
func (s *Service) setProgressFailed() { s.progress.setState(progressFailed) }

// resetProgress 开始一次新的下载，清零计数并置下载态。
func (s *Service) resetProgress() { s.progress.reset() }
