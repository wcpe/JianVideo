package library

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// ScanExecFunc 扫描执行函数签名，与 Service.ScanLibraryWithType 一致。
// 经函数注入而非直接依赖 Service，便于队列单测替身。
type ScanExecFunc func(libraryID int64, path, dirType, mode string) (int, error)

// ScanChangeExecFunc 扫描变更执行函数签名。
type ScanChangeExecFunc func(change ScanChange) (int, error)

// TaskQueue 扫描任务队列（FR-29）：以 SQLite ScanTask 表为持久化真源，
// 单 worker goroutine 串行执行入队任务，服务重启时把残留 running 重置为 pending 重新入队。
//
// 职责单一：只负责「排队 + 串行调度 + 状态持久化」，扫描执行逻辑由注入的 exec 承担，
// 不在此重写扫描（增量/全量对账是 FR-27）。
type TaskQueue struct {
	db         *gorm.DB
	exec       ScanExecFunc
	changeExec ScanChangeExecFunc
	audit      audit.Recorder
	tasks      *tasksvc.Service

	store targetStore // 任务执行目标的过程态内存映射

	mu       sync.Mutex // 保护 worker 生命周期标志
	signal   chan struct{}
	stopCh   chan struct{}
	started  bool
	stopOnce sync.Once
}

// WithAudit 注入审计记录器，使任务状态变更与审计事件同事务提交。
func (q *TaskQueue) WithAudit(rec audit.Recorder) *TaskQueue {
	q.audit = rec
	return q
}

// WithTasks 注入通用任务中心，使旧扫描队列生命周期同步到 tasks 真源。
func (q *TaskQueue) WithTasks(svc *tasksvc.Service) *TaskQueue {
	q.tasks = svc
	return q
}

// WithChangeExec 注入单路径扫描变更执行函数。
func (q *TaskQueue) WithChangeExec(exec ScanChangeExecFunc) *TaskQueue {
	q.changeExec = exec
	return q
}

// NewTaskQueue 创建扫描任务队列。exec 为实际扫描执行函数（通常为 Service.ScanLibraryWithType）。
func NewTaskQueue(db *gorm.DB, exec ScanExecFunc) *TaskQueue {
	return &TaskQueue{
		db:     db,
		exec:   exec,
		signal: make(chan struct{}, 1),
		stopCh: make(chan struct{}),
	}
}

// Enqueue 入队一个扫描任务，落库为 pending 并唤醒 worker，返回任务 ID。
func (q *TaskQueue) Enqueue(libraryID int64, path, dirType, scanType string) (int64, error) {
	return q.EnqueueInSpace(models.DefaultSpaceID, libraryID, path, dirType, scanType)
}

// EnqueueInSpace 入队指定 Space 的扫描任务，落库为 pending 并唤醒 worker。
func (q *TaskQueue) EnqueueInSpace(spaceID string, libraryID int64, path, dirType, scanType string) (int64, error) {
	if scanType == "" {
		scanType = models.ScanTypeFull
	}
	target := scanTarget{path: path, dirType: dirType, libraryID: libraryID}
	payload, err := encodeScanTarget(target)
	if err != nil {
		return 0, err
	}
	task := &models.ScanTask{
		SpaceID:     normalizeSpaceID(spaceID),
		LibraryID:   libraryID,
		ScanType:    scanType,
		Status:      models.ScanTaskStatusPending,
		PayloadJSON: payload,
	}
	if err := q.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		return q.recordAuditTx(tx, task, "task.created", "")
	}); err != nil {
		return 0, err
	}
	q.syncTask(task)
	q.rememberTarget(task.ID, target)
	q.wake()
	log.Printf("[INFO] 扫描任务已入队: taskID=%d, libraryID=%d, type=%s", task.ID, libraryID, scanType)
	return task.ID, nil
}

// EnqueueChange 将 watcher/轮询产生的单路径变更入队为增量扫描任务。
func (q *TaskQueue) EnqueueChange(change ScanChange) (int64, error) {
	change = NormalizeScanChange(change)
	if change.LibraryID <= 0 || change.Path == "" {
		return 0, fmt.Errorf("扫描变更参数无效")
	}
	target := scanTarget{libraryID: change.LibraryID, change: &change}
	payload, err := encodeScanTarget(target)
	if err != nil {
		return 0, err
	}
	task := &models.ScanTask{
		SpaceID:     change.SpaceID,
		LibraryID:   change.LibraryID,
		ScanType:    models.ScanTypeIncremental,
		Status:      models.ScanTaskStatusPending,
		PayloadJSON: payload,
	}
	if err := q.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		return q.recordAuditTx(tx, task, "task.created", "")
	}); err != nil {
		return 0, err
	}
	q.syncTask(task)
	q.rememberTarget(task.ID, target)
	q.wake()
	log.Printf("[INFO] 扫描变更任务已入队: taskID=%d, libraryID=%d, op=%s", task.ID, change.LibraryID, change.Op)
	return task.ID, nil
}

