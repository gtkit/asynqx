package asynqx

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gtkit/json"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	workerStateIdle int32 = iota
	workerStateStarting
	workerStateRunning
	workerStateStopping
	workerStateStopped
)

// Worker 负责消费 asynq 任务并管理处理器注册与运行生命周期。
type Worker struct {
	cfg           Config
	mux           *asynq.ServeMux
	runner        workerRunner
	handlers      sync.Map
	registrations activityCounter
	state         atomic.Int32
	stopped       chan struct{}
	stoppedOnce   sync.Once
	stopOnce      sync.Once
}

type workerRunner interface {
	Start(handler asynq.Handler) error
	Shutdown()
}

type workerRunnerFactory func(Config) (workerRunner, error)

// NewWorker 基于共享配置创建 Worker，并初始化底层 asynq 执行器。
func NewWorker(opts ...WorkerOption) (*Worker, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	return newWorker(cfg, defaultWorkerRunnerFactory)
}

func newWorker(cfg Config, factory workerRunnerFactory) (*Worker, error) {
	if factory == nil {
		return nil, invalidConfigurationError("worker.runner_factory", "must not be nil")
	}

	runner, err := factory(cfg.clone())
	if err != nil {
		return nil, err
	}

	if runner == nil {
		return nil, invalidConfigurationError("worker.runner", "must not be nil")
	}

	mux := asynq.NewServeMux()
	if len(cfg.Middleware) > 0 {
		mux.Use(cfg.Middleware...)
	}

	worker := &Worker{
		cfg:           cfg.clone(),
		mux:           mux,
		runner:        runner,
		registrations: newActivityCounter(),
		stopped:       make(chan struct{}),
	}
	worker.state.Store(workerStateIdle)

	return worker, nil
}

var defaultWorkerRunnerFactory = func(cfg Config) (workerRunner, error) {
	redisClient, err := newRedisUniversalClient(cfg.Redis)
	if err != nil {
		return nil, err
	}

	runner := asynq.NewServerFromRedisClient(redisClient, cfg.asynqConfig())
	if runner == nil {
		closeErr := redisClient.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("%w: %w", invalidConfigurationError("worker.runner", "must not be nil"), closeErr)
		}

		return nil, invalidConfigurationError("worker.runner", "must not be nil")
	}

	return &managedWorkerRunner{runner: runner, redisClient: redisClient}, nil
}

type managedWorkerRunner struct {
	runner      *asynq.Server
	redisClient redis.UniversalClient
	closeOnce   sync.Once
}

func (r *managedWorkerRunner) Start(handler asynq.Handler) error {
	return r.runner.Start(handler)
}

func (r *managedWorkerRunner) Shutdown() {
	r.closeOnce.Do(func() {
		r.runner.Shutdown()
		_ = r.redisClient.Close()
	})
}

// HandleRaw 在 Worker 启动前注册原始 asynq 处理器。
func (w *Worker) HandleRaw(taskType string, handler func(context.Context, *asynq.Task) error) error {
	if w == nil {
		return invalidArgumentError("worker", "must not be nil")
	}

	if strings.TrimSpace(taskType) == "" {
		return invalidArgumentError("task_type", "must not be empty")
	}

	if handler == nil {
		return invalidArgumentError("handler", "must not be nil")
	}

	err := w.beginRegistration()
	if err != nil {
		return err
	}

	defer w.endRegistration()

	if _, loaded := w.handlers.LoadOrStore(taskType, struct{}{}); loaded {
		return ErrHandlerAlreadyRegistered
	}

	w.mux.HandleFunc(taskType, handler)

	return nil
}

// Handle 注册带泛型 payload 解码的处理器。
func Handle[T any](worker *Worker, taskType string, handler func(context.Context, T) error) error {
	if worker == nil {
		return invalidArgumentError("worker", "must not be nil")
	}

	if handler == nil {
		return invalidArgumentError("handler", "must not be nil")
	}

	return worker.HandleRaw(taskType, func(ctx context.Context, task *asynq.Task) error {
		var payload T

		err := json.Unmarshal(task.Payload(), &payload)
		if err != nil {
			return fmt.Errorf("unmarshal task payload: %w: %w", err, asynq.SkipRetry)
		}

		return handler(ctx, payload)
	})
}

