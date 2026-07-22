// Package tasks 提供通用异步任务队列中心。
package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	dbutil "github.com/wcpe/JianVideo/internal/db"
	"github.com/wcpe/JianVideo/internal/db/models"
)

const (
	fairnessHighPriorityLimit = 3
	retryBackoffBase          = time.Second
)

var (
	// ErrTaskNotFound 表示指定任务不存在或不在当前查询作用域。
	ErrTaskNotFound = errors.New("任务不存在")
	// ErrNoPendingTask 表示当前没有可领取的待执行任务。
	ErrNoPendingTask = errors.New("没有可领取的任务")
)

// Service 管理通用异步任务的入队、领取、状态流转与审计。
type Service struct {
	db             *gorm.DB
	audit          audit.Recorder
	now            func() time.Time
	mu             sync.Mutex
	priorityStreak map[string]int
	cancelMu       sync.Mutex
	runningCancels map[int64]runningCancellation
}

type runningCancellation struct {
	token  *byte
	cancel context.CancelFunc
}

// EnqueueInput 描述创建通用异步任务所需的字段。
type EnqueueInput struct {
	Scope          string
	SpaceID        string
	Type           string
	Priority       int
	MaxAttempts    int
	IdempotencyKey string
	PayloadJSON    string
	ResourceType   string
	ResourceID     string
}

// LegacySyncInput 描述旧队列任务同步到通用任务表所需的字段。
type LegacySyncInput struct {
	Scope          string
	SpaceID        string
	Type           string
	Status         string
	Priority       int
	Attempts       int
	MaxAttempts    int
	Progress       int
	IdempotencyKey string
	PayloadJSON    string
	ResourceType   string
	ResourceID     string
	Error          string
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

// Query 描述任务列表、统计和详情查询的过滤条件。
type Query struct {
	Scope        string
	SpaceID      string
	Type         string
	Status       string
	ResourceType string
	ResourceID   string
	Page         int
	PageSize     int
	Limit        int
}

// ClaimQuery 描述 worker 领取任务时的过滤条件。
type ClaimQuery struct {
	Type string
}

// ProgressInput 描述任务进度更新字段。
type ProgressInput struct {
	Progress   int
	Checkpoint string
}

// Page 是任务分页查询结果。
type Page struct {
	Items []models.Task
	Page  int
	Size  int
	Total int64
}

// Stats 是任务统计汇总结果。
type Stats struct {
	Total    int64
	ByStatus map[string]int64
	ByType   map[string]int64
}

// NewService 创建通用异步任务服务。
func NewService(db *gorm.DB) *Service {
	return &Service{
		db:             db,
		now:            time.Now,
		priorityStreak: map[string]int{},
		runningCancels: map[int64]runningCancellation{},
	}
}

// WithAudit 设置任务状态变更审计记录器。
func (s *Service) WithAudit(rec audit.Recorder) *Service {
	s.audit = rec
	return s
}

// SetNowForTest 替换当前时间来源以便测试可控。
func (s *Service) SetNowForTest(now func() time.Time) {
	s.now = now
}

// NormalizeStatus 将外部或旧队列状态归一为通用任务状态。
func NormalizeStatus(status string) (string, error) {
	switch strings.TrimSpace(status) {
	case models.TaskStatusPending:
		return models.TaskStatusPending, nil
	case models.TaskStatusRunning:
		return models.TaskStatusRunning, nil
	case models.TaskStatusSucceeded, "completed":
		return models.TaskStatusSucceeded, nil
	case models.TaskStatusFailed, "error":
		return models.TaskStatusFailed, nil
	case models.TaskStatusCanceled:
		return models.TaskStatusCanceled, nil
	default:
		return "", fmt.Errorf("任务状态无效: %s", status)
	}
}

// Enqueue 创建待执行任务，并按幂等键复用未完成任务。
func (s *Service) Enqueue(ctx context.Context, input EnqueueInput) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var task *models.Task
	err := dbutil.RetrySQLiteBusy(ctx, func() error {
		task = nil
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var enqueueErr error
			task, enqueueErr = s.enqueueTxLocked(ctx, tx, input)
			return enqueueErr
		})
	})
	return task, err
}

