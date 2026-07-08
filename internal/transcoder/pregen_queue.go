package transcoder

import (
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// PregenExecFunc 预生成执行函数签名（FR-77）。
// 经函数注入而非直接依赖 library/playback，便于队列单测替身并保持 transcoder 依赖单向。
// 入参为任务快照的媒体与目标编码，由注入方负责反查媒体路径并调 PreSliceWithCodec。
type PregenExecFunc func(mediaID int64, codec string) error

// PregenQueue 转码预生成队列（FR-77）：以 SQLite TranscodeTask 表为持久化真源，
// 单 worker goroutine 串行执行入队任务，服务重启时把残留 running 重置为 pending 重新入队。
//
// 范式复用 FR-29 扫描任务队列（见 ADR-0039）。职责单一：只负责「排队 + 串行调度 + 状态持久化」，
// 实际预转码切片由注入的 exec 承担（调 PreSliceWithCodec）。
type PregenQueue struct {
	db   *gorm.DB
	exec PregenExecFunc

	mu       sync.Mutex // 保护 worker 生命周期标志
	signal   chan struct{}
	stopCh   chan struct{}
	started  bool
	stopOnce sync.Once
}

// NewPregenQueue 创建预生成队列。exec 为实际预转码执行函数。
func NewPregenQueue(db *gorm.DB, exec PregenExecFunc) *PregenQueue {
	return &PregenQueue{
		db:     db,
		exec:   exec,
		signal: make(chan struct{}, 1),
		stopCh: make(chan struct{}),
	}
}

// Enqueue 入队一个预生成任务，落库为 pending 并唤醒 worker，返回任务 ID。
// codec/width/height 为入队时刻预设的快照，使任务执行不强依赖预设此后是否被改/删。
func (q *PregenQueue) Enqueue(mediaID, presetID int64, codec string, width, height int) (int64, error) {
	return q.EnqueueInSpace(models.DefaultSpaceID, mediaID, presetID, codec, width, height)
}

// EnqueueInSpace 入队指定 Space 的预生成任务。
func (q *PregenQueue) EnqueueInSpace(spaceID string, mediaID, presetID int64, codec string, width, height int) (int64, error) {
	task := &models.TranscodeTask{
		SpaceID:  normalizeTaskSpaceID(spaceID),
		MediaID:  mediaID,
		PresetID: presetID,
		Codec:    codec,
		Width:    width,
		Height:   height,
		Status:   models.TranscodeTaskStatusPending,
	}
	if err := q.db.Create(task).Error; err != nil {
		return 0, err
	}
	q.wake()
	log.Printf("[INFO] 预生成任务已入队: taskID=%d, mediaID=%d, presetID=%d, codec=%s", task.ID, mediaID, presetID, codec)
	return task.ID, nil
}

// RecoverRunning 重启恢复：把残留 running 任务重置为 pending，以便 worker 重新执行（FR-77）。
// 须在 Start 之前调用。任务已持久化媒体/编码快照，无需额外内存目标重建。
func (q *PregenQueue) RecoverRunning() error {
	var cnt int64
	if err := q.db.Model(&models.TranscodeTask{}).
		Where("status = ?", models.TranscodeTaskStatusRunning).Count(&cnt).Error; err != nil {
		return err
	}
	if cnt == 0 {
		return nil
	}
	if err := q.db.Model(&models.TranscodeTask{}).
		Where("status = ?", models.TranscodeTaskStatusRunning).
		Updates(map[string]any{"status": models.TranscodeTaskStatusPending, "started_at": nil}).Error; err != nil {
		return err
	}
	log.Printf("[INFO] 重启恢复预生成队列：%d 个残留 running 任务已重置为 pending", cnt)
	return nil
}

// Start 启动 worker goroutine（幂等：重复调用只启动一次）。
func (q *PregenQueue) Start() {
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
func (q *PregenQueue) Stop() {
	q.stopOnce.Do(func() { close(q.stopCh) })
}

// ListTasks 返回预生成任务，按创建时间倒序（最近在前）。statusFilter 非空时仅返回该状态。
func (q *PregenQueue) ListTasks(statusFilter string) ([]models.TranscodeTask, error) {
	return q.ListTasksInSpace(models.DefaultSpaceID, statusFilter)
}

// ListTasksInSpace 返回指定 Space 的预生成任务。
func (q *PregenQueue) ListTasksInSpace(spaceID, statusFilter string) ([]models.TranscodeTask, error) {
	var tasks []models.TranscodeTask
	query := q.db.Where("space_id = ?", normalizeTaskSpaceID(spaceID)).Order("created_at DESC, id DESC")
	if statusFilter != "" {
		query = query.Where("status = ?", statusFilter)
	}
	if err := query.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// loop 单 worker 主循环：取最早 pending 执行，队列空时阻塞等待信号，不空转轮询。
func (q *PregenQueue) loop() {
	for {
		task, ok := q.nextPending()
		if !ok {
			select {
			case <-q.signal:
				continue
			case <-q.stopCh:
				return
			}
		}
		q.runTask(task)
		select {
		case <-q.stopCh:
			return
		default:
		}
	}
}

// nextPending 取最早入队的 pending 任务并置为 running（记 started_at）。
func (q *PregenQueue) nextPending() (models.TranscodeTask, bool) {
	var task models.TranscodeTask
	err := q.db.Where("status = ?", models.TranscodeTaskStatusPending).
		Order("created_at ASC, id ASC").First(&task).Error
	if err != nil {
		return models.TranscodeTask{}, false
	}
	now := time.Now()
	if err := q.db.Model(&models.TranscodeTask{}).Where("id = ?", task.ID).
		Updates(map[string]any{"status": models.TranscodeTaskStatusRunning, "started_at": now}).Error; err != nil {
		log.Printf("[ERROR] 标记预生成任务为 running 失败: taskID=%d, err=%v", task.ID, err)
		return models.TranscodeTask{}, false
	}
	task.Status = models.TranscodeTaskStatusRunning
	task.StartedAt = &now
	return task, true
}

// runTask 执行单个预生成任务并写回终态。预转码（高开销 IO）在锁外。
func (q *PregenQueue) runTask(task models.TranscodeTask) {
	log.Printf("[INFO] 开始执行预生成任务: taskID=%d, mediaID=%d, codec=%s", task.ID, task.MediaID, task.Codec)
	err := q.exec(task.MediaID, task.Codec)
	now := time.Now()
	if err != nil {
		q.db.Model(&models.TranscodeTask{}).Where("id = ?", task.ID).
			Updates(map[string]any{
				"status":       models.TranscodeTaskStatusError,
				"error":        err.Error(),
				"completed_at": now,
			})
		log.Printf("[ERROR] 预生成任务执行失败: taskID=%d, err=%v", task.ID, err)
		return
	}
	q.db.Model(&models.TranscodeTask{}).Where("id = ?", task.ID).
		Updates(map[string]any{
			"status":       models.TranscodeTaskStatusCompleted,
			"completed_at": now,
		})
	log.Printf("[INFO] 预生成任务执行完成: taskID=%d", task.ID)
}

// wake 非阻塞唤醒 worker（signal 容量为 1，已有待处理信号时直接跳过）。
func (q *PregenQueue) wake() {
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

func normalizeTaskSpaceID(spaceID string) string {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		return models.DefaultSpaceID
	}
	return spaceID
}
