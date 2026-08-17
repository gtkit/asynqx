package asynqx

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gtkit/json/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// Worker 负责消费 asynq 任务并管理处理器注册与运行生命周期。
type Worker struct {
	lifecycle

	cfg           Config
	mux           *asynq.ServeMux
	runner        workerRunner
	handlers      sync.Map
	registrations activityCounter
}

type workerRunner interface {
	Start(handler asynq.Handler) error
	Shutdown()
}

type workerRunnerFactory func(Config) (workerRunner, error)

// NewWorker 基于共享配置创建 Worker，并初始化底层 asynq 执行器。
// 调用成功后，即使从未调用 Start，调用方也应调用 Shutdown 释放底层资源。
func NewWorker(opts ...WorkerOption) (*Worker, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	return newWorker(cfg, defaultWorkerRunnerFactory)
}

// NewWorkerFromConfig 基于已构造的共享配置创建 Worker，并初始化底层 asynq 执行器。
// 调用成功后，即使从未调用 Start，调用方也应调用 Shutdown 释放底层资源。
func NewWorkerFromConfig(cfg Config) (*Worker, error) {
	cfg = cfg.clone()

	err := cfg.validate()
	if err != nil {
		return nil, err
	}

	return newWorker(cfg, defaultWorkerRunnerFactory)
}

func newWorker(cfg Config, factory workerRunnerFactory) (*Worker, error) {
	if factory == nil {
		return nil, invalidConfigurationError("worker.runner_factory", "must not be nil")
	}

	runner, err := factory(cfg)
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
	}
	worker.init(ErrWorkerAlreadyRunning, ErrWorkerStopped, worker.runner.Shutdown)

	return worker, nil
}

var defaultWorkerRunnerFactory = func(cfg Config) (workerRunner, error) {
	redisClient, ownsClient, err := resolveRedisClient(cfg)
	if err != nil {
		return nil, err
	}

	if cfg.PingOnStart {
		err = pingRedisOnStart(context.Background(), redisClient, cfg.PingTimeout)
		if err != nil {
			if ownsClient {
				_ = redisClient.Close()
			}

			return nil, err
		}
	}

	runner := asynq.NewServerFromRedisClient(redisClient, cfg.asynqConfig())
	if runner == nil {
		if ownsClient {
			closeErr := redisClient.Close()
			if closeErr != nil {
				return nil, fmt.Errorf("%w: %w", invalidConfigurationError("worker.runner", "must not be nil"), closeErr)
			}
		}

		return nil, invalidConfigurationError("worker.runner", "must not be nil")
	}

	return &managedWorkerRunner{runner: runner, redisClient: redisClient, ownsClient: ownsClient}, nil
}

type managedWorkerRunner struct {
	runner      *asynq.Server
	redisClient redis.UniversalClient
	ownsClient  bool
	closeOnce   sync.Once
}

func (r *managedWorkerRunner) Start(handler asynq.Handler) error {
	return r.runner.Start(handler)
}

func (r *managedWorkerRunner) Shutdown() {
	r.closeOnce.Do(func() {
		r.runner.Shutdown()

		if r.ownsClient {
			_ = r.redisClient.Close()
		}
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

// Handle 通过任意 Registrar（*Worker 或 *App）注册带泛型 payload 解码的处理器。
func Handle[T any](registrar Registrar, taskType string, handler func(context.Context, T) error) error {
	if isNilInterface(registrar) {
		return invalidArgumentError("registrar", "must not be nil")
	}

	if handler == nil {
		return invalidArgumentError("handler", "must not be nil")
	}

	return registrar.HandleRaw(taskType, func(ctx context.Context, task *asynq.Task) error {
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

	return w.start(
		func() error {
			// 等待在途的处理器注册完成，再确认 ctx 是否仍有效。
			w.registrations.Wait()

			return ctx.Err()
		},
		func() error {
			return w.runner.Start(w.mux)
		},
	)
}

// Run 启动 Worker，并在 ctx 取消后触发 Shutdown。
// 如果 ctx 取消后关闭成功，Run 返回 nil；调用方需要区分退出原因时应读取 ctx.Err()。
// Run 触发的关闭流程以 Config.ShutdownTimeout 加余量（其 10%，上限 5 秒）作为默认
// 等待预算，保证底层任务收尾用满窗口时外层等待不至于先行超时。
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

	return w.shutdown(ctx)
}

func (w *Worker) beginRegistration() error {
	switch w.state.Load() {
	case stateStopping, stateStopped:
		return ErrWorkerStopped
	case stateIdle:
	default:
		return ErrWorkerAlreadyRunning
	}

	w.registrations.Add()

	switch w.state.Load() {
	case stateStopping, stateStopped:
		w.registrations.Done()

		return ErrWorkerStopped
	case stateIdle:
		return nil
	default:
		w.registrations.Done()

		return ErrWorkerAlreadyRunning
	}
}

func (w *Worker) endRegistration() {
	w.registrations.Done()
}

func (w *Worker) shutdownContext() (context.Context, context.CancelFunc) {
	return newShutdownContext(w.cfg.ShutdownTimeout)
}
