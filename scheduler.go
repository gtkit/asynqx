package asynqx

import (
	"context"
	"strings"
	"sync"

	"github.com/hibiken/asynq"
)

// Scheduler 负责管理周期任务的注册、启动与关闭生命周期。
type Scheduler struct {
	lifecycle

	cfg              Config
	runner           schedulerRunner
	activeOperations activityCounter
}

type schedulerRunner interface {
	Register(spec string, task *asynq.Task, opts ...asynq.Option) (string, error)
	Unregister(entryID string) error
	Start() error
	Shutdown()
}

type schedulerRunnerFactory func(Config) (schedulerRunner, error)

// NewScheduler 基于共享配置创建 Scheduler，并初始化底层 asynq 调度器。
// 调用成功后，即使从未调用 Start，调用方也应调用 Shutdown 释放底层资源。
func NewScheduler(opts ...SchedulerOption) (*Scheduler, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	return newScheduler(cfg, defaultSchedulerRunnerFactory)
}

// NewSchedulerFromConfig 基于已构造的共享配置创建 Scheduler，并初始化底层 asynq 调度器。
// 调用成功后，即使从未调用 Start，调用方也应调用 Shutdown 释放底层资源。
func NewSchedulerFromConfig(cfg Config) (*Scheduler, error) {
	cfg = cfg.clone()

	err := cfg.validate()
	if err != nil {
		return nil, err
	}

	return newScheduler(cfg, defaultSchedulerRunnerFactory)
}

func newScheduler(cfg Config, factory schedulerRunnerFactory) (*Scheduler, error) {
	if factory == nil {
		return nil, invalidConfigurationError("scheduler.runner_factory", "must not be nil")
	}

	runner, err := factory(cfg)
	if err != nil {
		return nil, err
	}

	if runner == nil {
		return nil, invalidConfigurationError("scheduler.runner", "must not be nil")
	}

	scheduler := &Scheduler{
		cfg:              cfg.clone(),
		runner:           runner,
		activeOperations: newActivityCounter(),
	}
	scheduler.init(ErrSchedulerAlreadyRunning, ErrSchedulerStopped, func() {
		// 先等待在途的 Register/Unregister 完成，再关闭底层调度器。
		scheduler.activeOperations.Wait()
		scheduler.runner.Shutdown()
	})

	return scheduler, nil
}

var defaultSchedulerRunnerFactory = func(cfg Config) (schedulerRunner, error) {
	// 复用外部共享客户端：走 NewSchedulerFromRedisClient，连接生命周期由调用方负责，
	// asynqx 不会关闭它。
	if !isNilInterface(cfg.RedisClient) {
		if cfg.PingOnStart {
			err := pingRedisOnStart(context.Background(), cfg.RedisClient, cfg.PingTimeout)
			if err != nil {
				return nil, err
			}
		}

		runner := asynq.NewSchedulerFromRedisClient(cfg.RedisClient, cfg.schedulerOptions())
		if runner == nil {
			return nil, invalidConfigurationError("scheduler.runner", "must not be nil")
		}

		return &managedSchedulerRunner{runner: runner}, nil
	}

	// 由 asynqx 按连接参数创建：交给 asynq.NewScheduler 自建并在 Shutdown 时干净关闭其
	// 内部客户端。asynq 的 Scheduler.Shutdown 不像 Server.Shutdown 那样对共享连接做保护，
	// 若改用 NewSchedulerFromRedisClient，每次关闭都会触发 "redis connection is shared"
	// 错误日志；此路径可彻底避免该噪音。
	if cfg.PingOnStart {
		err := pingRedisOptionOnStart(context.Background(), cfg.Redis, cfg.PingTimeout)
		if err != nil {
			return nil, err
		}
	}

	runner := asynq.NewScheduler(cfg.Redis, cfg.schedulerOptions())
	if runner == nil {
		return nil, invalidConfigurationError("scheduler.runner", "must not be nil")
	}

	return &managedSchedulerRunner{runner: runner}, nil
}

type managedSchedulerRunner struct {
	runner    *asynq.Scheduler
	closeOnce sync.Once
}

func (r *managedSchedulerRunner) Register(spec string, task *asynq.Task, opts ...asynq.Option) (string, error) {
	return r.runner.Register(spec, task, opts...)
}

