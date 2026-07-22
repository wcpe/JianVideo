package library

import (
	"sync"
	"time"
)

// ScanScheduler 定时扫描调度器（FR-28）：按设置中的可配置周期，周期性触发一轮全库入队扫描。
//
// 纯定时组件，经函数注入解耦：周期与触发动作均由外部提供，便于无依赖的 -race 单测。
// 不重写扫描执行（FR-27）、不重写入队（FR-29），到点调 triggerFn 经队列入队即可。
type ScanScheduler struct {
	intervalFn func() time.Duration // 返回当前周期；<=0 表示关闭定时扫描
	triggerFn  func()               // 触发一轮全库入队（高开销，在锁外执行）

	reload   chan struct{}
	stopCh   chan struct{}
	mu       sync.Mutex // 仅保护 started 生命周期标志
	started  bool
	stopOnce sync.Once
}

// NewScanScheduler 创建定时扫描调度器。
// intervalFn 读取当前周期（通常解析 settings.scan_interval），triggerFn 触发一轮全库入队。
func NewScanScheduler(intervalFn func() time.Duration, triggerFn func()) *ScanScheduler {
	return &ScanScheduler{
		intervalFn: intervalFn,
		triggerFn:  triggerFn,
		reload:     make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
	}
}

// Start 启动调度循环（幂等：重复调用只启动一次）。
func (s *ScanScheduler) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go s.loop()
}

// Stop 停止调度（幂等）。
func (s *ScanScheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// Reload 周期变更后非阻塞唤醒循环重排（如设置页改了 scan_interval）。
func (s *ScanScheduler) Reload() {
	select {
	case s.reload <- struct{}{}:
	default:
	}
}

// loop 调度主循环：每轮按当前周期等待，到点触发；周期 <=0 时阻塞等待重载或停止，不空转。
func (s *ScanScheduler) loop() {
	for {
		interval := s.intervalFn()
		if interval <= 0 {
			// 定时扫描关闭：阻塞等待周期变更或停止
			select {
			case <-s.reload:
				continue
			case <-s.stopCh:
				return
			}
		}

		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
			s.triggerFn()
		case <-s.reload:
			timer.Stop()
		case <-s.stopCh:
			timer.Stop()
			return
		}
	}
}