// EnqueueScheduled 为定时扫描（FR-28）批量入队：逐库跳过禁用库与已有活动（pending/running）任务的库，
// 入队指定类型扫描并返回实际入队数量。复用 Enqueue，不另写扫描逻辑。
func (q *TaskQueue) EnqueueScheduled(libs []models.LibraryPath, scanType string) int {
	enqueued := 0
	for _, lp := range libs {
		if lp.Enabled != 1 {
			continue
		}
		active, err := q.hasActiveTask(lp.ID)
		if err != nil {
			log.Printf("[WARN] 定时扫描查活动任务失败，跳过: libraryID=%d, err=%v", lp.ID, err)
			continue
		}
		if active {
			// 该库已有待执行 / 执行中的任务，跳过以防积压
			continue
		}
		if _, err := q.EnqueueInSpace(lp.SpaceID, lp.ID, lp.Path, lp.Type, scanType); err != nil {
			log.Printf("[WARN] 定时扫描入队失败，跳过: libraryID=%d, err=%v", lp.ID, err)
			continue
		}
		enqueued++
	}
	return enqueued
}

// hasActiveTask 判断指定库是否已有活动任务（pending 或 running）。
func (q *TaskQueue) hasActiveTask(libraryID int64) (bool, error) {
	var cnt int64
	err := q.db.Model(&models.ScanTask{}).
		Where("library_id = ? AND status IN ?", libraryID,
			[]string{models.ScanTaskStatusPending, models.ScanTaskStatusRunning}).
		Count(&cnt).Error
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// RecoverRunning 重启恢复：把残留 running 任务重置为 pending，以便 worker 重新执行（FR-29）。
// 须在 Start 之前调用。注意：恢复后这些任务的扫描目标需经 Enqueue 重新登记，
// 故恢复时按 library_id 反查目录路径重新登记内存目标。
func (q *TaskQueue) RecoverRunning() error {
	var stale []models.ScanTask
	if err := q.db.Where("status = ?", models.ScanTaskStatusRunning).Find(&stale).Error; err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}
	if err := q.db.Model(&models.ScanTask{}).
		Where("status = ?", models.ScanTaskStatusRunning).
		Updates(map[string]any{"status": models.ScanTaskStatusPending, "started_at": nil}).Error; err != nil {
		return err
	}
	for _, t := range stale {
		target, ok, err := q.targetFromTask(t)
		if err == nil && ok {
			q.rememberTarget(t.ID, target)
			t.Status = models.ScanTaskStatusPending
			t.StartedAt = nil
			q.syncTask(&t)
			continue
		}
		if err != nil {
			q.markError(t.ID, err.Error())
			continue
		}
		path, dirType, err := q.lookupTarget(t.LibraryID)
		if err != nil {
			// 目录已不存在，无法重新执行，标记为错误而非永久卡 pending
			q.markError(t.ID, "重启恢复失败：媒体库目录不存在或不可读")
			continue
		}
		q.rememberTarget(t.ID, scanTarget{path: path, dirType: dirType, libraryID: t.LibraryID})
		t.Status = models.ScanTaskStatusPending
		t.StartedAt = nil
		q.syncTask(&t)
	}
	log.Printf("[INFO] 重启恢复扫描队列：%d 个残留 running 任务已重置为 pending", len(stale))
	return nil
}

// Start 启动 worker goroutine（幂等：重复调用只启动一次）。
func (q *TaskQueue) Start() {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return
	}
	q.started = true
	q.mu.Unlock()

	go q.loop()
	// 启动后唤醒一次，处理恢复入队的 pending 任务
	q.wake()
}

// Stop 停止 worker（幂等）。
func (q *TaskQueue) Stop() {
	q.stopOnce.Do(func() { close(q.stopCh) })
}

// ListTasks 返回所有扫描任务，按创建时间倒序（最近在前）。
func (q *TaskQueue) ListTasks() ([]models.ScanTask, error) {
	return q.ListTasksInSpace(models.DefaultSpaceID)
}