// EnqueueTx 在调用方事务内创建任务，供设置与补偿任务原子提交。
// tx 须非 nil；api 层应传入 settings.TxRepository 等实现，domain 可用 AsTx(*gorm.DB)。
func (s *Service) EnqueueTx(ctx context.Context, tx Tx, input EnqueueInput) (*models.Task, error) {
	if tx == nil || tx.DB() == nil {
		return nil, errors.New("事务句柄不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enqueueTxLocked(ctx, tx.DB(), input)
}

func (s *Service) enqueueTxLocked(ctx context.Context, tx *gorm.DB, input EnqueueInput) (*models.Task, error) {
	scope, spaceID, err := normalizeScope(input.Scope, input.SpaceID)
	if err != nil {
		return nil, err
	}
	taskType := strings.TrimSpace(input.Type)
	if taskType == "" {
		return nil, errors.New("任务类型不能为空")
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if key != "" {
		if existing, ok, findErr := findUnfinishedByKeyDB(ctx, tx, key); findErr != nil {
			return nil, findErr
		} else if ok {
			return &existing, nil
		}
	}
	now := s.now().UTC()
	maxAttempts := input.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	task := models.Task{
		Scope: scope, SpaceID: spaceID, Type: taskType, Status: models.TaskStatusPending,
		Priority: input.Priority, MaxAttempts: maxAttempts, IdempotencyKey: key,
		PayloadJSON: strings.TrimSpace(input.PayloadJSON), ResourceType: strings.TrimSpace(input.ResourceType),
		ResourceID: strings.TrimSpace(input.ResourceID), CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.WithContext(ctx).Create(&task).Error; err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, &task, "task.created", ""); err != nil {
		return nil, err
	}
	return &task, nil
}

// SyncLegacy 将旧扫描或转码队列任务同步到通用任务真源。
func (s *Service) SyncLegacy(ctx context.Context, input LegacySyncInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.TrimSpace(input.IdempotencyKey)
	if key == "" {
		return errors.New("旧任务同步必须包含幂等键")
	}
	scope, spaceID, err := normalizeScope(input.Scope, input.SpaceID)
	if err != nil {
		return err
	}
	status, err := NormalizeStatus(input.Status)
	if err != nil {
		return err
	}
	taskType := strings.TrimSpace(input.Type)
	if taskType == "" {
		return errors.New("任务类型不能为空")
	}
	now := s.now().UTC()
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	maxAttempts := input.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	progress := input.Progress
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	if status == models.TaskStatusSucceeded {
		progress = 100
	}

	var existing models.Task
	err = s.db.WithContext(ctx).Where("idempotency_key = ?", key).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		task := models.Task{
			Scope:          scope,
			SpaceID:        spaceID,
			Type:           taskType,
			Status:         status,
			Priority:       input.Priority,
			Attempts:       input.Attempts,
			MaxAttempts:    maxAttempts,
			Progress:       progress,
			IdempotencyKey: key,
			PayloadJSON:    strings.TrimSpace(input.PayloadJSON),
			ResourceType:   strings.TrimSpace(input.ResourceType),
			ResourceID:     strings.TrimSpace(input.ResourceID),
			Error:          strings.TrimSpace(input.Error),
			CreatedAt:      createdAt.UTC(),
			UpdatedAt:      now,
			StartedAt:      input.StartedAt,
			FinishedAt:     input.FinishedAt,
		}
		return s.db.WithContext(ctx).Create(&task).Error
	}
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ?", existing.ID).Updates(map[string]any{
		"scope":         scope,
		"space_id":      spaceID,
		"type":          taskType,
		"status":        status,
		"priority":      input.Priority,
		"attempts":      input.Attempts,
		"max_attempts":  maxAttempts,
		"progress":      progress,
		"payload_json":  strings.TrimSpace(input.PayloadJSON),
		"resource_type": strings.TrimSpace(input.ResourceType),
		"resource_id":   strings.TrimSpace(input.ResourceID),
		"error":         strings.TrimSpace(input.Error),
		"started_at":    input.StartedAt,
		"finished_at":   input.FinishedAt,
		"next_run_at":   nil,
		"updated_at":    now,
	}).Error
}