// Start 启动 Worker 底层执行器；成功后将拒绝新的处理器注册。
func (w *Worker) Start(ctx context.Context) error {
	if w == nil {
		return invalidArgumentError("worker", "must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	err := ctx.Err()
	if err != nil {
		return err
	}

	if !w.state.CompareAndSwap(workerStateIdle, workerStateStarting) {
		switch w.state.Load() {
		case workerStateStopped, workerStateStopping:
			return ErrWorkerStopped
		default:
			return ErrWorkerAlreadyRunning
		}
	}

	w.registrations.Wait()

	err = ctx.Err()
	if err != nil {
		if w.state.CompareAndSwap(workerStateStarting, workerStateIdle) {
			return err
		}

		if w.state.Load() == workerStateStopping {
			w.markStopped()

			return ErrWorkerStopped
		}

		return err
	}

	err = w.runner.Start(w.mux)
	if err != nil {
		if w.state.Load() == workerStateStopping {
			w.markStopped()

			return ErrWorkerStopped
		}

		w.state.Store(workerStateIdle)

		return err
	}

	if w.state.CompareAndSwap(workerStateStarting, workerStateRunning) {
		return nil
	}

	if w.state.Load() == workerStateStopping {
		w.beginStop(false)

		return ErrWorkerStopped
	}

	return nil
}

// Run 启动 Worker，并在 ctx 取消后触发 Shutdown。
// 如果 ctx 取消后关闭成功，Run 返回 nil；调用方需要区分退出原因时应读取 ctx.Err()。
// Run 触发的关闭流程使用 Config.ShutdownTimeout 作为默认等待预算。
func (w *Worker) Run(ctx context.Context) error {
	if w == nil {
		return invalidArgumentError("worker", "must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	err := w.Start(ctx)
	if err != nil {
		return err
	}

	<-ctx.Done()

	shutdownCtx, cancel := w.shutdownContext()
	defer cancel()

	return w.Shutdown(shutdownCtx)
}

// Shutdown 关闭 Worker，重复调用是安全的。
func (w *Worker) Shutdown(ctx context.Context) error {
	if w == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	for {
		switch w.state.Load() {
		case workerStateIdle:
			if w.state.CompareAndSwap(workerStateIdle, workerStateStopping) {
				w.beginStop(false)

				return nil
			}
		case workerStateStarting:
			if w.state.CompareAndSwap(workerStateStarting, workerStateStopping) {
				return w.waitStopped(ctx)
			}
		case workerStateRunning:
			if w.state.CompareAndSwap(workerStateRunning, workerStateStopping) {
				w.beginStop(true)

				return w.waitStopped(ctx)
			}
		case workerStateStopping, workerStateStopped:
			return w.waitStopped(ctx)
		}
	}
}

func (w *Worker) beginRegistration() error {
	switch w.state.Load() {
	case workerStateStopping, workerStateStopped:
		return ErrWorkerStopped
	case workerStateIdle:
	default:
		return ErrWorkerAlreadyRunning
	}

	w.registrations.Add()

	switch w.state.Load() {
	case workerStateStopping, workerStateStopped:
		w.registrations.Done()

		return ErrWorkerStopped
	case workerStateIdle:
		return nil
	default:
		w.registrations.Done()

		return ErrWorkerAlreadyRunning
	}
}

func (w *Worker) endRegistration() {
	w.registrations.Done()
}

func (w *Worker) beginStop(async bool) {
	w.stopOnce.Do(func() {
		if async {
			go w.finishStop()

			return
		}

		w.finishStop()
	})
}

func (w *Worker) finishStop() {
	w.runner.Shutdown()
	w.markStopped()
}

func (w *Worker) markStopped() {
	w.state.Store(workerStateStopped)
	w.stoppedOnce.Do(func() {
		close(w.stopped)
	})
}

func (w *Worker) waitStopped(ctx context.Context) error {
	if w.state.Load() == workerStateStopped {
		return nil
	}

	select {
	case <-w.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) shutdownContext() (context.Context, context.CancelFunc) {
	if w.cfg.ShutdownTimeout <= 0 {
		return context.Background(), func() {}
	}

	return context.WithTimeout(context.Background(), w.cfg.ShutdownTimeout)
}