// ListTasksInSpace 返回指定 Space 的扫描任务，按创建时间倒序（最近在前）。
func (q *TaskQueue) ListTasksInSpace(spaceID string) ([]models.ScanTask, error) {
	var tasks []models.ScanTask
	if err := q.db.Where("space_id = ?", normalizeSpaceID(spaceID)).Order("created_at DESC, id DESC").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// CancelTaskInSpace 取消尚未执行的扫描任务，并写入同事务审计事件。
func (q *TaskQueue) CancelTaskInSpace(spaceID string, taskID int64) error {
	spaceID = normalizeSpaceID(spaceID)
	var task models.ScanTask
	if err := q.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space_id = ? AND id = ?", spaceID, taskID).First(&task).Error; err != nil {
			return err
		}
		if task.Status != models.ScanTaskStatusPending {
			return fmt.Errorf("仅 pending 扫描任务可取消")
		}
		now := time.Now()
		if err := tx.Model(&models.ScanTask{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":       models.ScanTaskStatusCanceled,
			"completed_at": now,
		}).Error; err != nil {
			return err
		}
		task.Status = models.ScanTaskStatusCanceled
		task.CompletedAt = &now
		q.forgetTarget(taskID)
		if err := q.recordAuditTx(tx, &task, "task.canceled", ""); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	q.syncTask(&task)
	return nil
}

// RetryTaskInSpace 将失败或已取消的扫描任务重新置为 pending，并写入同事务审计事件。
func (q *TaskQueue) RetryTaskInSpace(spaceID string, taskID int64) error {
	spaceID = normalizeSpaceID(spaceID)
	var task models.ScanTask
	if err := q.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space_id = ? AND id = ?", spaceID, taskID).First(&task).Error; err != nil {
			return err
		}
		if task.Status != models.ScanTaskStatusError && task.Status != models.ScanTaskStatusCanceled {
			return fmt.Errorf("仅 error 或 canceled 扫描任务可重试")
		}
		if err := tx.Model(&models.ScanTask{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":        models.ScanTaskStatusPending,
			"error":         "",
			"started_at":    nil,
			"completed_at":  nil,
			"scanned_files": 0,
			"total_files":   0,
		}).Error; err != nil {
			return err
		}
		task.Status = models.ScanTaskStatusPending
		task.Error = ""
		task.StartedAt = nil
		task.CompletedAt = nil
		if err := q.recordAuditTx(tx, &task, "task.retried", ""); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	q.syncTask(&task)
	target, ok, err := q.targetFromTask(task)
	if err != nil {
		return err
	}
	if !ok {
		path, dirType, err := q.lookupTarget(task.LibraryID)
		if err != nil {
			return err
		}
		target = scanTarget{path: path, dirType: dirType, libraryID: task.LibraryID}
	}
	q.rememberTarget(task.ID, target)
	q.wake()
	return nil
}

// loop 单 worker 主循环：取最早 pending 执行，队列空时阻塞等待信号，不空转轮询。
func (q *TaskQueue) loop() {
	for {
		task, ok := q.nextPending()
		if !ok {
			// 无 pending，阻塞等待入队信号或停止
			select {
			case <-q.signal:
				continue
			case <-q.stopCh:
				return
			}
		}
		q.runTask(task)
		// 处理完一个继续取下一个；若期间已停止则退出
		select {
		case <-q.stopCh:
			return
		default:
		}
	}
}

// nextPending 取最早入队的 pending 任务并置为 running（记 started_at）。
// 返回 (task, true) 表示取到；(_, false) 表示当前无 pending。
func (q *TaskQueue) nextPending() (models.ScanTask, bool) {
	var task models.ScanTask
	err := q.db.Where("status = ?", models.ScanTaskStatusPending).
		Order("created_at ASC, id ASC").First(&task).Error
	if err != nil {
		return models.ScanTask{}, false
	}
	now := time.Now()
	if err := q.db.Model(&models.ScanTask{}).Where("id = ?", task.ID).
		Updates(map[string]any{"status": models.ScanTaskStatusRunning, "started_at": now}).Error; err != nil {
		log.Printf("[ERROR] 标记扫描任务为 running 失败: taskID=%d, err=%v", task.ID, err)
		return models.ScanTask{}, false
	}
	task.Status = models.ScanTaskStatusRunning
	task.StartedAt = &now
	q.syncTask(&task)
	return task, true
}

