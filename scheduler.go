package asynqx

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	schedulerStateIdle int32 = iota
	schedulerStateStarting
	schedulerStateRunning
	schedulerStateStopping
	schedulerStateStopped
)

// Scheduler 负责管理周期任务的注册、启动与关闭生命周期。
type Scheduler struct {
	cfg              Config
	runner           schedulerRunner
	state            atomic.Int32
	activeOperations activityCounter
	stopped          chan struct{}
	stoppedOnce      sync.Once
	stopOnce         sync.Once
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
		stopped:          make(chan struct{}),
	}
	scheduler.state.Store(schedulerStateIdle)

	return scheduler, nil
}

var defaultSchedulerRunnerFactory = func(cfg Config) (schedulerRunner, error) {
	redisClient, err := newRedisUniversalClient(cfg.Redis)
	if err != nil {
		return nil, err
	}

	if cfg.PingOnStart {
		err = pingRedisOnStart(context.Background(), redisClient)
		if err != nil {
			_ = redisClient.Close()

			return nil, err
		}
	}

	runner := asynq.NewSchedulerFromRedisClient(redisClient, cfg.schedulerOptions())
	if runner == nil {
		closeErr := redisClient.Close()
		if closeErr != nil {
			return nil, invalidConfigurationError("scheduler.runner", closeErr.Error())
		}

		return nil, invalidConfigurationError("scheduler.runner", "must not be nil")
	}

	return &managedSchedulerRunner{runner: runner, redisClient: redisClient}, nil
}

type managedSchedulerRunner struct {
	runner      *asynq.Scheduler
	redisClient redis.UniversalClient
	closeOnce   sync.Once
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
	r.closeOnce.Do(func() {
		r.runner.Shutdown()
		_ = r.redisClient.Close()
	})
}

// Register 注册一个周期任务，并返回底层调度器生成的 entryID。
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

	if !s.state.CompareAndSwap(schedulerStateIdle, schedulerStateStarting) {
		switch s.state.Load() {
		case schedulerStateStopping, schedulerStateStopped:
			return ErrSchedulerStopped
		default:
			return ErrSchedulerAlreadyRunning
		}
	}

	err = s.runner.Start()
	if err != nil {
		if s.state.Load() == schedulerStateStopping {
			s.beginStop(false)

			return ErrSchedulerStopped
		}

		s.state.Store(schedulerStateIdle)

		return err
	}

	if s.state.CompareAndSwap(schedulerStateStarting, schedulerStateRunning) {
		return nil
	}

	if s.state.Load() == schedulerStateStopping {
		s.beginStop(false)

		return ErrSchedulerStopped
	}

	return nil
}

// Run 启动调度器，并在 ctx 取消后触发 Shutdown。
// 如果 ctx 取消后关闭成功，Run 返回 nil；调用方需要区分退出原因时应读取 ctx.Err()。
// Run 触发的关闭流程使用 Config.ShutdownTimeout 作为默认等待预算。
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

	for {
		switch s.state.Load() {
		case schedulerStateIdle:
			if s.state.CompareAndSwap(schedulerStateIdle, schedulerStateStopping) {
				s.beginStop(false)

				return nil
			}
		case schedulerStateStarting:
			if s.state.CompareAndSwap(schedulerStateStarting, schedulerStateStopping) {
				return s.waitStopped(ctx)
			}
		case schedulerStateRunning:
			if s.state.CompareAndSwap(schedulerStateRunning, schedulerStateStopping) {
				s.beginStop(true)

				return s.waitStopped(ctx)
			}
		case schedulerStateStopping, schedulerStateStopped:
			return s.waitStopped(ctx)
		}
	}
}

func (s *Scheduler) beginOperation() error {
	switch s.state.Load() {
	case schedulerStateIdle, schedulerStateStarting, schedulerStateRunning:
	default:
		return ErrSchedulerStopped
	}

	s.activeOperations.Add()

	switch s.state.Load() {
	case schedulerStateIdle, schedulerStateStarting, schedulerStateRunning:
		return nil
	default:
		s.activeOperations.Done()

		return ErrSchedulerStopped
	}
}

func (s *Scheduler) endOperation() {
	s.activeOperations.Done()
}

func (s *Scheduler) beginStop(async bool) {
	s.stopOnce.Do(func() {
		if async {
			go s.finishStop()

			return
		}

		s.finishStop()
	})
}

func (s *Scheduler) finishStop() {
	s.activeOperations.Wait()
	s.runner.Shutdown()
	s.state.Store(schedulerStateStopped)
	s.stoppedOnce.Do(func() {
		close(s.stopped)
	})
}

func (s *Scheduler) waitStopped(ctx context.Context) error {
	if s.state.Load() == schedulerStateStopped {
		return nil
	}

	select {
	case <-s.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scheduler) shutdownContext() (context.Context, context.CancelFunc) {
	if s.cfg.ShutdownTimeout <= 0 {
		return context.Background(), func() {}
	}

	return context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
}