// List 按过滤条件返回任务分页列表。
func (s *Service) List(ctx context.Context, query Query) (Page, error) {
	db, err := s.applyQuery(s.db.WithContext(ctx).Model(&models.Task{}), query)
	if err != nil {
		return Page{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page{}, err
	}
	var items []models.Task
	db = db.Order("created_at DESC, id DESC")
	page, size := normalizePage(query)
	if query.Limit > 0 {
		size = query.Limit
		page = 1
	}
	db = db.Limit(size).Offset((page - 1) * size)
	if err := db.Find(&items).Error; err != nil {
		return Page{}, err
	}
	return Page{Items: items, Page: page, Size: size, Total: total}, nil
}

// Get 按作用域查询单个任务。
func (s *Service) Get(ctx context.Context, id int64, query Query) (*models.Task, error) {
	db, err := s.applyQuery(s.db.WithContext(ctx).Model(&models.Task{}), query)
	if err != nil {
		return nil, err
	}
	var task models.Task
	err = db.Where("id = ?", id).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// Stats 按过滤条件聚合任务数量。
func (s *Service) Stats(ctx context.Context, query Query) (Stats, error) {
	db, err := s.applyQuery(s.db.WithContext(ctx).Model(&models.Task{}), query)
	if err != nil {
		return Stats{}, err
	}
	var statusRows []struct {
		Status string
		Count  int64
	}
	if err := db.Session(&gorm.Session{}).Select("status, COUNT(*) AS count").Group("status").Scan(&statusRows).Error; err != nil {
		return Stats{}, err
	}
	var typeRows []struct {
		Type  string
		Count int64
	}
	if err := db.Session(&gorm.Session{}).Select("type, COUNT(*) AS count").Group("type").Scan(&typeRows).Error; err != nil {
		return Stats{}, err
	}
	stats := Stats{ByStatus: map[string]int64{
		models.TaskStatusPending:   0,
		models.TaskStatusRunning:   0,
		models.TaskStatusSucceeded: 0,
		models.TaskStatusFailed:    0,
		models.TaskStatusCanceled:  0,
	}, ByType: map[string]int64{}}
	for _, row := range statusRows {
		stats.Total += row.Count
		stats.ByStatus[row.Status] = row.Count
	}
	for _, row := range typeRows {
		stats.ByType[row.Type] = row.Count
	}
	return stats, nil
}

// ClaimNext 领取一个已到期的 pending 任务并标记为 running。
func (s *Service) ClaimNext(ctx context.Context, query ClaimQuery) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().UTC()
	base := s.db.WithContext(ctx).
		Where("status = ?", models.TaskStatusPending).
		Where("next_run_at IS NULL OR next_run_at <= ?", now)
	taskType := strings.TrimSpace(query.Type)
	if taskType != "" {
		base = base.Where("type = ?", taskType)
	}
	top, err := firstPending(base)
	if err != nil {
		return nil, err
	}

	selected := top
	key := taskType
	if key == "" {
		key = "*"
	}
	if s.priorityStreak[key] >= fairnessHighPriorityLimit {
		if lower, ok := oldestLowerPriority(base, top.Priority); ok {
			selected = lower
			s.priorityStreak[key] = 0
		}
	}

	updates := map[string]any{
		"status":      models.TaskStatusRunning,
		"started_at":  now,
		"next_run_at": nil,
		"updated_at":  now,
	}
	result := s.db.WithContext(ctx).Model(&models.Task{}).
		Where("id = ? AND status = ?", selected.ID, models.TaskStatusPending).
		Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrTaskNotFound
	}
	selected.Status = models.TaskStatusRunning
	selected.StartedAt = &now
	selected.NextRunAt = nil
	selected.UpdatedAt = now
	if selected.Priority == top.Priority {
		s.priorityStreak[key]++
	} else {
		s.priorityStreak[key] = 0
	}
	return &selected, nil
}

