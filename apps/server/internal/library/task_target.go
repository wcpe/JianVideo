package library

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const (
	scanPayloadKindLibrary = "library"
	scanPayloadKindChange  = "change"
)

// scanTarget 扫描目标的执行参数。入队时写入 payload，执行期同时暂存内存映射。
type scanTarget struct {
	libraryID int64
	path      string
	dirType   string
	change    *ScanChange
}

type scanTaskPayload struct {
	Kind    string      `json:"kind"`
	Path    string      `json:"path,omitempty"`
	DirType string      `json:"dir_type,omitempty"`
	Change  *ScanChange `json:"change,omitempty"`
}

func encodeScanTarget(t scanTarget) (string, error) {
	payload := scanTaskPayload{
		Kind:    scanPayloadKindLibrary,
		Path:    t.path,
		DirType: t.dirType,
	}
	if t.change != nil {
		change := NormalizeScanChange(*t.change)
		payload.Kind = scanPayloadKindChange
		payload.Change = &change
		payload.Path = ""
		payload.DirType = ""
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("编码扫描任务载荷失败: %w", err)
	}
	return string(b), nil
}

func decodeScanTarget(task models.ScanTask) (scanTarget, bool, error) {
	if task.PayloadJSON == "" {
		return scanTarget{}, false, nil
	}
	var payload scanTaskPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return scanTarget{}, false, fmt.Errorf("解析扫描任务载荷失败: %w", err)
	}
	switch payload.Kind {
	case scanPayloadKindChange:
		if payload.Change == nil {
			return scanTarget{}, false, fmt.Errorf("扫描变更载荷缺少变更内容")
		}
		change := NormalizeScanChange(*payload.Change)
		return scanTarget{libraryID: change.LibraryID, change: &change}, true, nil
	case scanPayloadKindLibrary:
		return scanTarget{libraryID: task.LibraryID, path: payload.Path, dirType: payload.DirType}, true, nil
	default:
		return scanTarget{}, false, fmt.Errorf("未知扫描任务载荷类型: %s", payload.Kind)
	}
}

// targetStore 任务 ID → 扫描目标 的内存映射，配合队列在入队与执行间传递目标参数。
// 仅作快速路径；进程退出后由 scan_tasks.payload_json 或 library_paths 重建。
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

// forgetTarget 丢弃某任务的过程态扫描目标。
func (q *TaskQueue) forgetTarget(taskID int64) {
	q.store.mu.Lock()
	defer q.store.mu.Unlock()
	delete(q.store.targets, taskID)
}

// lookupTarget 按 library_id 反查媒体库目录路径与类型（重启恢复时重建扫描目标）。
func (q *TaskQueue) lookupTarget(libraryID int64) (path, dirType string, err error) {
	var lp models.LibraryPath
	if err := q.db.First(&lp, libraryID).Error; err != nil {
		return "", "", fmt.Errorf("媒体库目录不存在: id=%d", libraryID)
	}
	return lp.Path, lp.Type, nil
}
