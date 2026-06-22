package library

import (
	"fmt"
	"sync"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// scanTarget 扫描目标的执行参数。仅 worker 执行期需要，不入库（path 真源在 library_paths 表）。
type scanTarget struct {
	libraryID int64
	path      string
	dirType   string
}

// targetStore 任务 ID → 扫描目标 的内存映射，配合队列在入队与执行间传递目标参数。
// 仅过程态，并发安全；进程退出即丢弃（重启由 RecoverRunning 按 library_id 反查重建）。
type targetStore struct {
	mu      sync.Mutex
	targets map[int64]scanTarget
}

// rememberTarget 记下某任务的扫描目标。
func (q *TaskQueue) rememberTarget(taskID int64, t scanTarget) {
	q.store.mu.Lock()
	defer q.store.mu.Unlock()
	if q.store.targets == nil {
		q.store.targets = make(map[int64]scanTarget)
	}
	q.store.targets[taskID] = t
}

// takeTarget 取出并移除某任务的扫描目标。
func (q *TaskQueue) takeTarget(taskID int64) (scanTarget, bool) {
	q.store.mu.Lock()
	defer q.store.mu.Unlock()
	t, ok := q.store.targets[taskID]
	if ok {
		delete(q.store.targets, taskID)
	}
	return t, ok
}

// lookupTarget 按 library_id 反查媒体库目录路径与类型（重启恢复时重建扫描目标）。
func (q *TaskQueue) lookupTarget(libraryID int64) (path, dirType string, err error) {
	var lp models.LibraryPath
	if err := q.db.First(&lp, libraryID).Error; err != nil {
		return "", "", fmt.Errorf("媒体库目录不存在: id=%d", libraryID)
	}
	return lp.Path, lp.Type, nil
}