// Cancel 将 pending 或 running 任务标记为 canceled。
func (s *Service) Cancel(ctx context.Context, id int64, query Query) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getScoped(ctx, id, query)
	if err != nil {
		return err
	}
	if task.Status != models.TaskStatusPending && task.Status != models.TaskStatusRunning {
		return fmt.Errorf("仅 pending 或 running 任务可取消")
	}
	now := s.now().UTC()
	task.Status = models.TaskStatusCanceled
	task.FinishedAt = &now
	task.UpdatedAt = now
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Task{}).Where("id = ?", id).Updates(map[string]any{
			"status":      models.TaskStatusCanceled,
			"finished_at": now,
			"updated_at":  now,
		}).Error; err != nil {
			return err
		}
		return s.recordAuditTx(ctx, tx, task, "task.canceled", "")
	})
	if err != nil {
		return err
	}
	s.signalCancellation(id)
	return nil
}

// Retry 将 failed 或 canceled 任务重置为 pending。
func (s *Service) Retry(ctx context.Context, id int64, query Query) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.getScoped(ctx, id, query)
	if err != nil {
		return err
	}
	if task.Status != models.TaskStatusFailed && task.Status != models.TaskStatusCanceled {
		return fmt.Errorf("仅 failed 或 canceled 任务可重试")
	}
	now := s.now().UTC()
	task.Status = models.TaskStatusPending
	task.Attempts = 0
	task.Progress = 0
	task.Checkpoint = ""
	task.Error = ""
	task.StartedAt = nil
	task.FinishedAt = nil
	task.UpdatedAt = now
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Task{}).Where("id = ?", id).Updates(map[string]any{
			"status":      models.TaskStatusPending,
			"attempts":    0,
			"progress":    0,
			"checkpoint":  "",
			"error":       "",
			"started_at":  nil,
			"finished_at": nil,
			"next_run_at": nil,
			"updated_at":  now,
		}).Error; err != nil {
			return err
		}
		return s.recordAuditTx(ctx, tx, task, "task.retried", "")
	})
}

// UpdateProgress 更新 running 任务的进度和检查点。
func (s *Service) UpdateProgress(ctx context.Context, id int64, input ProgressInput) error {
	if input.Progress < 0 || input.Progress > 100 {
		return fmt.Errorf("任务进度无效: %d", input.Progress)
	}
	task, err := s.getByID(ctx, id)
	if err != nil {
		return err
	}
	if task.Status != models.TaskStatusRunning {
		return fmt.Errorf("仅 running 任务可更新进度")
	}
	now := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ? AND status = ?", id, models.TaskStatusRunning).Updates(map[string]any{
		"progress":   input.Progress,
		"checkpoint": input.Checkpoint,
		"updated_at": now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("仅 running 任务可更新进度")
	}
	return nil
}

// MarkSucceeded 将 running 任务标记为 succeeded。
func (s *Service) MarkSucceeded(ctx context.Context, id int64) error {
	task, err := s.getByID(ctx, id)
	if err != nil {
		return err
	}
	if task.Status != models.TaskStatusRunning {
		return fmt.Errorf("仅 running 任务可标记成功")
	}
	now := s.now().UTC()
	task.Status = models.TaskStatusSucceeded
	task.Progress = 100
	task.Error = ""
	task.FinishedAt = &now
	task.UpdatedAt = now
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Task{}).Where("id = ? AND status = ?", id, models.TaskStatusRunning).Updates(map[string]any{
			"status":      models.TaskStatusSucceeded,
			"progress":    100,
			"error":       "",
			"finished_at": now,
			"updated_at":  now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("仅 running 任务可标记成功")
		}
		return s.recordAuditTx(ctx, tx, task, "task.succeeded", "")
	})
}

