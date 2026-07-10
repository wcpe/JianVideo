package tasks

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const defaultWorkerPollInterval = 200 * time.Millisecond

// Handler 是通用任务 worker 的业务执行函数。
type Handler func(context.Context, models.Task) error

type workerSpec struct {
	taskType    string
	concurrency int
	handler     Handler
}

// WorkerRegistry 按任务类型注册处理器，并按各类型并发上限领取执行 pending 任务。
type WorkerRegistry struct {
	service     *Service
	mu          sync.Mutex
	runMu       sync.Mutex
	wakeMu      sync.Mutex
	wakeRunning bool
	wakePending bool
	specs       map[string]workerSpec
}

// NewWorkerRegistry 创建通用任务 worker 注册表。
func NewWorkerRegistry(service *Service) *WorkerRegistry {
	return &WorkerRegistry{
		service: service,
		specs:   map[string]workerSpec{},
	}
}

// Register 注册指定任务类型的处理器和并发上限。
func (r *WorkerRegistry) Register(taskType string, concurrency int, handler Handler) error {
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return errors.New("任务类型不能为空")
	}
	if concurrency <= 0 {
		return errors.New("worker 并发上限必须大于 0")
	}
	if handler == nil {
		return errors.New("worker 处理器不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs[taskType] = workerSpec{taskType: taskType, concurrency: concurrency, handler: handler}
	return nil
}

// UpdateProgress 更新当前 worker 任务的进度和检查点。
func (r *WorkerRegistry) UpdateProgress(ctx context.Context, taskID int64, input ProgressInput) error {
	if r == nil || r.service == nil {
		return errors.New("worker 注册表未初始化")
	}
	return r.service.UpdateProgress(ctx, taskID, input)
}

// Wake 异步唤醒注册表；并发信号只合并为一次补跑，不堆积等待 goroutine。
func (r *WorkerRegistry) Wake() {
	if r == nil {
		return
	}
	r.wakeMu.Lock()
	if r.wakeRunning {
		r.wakePending = true
		r.wakeMu.Unlock()
		return
	}
	r.wakeRunning = true
	r.wakeMu.Unlock()
	go r.wakeLoop()
}

func (r *WorkerRegistry) wakeLoop() {
	for {
		if err := r.RunPending(context.Background()); err != nil {
			log.Printf("[ERROR] 通用任务 worker 执行失败: %v", err)
		}
		r.wakeMu.Lock()
		if r.wakePending {
			r.wakePending = false
			r.wakeMu.Unlock()
			continue
		}
		r.wakeRunning = false
		r.wakeMu.Unlock()
		return
	}
}

// RunPending 领取并执行当前所有已到期的 pending 任务。
func (r *WorkerRegistry) RunPending(ctx context.Context) error {
	r.runMu.Lock()
	defer r.runMu.Unlock()

	specs := r.snapshot()
	errs := make(chan error, len(specs))
	var wg sync.WaitGroup
	for _, spec := range specs {
		wg.Add(1)
		go func(spec workerSpec) {
			defer wg.Done()
			errs <- r.runType(ctx, spec)
		}(spec)
	}
	wg.Wait()
	close(errs)
	return firstWorkerError(errs)
}

// Run 持续轮询并执行新入队任务，直到上下文取消。
func (r *WorkerRegistry) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = defaultWorkerPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := r.RunPending(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// DefaultConcurrency 返回首批任务类型的默认并发上限。
func DefaultConcurrency(taskType string) int {
	taskType = strings.TrimSpace(taskType)
	switch {
	case taskType == "library.scan":
		return 1
	case taskType == "library.file_hash_backfill":
		return 1
	case strings.HasPrefix(taskType, "transcode."):
		return 1
	case strings.HasPrefix(taskType, "thumbnail."):
		return 4
	default:
		return 2
	}
}

func (r *WorkerRegistry) snapshot() []workerSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.specs))
	for key := range r.specs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	specs := make([]workerSpec, 0, len(keys))
	for _, key := range keys {
		specs = append(specs, r.specs[key])
	}
	return specs
}

func (r *WorkerRegistry) runType(ctx context.Context, spec workerSpec) error {
	errs := make(chan error, spec.concurrency)
	var wg sync.WaitGroup
	for i := 0; i < spec.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- r.workerLoop(ctx, spec)
		}()
	}
	wg.Wait()
	close(errs)
	return firstWorkerError(errs)
}

func (r *WorkerRegistry) workerLoop(ctx context.Context, spec workerSpec) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		task, err := r.service.ClaimNext(ctx, ClaimQuery{Type: spec.taskType})
		if errors.Is(err, ErrNoPendingTask) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := r.finishTask(ctx, spec, task); err != nil {
			return err
		}
	}
}

func (r *WorkerRegistry) finishTask(ctx context.Context, spec workerSpec, task *models.Task) error {
	taskCtx, cancel := context.WithCancel(ctx)
	unbind := r.service.bindCancellation(task.ID, cancel)
	defer func() {
		unbind()
		cancel()
	}()
	if canceled, err := r.service.isCanceled(ctx, task.ID); err != nil || canceled {
		return err
	}
	taskErr := spec.handler(taskCtx, *task)
	if taskCtx.Err() != nil {
		return nil
	}
	if taskErr != nil {
		return r.finishFailedTask(ctx, task.ID, taskErr)
	}
	if err := r.service.MarkSucceeded(ctx, task.ID); err != nil {
		if canceled, checkErr := r.service.isCanceled(ctx, task.ID); checkErr == nil && canceled {
			return nil
		}
		return err
	}
	return nil
}

func (r *WorkerRegistry) finishFailedTask(ctx context.Context, taskID int64, taskErr error) error {
	if canceled, err := r.service.isCanceled(ctx, taskID); err != nil || canceled {
		return err
	}
	if err := r.service.MarkFailed(ctx, taskID, taskErr.Error()); err != nil {
		if canceled, checkErr := r.service.isCanceled(ctx, taskID); checkErr == nil && canceled {
			return nil
		}
		return err
	}
	return nil
}

func firstWorkerError(errs <-chan error) error {
	for err := range errs {
		if err != nil {
			return fmt.Errorf("worker 执行失败: %w", err)
		}
	}
	return nil
}