func (r *managedSchedulerRunner) Unregister(entryID string) error {
	return r.runner.Unregister(entryID)
}

func (r *managedSchedulerRunner) Start() error {
	return r.runner.Start()
}

func (r *managedSchedulerRunner) Shutdown() {
	// 底层客户端不由 managedSchedulerRunner 关闭：外部共享客户端由调用方负责，
	// asynqx 自建的客户端由 asynq.Scheduler 在其 Shutdown 中自行关闭。
	r.closeOnce.Do(r.runner.Shutdown)
}

// Register 注册一个周期任务，并返回底层调度器生成的 entryID。
// payload 会被序列化为 JSON 后随任务投递。
func (s *Scheduler) Register(
	ctx context.Context,
	spec string,
	taskType string,
	payload any,
	opts ...TaskOption,
) (string, error) {
	if s == nil {
		return "", invalidArgumentError("scheduler", "must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	if strings.TrimSpace(spec) == "" {
		return "", invalidArgumentError("spec", "must not be empty")
	}

	if strings.TrimSpace(taskType) == "" {
		return "", invalidArgumentError("task_type", "must not be empty")
	}

	if err := s.beginOperation(); err != nil {
		return "", err
	}

	defer s.endOperation()

	if err := ctx.Err(); err != nil {
		return "", err
	}

	body, err := marshalPayload(payload)
	if err != nil {
		return "", err
	}

	asynqOpts, err := buildTaskOptions(opts...)
	if err != nil {
		return "", err
	}

	asynqOpts = applyDefaultTaskTimeout(asynqOpts, s.cfg.TaskTimeout)

	task := asynq.NewTask(taskType, body)

	return s.runner.Register(spec, task, asynqOpts...)
}

// Unregister 按 entryID 移除一个已经注册的周期任务。
func (s *Scheduler) Unregister(ctx context.Context, entryID string) error {
	if s == nil {
		return invalidArgumentError("scheduler", "must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	err := ctx.Err()
	if err != nil {
		return err
	}

	if strings.TrimSpace(entryID) == "" {
		return invalidArgumentError("entry_id", "must not be empty")
	}

	err = s.beginOperation()
	if err != nil {
		return err
	}

	defer s.endOperation()

	err = ctx.Err()
	if err != nil {
		return err
	}

	return s.runner.Unregister(entryID)
}

// Start 启动底层调度器；成功后 Scheduler 进入运行态。
func (s *Scheduler) Start(ctx context.Context) error {
	if s == nil {
		return invalidArgumentError("scheduler", "must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	err := ctx.Err()
	if err != nil {
		return err
	}

	return s.start(s.runner.Start)
}

// Run 启动调度器，并在 ctx 取消后触发 Shutdown。
// 如果 ctx 取消后关闭成功，Run 返回 nil；调用方需要区分退出原因时应读取 ctx.Err()。
// Run 触发的关闭流程以 Config.ShutdownTimeout 加余量（其 10%，上限 5 秒）作为默认
// 等待预算。
func (s *Scheduler) Run(ctx context.Context) error {
	if s == nil {
		return invalidArgumentError("scheduler", "must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	err := s.Start(ctx)
	if err != nil {
		return err
	}

	<-ctx.Done()

	shutdownCtx, cancel := s.shutdownContext()
	defer cancel()

	return s.Shutdown(shutdownCtx)
}

// Shutdown 关闭调度器；重复调用是安全的。
func (s *Scheduler) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	return s.shutdown(ctx)
}

func (s *Scheduler) beginOperation() error {
	switch s.state.Load() {
	case stateIdle, stateStarting, stateRunning:
	default:
		return ErrSchedulerStopped
	}

	s.activeOperations.Add()

	switch s.state.Load() {
	case stateIdle, stateStarting, stateRunning:
		return nil
	default:
		s.activeOperations.Done()

		return ErrSchedulerStopped
	}
}

func (s *Scheduler) endOperation() {
	s.activeOperations.Done()
}

func (s *Scheduler) shutdownContext() (context.Context, context.CancelFunc) {
	return newShutdownContext(s.cfg.ShutdownTimeout)
}