// MarkFailed 将 running 任务标记为 failed，或在剩余尝试次数内回到 pending。
func (s *Service) MarkFailed(ctx context.Context, id int64, reason string) error {
	task, err := s.getByID(ctx, id)
	if err != nil {
		return err
	}
	if task.Status != models.TaskStatusRunning {
		return fmt.Errorf("仅 running 任务可标记失败")
	}
	now := s.now().UTC()
	attempts := task.Attempts + 1
	status := models.TaskStatusFailed
	var startedAt any = task.StartedAt
	var finishedAt any = now
	var nextRunAt any
	if attempts < task.MaxAttempts {
		status = models.TaskStatusPending
		startedAt = nil
		finishedAt = nil
		next := now.Add(retryBackoff(attempts))
		nextRunAt = next
	}
	task.Status = status
	task.Attempts = attempts
	task.Error = strings.TrimSpace(reason)
	task.UpdatedAt = now
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Task{}).Where("id = ? AND status = ?", id, models.TaskStatusRunning).Updates(map[string]any{
			"status":      status,
			"attempts":    attempts,
			"error":       strings.TrimSpace(reason),
			"started_at":  startedAt,
			"finished_at": finishedAt,
			"next_run_at": nextRunAt,
			"updated_at":  now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("仅 running 任务可标记失败")
		}
		if status != models.TaskStatusFailed {
			return nil
		}
		task.FinishedAt = &now
		return s.recordAuditTx(ctx, tx, task, "task.failed", task.Error)
	})
}

// RecoverRunning 将进程重启后遗留的 running 任务恢复为 pending。
func (s *Service) RecoverRunning(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().UTC()
	return s.db.WithContext(ctx).Model(&models.Task{}).
		Where("status = ?", models.TaskStatusRunning).
		Updates(map[string]any{
			"status":      models.TaskStatusPending,
			"started_at":  nil,
			"next_run_at": nil,
			"updated_at":  now,
		}).Error
}

func (s *Service) bindCancellation(id int64, cancel context.CancelFunc) func() {
	token := new(byte)
	s.cancelMu.Lock()
	s.runningCancels[id] = runningCancellation{token: token, cancel: cancel}
	s.cancelMu.Unlock()
	return func() {
		s.cancelMu.Lock()
		if current, ok := s.runningCancels[id]; ok && current.token == token {
			delete(s.runningCancels, id)
		}
		s.cancelMu.Unlock()
	}
}

func (s *Service) signalCancellation(id int64) {
	s.cancelMu.Lock()
	current, ok := s.runningCancels[id]
	s.cancelMu.Unlock()
	if ok {
		current.cancel()
	}
}

func (s *Service) isCanceled(ctx context.Context, id int64) (bool, error) {
	task, err := s.getByID(ctx, id)
	if err != nil {
		return false, err
	}
	return task.Status == models.TaskStatusCanceled, nil
}

func findUnfinishedByKeyDB(ctx context.Context, db *gorm.DB, key string) (models.Task, bool, error) {
	var task models.Task
	err := db.WithContext(ctx).
		Where("idempotency_key = ? AND status IN ?", key, []string{models.TaskStatusPending, models.TaskStatusRunning}).
		Order("created_at ASC, id ASC").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Task{}, false, nil
	}
	if err != nil {
		return models.Task{}, false, err
	}
	return task, true, nil
}