// runTask 执行单个任务并写回终态（completed / error）。扫描执行（高开销 IO）在锁外。
func (q *TaskQueue) runTask(task models.ScanTask) {
	target, ok := q.takeTarget(task.ID)
	if !ok {
		restored, restoredOK, err := q.targetFromTask(task)
		if err != nil {
			q.markError(task.ID, err.Error())
			return
		}
		if !restoredOK {
			q.markError(task.ID, "扫描目标缺失：任务无法执行")
			return
		}
		target = restored
	}

	log.Printf("[INFO] 开始执行扫描任务: taskID=%d, libraryID=%d", task.ID, task.LibraryID)
	count, err := q.runTarget(task, target)
	now := time.Now()
	if err != nil {
		if txErr := q.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.ScanTask{}).Where("id = ?", task.ID).
				Updates(map[string]any{
					"status":       models.ScanTaskStatusError,
					"error":        err.Error(),
					"completed_at": now,
				}).Error; err != nil {
				return err
			}
			return q.recordAuditTx(tx, &task, "task.failed", err.Error())
		}); txErr != nil {
			log.Printf("[ERROR] 标记扫描任务失败终态失败: taskID=%d, err=%v", task.ID, txErr)
		}
		task.Status = models.ScanTaskStatusError
		task.Error = err.Error()
		task.CompletedAt = &now
		q.syncTask(&task)
		log.Printf("[ERROR] 扫描任务执行失败: taskID=%d, err=%v", task.ID, err)
		return
	}
	if err := q.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.ScanTask{}).Where("id = ?", task.ID).
			Updates(map[string]any{
				"status":        models.ScanTaskStatusCompleted,
				"scanned_files": count,
				"completed_at":  now,
			}).Error; err != nil {
			return err
		}
		return q.recordAuditTx(tx, &task, "task.succeeded", "")
	}); err != nil {
		log.Printf("[ERROR] 标记扫描任务完成终态失败: taskID=%d, err=%v", task.ID, err)
		return
	}
	task.Status = models.ScanTaskStatusCompleted
	task.ScannedFiles = count
	task.CompletedAt = &now
	q.syncTask(&task)
	log.Printf("[INFO] 扫描任务执行完成: taskID=%d, count=%d", task.ID, count)
}

func (q *TaskQueue) runTarget(task models.ScanTask, target scanTarget) (int, error) {
	if target.change != nil {
		if q.changeExec == nil {
			return 0, fmt.Errorf("扫描变更执行器未配置")
		}
		return q.changeExec(*target.change)
	}
	return q.exec(target.libraryID, target.path, target.dirType, task.ScanType)
}

func (q *TaskQueue) targetFromTask(task models.ScanTask) (scanTarget, bool, error) {
	target, ok, err := decodeScanTarget(task)
	if err != nil || ok {
		return target, ok, err
	}
	return scanTarget{}, false, nil
}

// markError 把任务标记为 error 并记录原因（用于无法执行的边界场景）。
func (q *TaskQueue) markError(taskID int64, reason string) {
	now := time.Now()
	q.db.Model(&models.ScanTask{}).Where("id = ?", taskID).
		Updates(map[string]any{
			"status":       models.ScanTaskStatusError,
			"error":        reason,
			"completed_at": now,
		})
	var task models.ScanTask
	if err := q.db.First(&task, taskID).Error; err == nil {
		q.syncTask(&task)
	}
	log.Printf("[WARN] 扫描任务标记为错误: taskID=%d, reason=%s", taskID, reason)
}

// wake 非阻塞唤醒 worker（signal 容量为 1，已有待处理信号时直接跳过）。
func (q *TaskQueue) wake() {
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

func (q *TaskQueue) syncTask(task *models.ScanTask) {
	if q.tasks == nil || task == nil {
		return
	}
	progress := 0
	if task.Status == models.ScanTaskStatusCompleted {
		progress = 100
	} else if task.TotalFiles > 0 {
		progress = task.ScannedFiles * 100 / task.TotalFiles
	}
	if err := q.tasks.SyncLegacy(context.Background(), tasksvc.LegacySyncInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        task.SpaceID,
		Type:           "library.scan",
		Status:         task.Status,
		Progress:       progress,
		IdempotencyKey: fmt.Sprintf("scan:%d", task.ID),
		PayloadJSON:    q.taskPayloadJSON(task),
		ResourceType:   "library",
		ResourceID:     fmt.Sprintf("%d", task.LibraryID),
		Error:          task.Error,
		CreatedAt:      task.CreatedAt,
		StartedAt:      task.StartedAt,
		FinishedAt:     task.CompletedAt,
	}); err != nil {
		log.Printf("[WARN] 同步扫描任务到通用任务中心失败: taskID=%d, err=%v", task.ID, err)
	}
}

func (q *TaskQueue) taskPayloadJSON(task *models.ScanTask) string {
	if task.PayloadJSON != "" {
		return task.PayloadJSON
	}
	return fmt.Sprintf(`{"legacy_table":"scan_tasks","legacy_id":%d,"library_id":%d,"scan_type":%q}`, task.ID, task.LibraryID, task.ScanType)
}

func (q *TaskQueue) recordAuditTx(tx *gorm.DB, task *models.ScanTask, action, errText string) error {
	if q.audit == nil {
		return nil
	}
	metadata := map[string]any{
		"task_kind":  "scan",
		"library_id": task.LibraryID,
		"scan_type":  task.ScanType,
	}
	if errText != "" {
		metadata["error"] = errText
	}
	return q.audit.RecordTx(context.Background(), tx, audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      task.SpaceID,
		ActorType:    audit.ActorSystem,
		Action:       action,
		ResourceType: "task",
		ResourceID:   fmt.Sprintf("%d", task.ID),
		Metadata:     metadata,
	})
}
