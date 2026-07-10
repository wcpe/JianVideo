package tasks

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/wcpe/JianVideo/internal/db/models"
)

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
	taskCtx, release := r.service.bindRunningContext(ctx, task.ID)
	defer release()
	if canceled, err := r.taskCanceled(ctx, task.ID); err != nil || canceled {
		return err
	}
	handlerErr := spec.handler(taskCtx, *task)
	if canceled, err := r.taskCanceled(ctx, task.ID); err != nil || canceled {
		return err
	}
	var err error
	if handlerErr != nil {
		err = r.service.MarkFailed(ctx, task.ID, handlerErr.Error())
	} else {
		err = r.service.MarkSucceeded(ctx, task.ID)
	}
	if err == nil {
		return nil
	}
	if canceled, statusErr := r.taskCanceled(ctx, task.ID); statusErr == nil && canceled {
		return nil
	}
	return err
}

func (r *WorkerRegistry) taskCanceled(ctx context.Context, taskID int64) (bool, error) {
	task, err := r.service.getByID(ctx, taskID)
	if err != nil {
		return false, err
	}
	return task.Status == models.TaskStatusCanceled, nil
}

func firstWorkerError(errs <-chan error) error {
	for err := range errs {
		if err != nil {
			return fmt.Errorf("worker 执行失败: %w", err)
		}
	}
	return nil
}