func (s *Service) applyQuery(db *gorm.DB, query Query) (*gorm.DB, error) {
	scope := strings.TrimSpace(query.Scope)
	if scope == models.TaskScopeSystem {
		db = db.Where("scope = ? AND space_id IS NULL", models.TaskScopeSystem)
	} else {
		spaceID := strings.TrimSpace(query.SpaceID)
		if spaceID == "" {
			spaceID = models.DefaultSpaceID
		}
		db = db.Where("scope = ? AND space_id = ?", models.TaskScopeSpace, spaceID)
	}
	if query.Type != "" {
		db = db.Where("type = ?", strings.TrimSpace(query.Type))
	}
	if query.Status != "" {
		status, err := NormalizeStatus(query.Status)
		if err != nil {
			return nil, err
		}
		db = db.Where("status = ?", status)
	}
	if query.ResourceType != "" {
		db = db.Where("resource_type = ?", strings.TrimSpace(query.ResourceType))
	}
	if query.ResourceID != "" {
		db = db.Where("resource_id = ?", strings.TrimSpace(query.ResourceID))
	}
	return db, nil
}

func (s *Service) getScoped(ctx context.Context, id int64, query Query) (*models.Task, error) {
	db, err := s.applyQuery(s.db.WithContext(ctx).Model(&models.Task{}), query)
	if err != nil {
		return nil, err
	}
	var task models.Task
	err = db.Where("id = ?", id).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *Service) getByID(ctx context.Context, id int64) (*models.Task, error) {
	var task models.Task
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func normalizeScope(scope, spaceID string) (string, *string, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = models.TaskScopeSpace
	}
	switch scope {
	case models.TaskScopeSpace:
		spaceID = strings.TrimSpace(spaceID)
		if spaceID == "" {
			spaceID = models.DefaultSpaceID
		}
		return scope, &spaceID, nil
	case models.TaskScopeSystem:
		if strings.TrimSpace(spaceID) != "" {
			return "", nil, errors.New("system 任务不能包含 space_id")
		}
		return scope, nil, nil
	default:
		return "", nil, fmt.Errorf("任务 scope 无效: %s", scope)
	}
}

func normalizePage(query Query) (int, int) {
	page := query.Page
	if page <= 0 {
		page = 1
	}
	size := query.PageSize
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func retryBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return retryBackoffBase
	}
	if attempts > 6 {
		attempts = 6
	}
	return time.Duration(1<<uint(attempts-1)) * retryBackoffBase
}

func (s *Service) recordAuditTx(ctx context.Context, tx *gorm.DB, task *models.Task, action, errText string) error {
	if s.audit == nil {
		return nil
	}
	input := audit.EventInput{
		Scope:        audit.ScopeSystem,
		ActorType:    audit.ActorSystem,
		Action:       action,
		ResourceType: "task",
		ResourceID:   fmt.Sprintf("%d", task.ID),
		Metadata: map[string]any{
			"task_type": task.Type,
			"status":    task.Status,
		},
	}
	if task.Scope == models.TaskScopeSpace {
		input.Scope = audit.ScopeSpace
		if task.SpaceID != nil {
			input.SpaceID = *task.SpaceID
		}
	}
	if errText != "" {
		input.Metadata = map[string]any{
			"task_type": task.Type,
			"status":    task.Status,
			"error":     errText,
		}
	}
	return s.audit.RecordTx(ctx, tx, input)
}

func firstPending(db *gorm.DB) (models.Task, error) {
	var task models.Task
	err := db.Order("priority DESC, created_at ASC, id ASC").First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Task{}, ErrNoPendingTask
	}
	if err != nil {
		return models.Task{}, err
	}
	return task, nil
}

func oldestLowerPriority(db *gorm.DB, topPriority int) (models.Task, bool) {
	var task models.Task
	err := db.Where("priority < ?", topPriority).
		Order("created_at ASC, id ASC").
		First(&task).Error
	if err != nil {
		return models.Task{}, false
	}
	return task, true
}
