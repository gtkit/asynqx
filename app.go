package asynqx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// AppOption 表示 App 使用的配置选项。
type AppOption = ConfigOption

// 编译期断言：App 与细粒度组件均满足任务定义所需的接口。
var (
	_ Enqueuer          = (*App)(nil)
	_ Registrar         = (*App)(nil)
	_ PeriodicRegistrar = (*App)(nil)
	_ Enqueuer          = (*Producer)(nil)
	_ Registrar         = (*Worker)(nil)
	_ PeriodicRegistrar = (*Scheduler)(nil)
)

// App 是"一份配置、一套连接、多种角色"的统一入口，适合绝大多数常规场景。
//
// App 在内部解析并持有一个共享的 Redis 连接池，并按需懒创建 Producer / Worker /
// Scheduler / Inspector——它们全部复用同一个连接池，因此调用方只需写一份配置。
// App 同时实现 Enqueuer / Registrar / PeriodicRegistrar，可直接配合 TaskType 使用：
//
//	app, _ := asynqx.New(asynqx.WithRedisAddr("127.0.0.1:6379"))
//	defer app.Close()
//
//	// 生产者：投递
//	WelcomeEmail.Enqueue(ctx, app, EmailPayload{UserID: "u-1"})
//
//	// 消费者：注册后运行（Run 阻塞至 ctx 取消并优雅关闭）
//	WelcomeEmail.Handle(app, func(ctx context.Context, p EmailPayload) error { return nil })
//	app.Run(ctx)
//
// 需要精细控制时，可用 Producer / Worker / Scheduler / Inspector 细粒度 API，
// 或通过 App 的同名方法获取已懒创建的底层组件。
//
// 注意：由于 App 让所有组件共享同一个连接池，Scheduler 在关闭时会由 asynq 内部打印
// 一条无害的 "redis connection is shared" 日志，连接本身不会被 Scheduler 关闭，
// 而是由 App 统一管理。
type App struct {
	cfg     Config
	rdb     redis.UniversalClient
	ownsRDB bool

	// mu 仅保护组件的懒创建；创建完成后组件指针存入 atomic.Pointer，
	// 读取（如每次 Enqueue）走无锁快路径，避免热路径上的锁竞争。
	mu        sync.Mutex
	producer  atomic.Pointer[Producer]
	worker    atomic.Pointer[Worker]
	scheduler atomic.Pointer[Scheduler]
	inspector atomic.Pointer[Inspector]

	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

// lazyComponent 返回 slot 中已创建的组件；未创建时在 app.mu 保护下按共享配置创建一次。
// 读路径无锁：已创建后仅做一次 atomic Load 与关闭标记检查。
func lazyComponent[T any](app *App, slot *atomic.Pointer[T], create func(Config) (*T, error)) (*T, error) {
	if app.closed.Load() {
		return nil, ErrClosed
	}

	if component := slot.Load(); component != nil {
		return component, nil
	}

	app.mu.Lock()
	defer app.mu.Unlock()

	if app.closed.Load() {
		return nil, ErrClosed
	}

	if component := slot.Load(); component != nil {
		return component, nil
	}

	component, err := create(app.cfg)
	if err != nil {
		return nil, err
	}

	slot.Store(component)

	return component, nil
}

// New 基于共享配置创建 App：内部解析并持有一个共享的 Redis 连接池。
// 使用完毕后应调用 Close（或 Shutdown）释放所有组件与连接。
func New(opts ...AppOption) (*App, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	return newApp(cfg)
}

// NewFromConfig 基于已构造的共享配置创建 App。
func NewFromConfig(cfg Config) (*App, error) {
	cfg = cfg.clone()

	err := cfg.validate()
	if err != nil {
		return nil, err
	}

	return newApp(cfg)
}

func newApp(cfg Config) (*App, error) {
	// 统一解析出一个共享连接池：外部客户端（WithRedisInstance）归调用方所有，
	// asynqx 自建则归 App 所有，由 App 在关闭时释放。
	rdb, ownsRDB, err := resolveRedisClient(cfg)
	if err != nil {
		return nil, err
	}

	// 让后续派生的所有组件都复用该连接池。
	cfg.RedisClient = rdb

	return &App{cfg: cfg, rdb: rdb, ownsRDB: ownsRDB}, nil
}

// Enqueue 通过 App 内部的 Producer 投递任务（首次调用时懒创建 Producer）。
// 实现 Enqueuer，可被 TaskType.Enqueue 使用。
func (a *App) Enqueue(
	ctx context.Context,
	taskType string,
	payload any,
	opts ...TaskOption,
) (*asynq.TaskInfo, error) {
	producer, err := a.Producer()
	if err != nil {
		return nil, err
	}

	return producer.Enqueue(ctx, taskType, payload, opts...)
}

// HandleRaw 通过 App 内部的 Worker 注册原始处理器（首次调用时懒创建 Worker）。
// 实现 Registrar，可被 Handle 与 TaskType.Handle 使用。
func (a *App) HandleRaw(taskType string, handler func(context.Context, *asynq.Task) error) error {
	worker, err := a.Worker()
	if err != nil {
		return err
	}

	return worker.HandleRaw(taskType, handler)
}

// Register 通过 App 内部的 Scheduler 注册周期任务（首次调用时懒创建 Scheduler）。
// 实现 PeriodicRegistrar，可被 TaskType.Register 使用。
func (a *App) Register(
	ctx context.Context,
	spec, taskType string,
	payload any,
	opts ...TaskOption,
) (string, error) {
	scheduler, err := a.Scheduler()
	if err != nil {
		return "", err
	}

	return scheduler.Register(ctx, spec, taskType, payload, opts...)
}

// Producer 返回 App 内部的 Producer，首次调用时按共享配置懒创建。
func (a *App) Producer() (*Producer, error) {
	if a == nil {
		return nil, ErrClosed
	}

	return lazyComponent(a, &a.producer, NewProducerFromConfig)
}

// Worker 返回 App 内部的 Worker，首次调用时按共享配置懒创建。
func (a *App) Worker() (*Worker, error) {
	if a == nil {
		return nil, ErrClosed
	}

	return lazyComponent(a, &a.worker, NewWorkerFromConfig)
}

// Scheduler 返回 App 内部的 Scheduler，首次调用时按共享配置懒创建。
func (a *App) Scheduler() (*Scheduler, error) {
	if a == nil {
		return nil, ErrClosed
	}

	return lazyComponent(a, &a.scheduler, NewSchedulerFromConfig)
}

// Inspector 返回 App 内部的 Inspector，首次调用时按共享配置懒创建。
func (a *App) Inspector() (*Inspector, error) {
	if a == nil {
		return nil, ErrClosed
	}

	return lazyComponent(a, &a.inspector, NewInspectorFromConfig)
}

// Ping 探测 App 持有的共享 Redis 连接是否可用，适合用作就绪/存活探针。
func (a *App) Ping(ctx context.Context) error {
	if a == nil || a.closed.Load() {
		return ErrClosed
	}

	if ctx == nil {
		ctx = context.Background()
	}

	return a.rdb.Ping(ctx).Err()
}

// Unregister 通过 App 内部的 Scheduler 按 entryID 移除已注册的周期任务，
// 与 Register 对称（首次调用时懒创建 Scheduler）。
func (a *App) Unregister(ctx context.Context, entryID string) error {
	scheduler, err := a.Scheduler()
	if err != nil {
		return err
	}

	return scheduler.Unregister(ctx, entryID)
}

// Start 启动消费侧：已通过 Handle 注册处理器的 Worker 与已通过 Register 注册周期任务的
// Scheduler（非阻塞）。两者都未注册时返回错误，以便尽早发现误用。
//
// 任一组件启动失败时会回滚已启动的组件，不会留下"半启动"状态在后台继续消费；
// 返回错误后 App 的消费侧应视为不可用，调用方应 Close 后重建或退出进程。
func (a *App) Start(ctx context.Context) error {
	if a == nil {
		return invalidArgumentError("app", "must not be nil")
	}

	if a.closed.Load() {
		return ErrClosed
	}

	worker := a.worker.Load()
	scheduler := a.scheduler.Load()

	if worker == nil && scheduler == nil {
		return invalidArgumentError("app", "Start/Run 前需先通过 Handle 注册处理器或通过 Register 注册周期任务")
	}

	if worker != nil {
		err := worker.Start(ctx)
		if err != nil {
			return err
		}
	}

	if scheduler != nil {
		err := scheduler.Start(ctx)
		if err != nil {
			return a.rollbackWorker(worker, err)
		}
	}

	return nil
}

// Run 启动消费侧并阻塞至 ctx 取消，然后按 Config.ShutdownTimeout 优雅关闭全部组件与连接。
// 启动失败时会清理已启动的组件后返回错误。
func (a *App) Run(ctx context.Context) error {
	if a == nil {
		return invalidArgumentError("app", "must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	err := a.Start(ctx)
	if err != nil {
		shutdownCtx, cancel := a.shutdownContext()
		defer cancel()

		shutdownErr := a.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			return errors.Join(err, shutdownErr)
		}

		return err
	}

	<-ctx.Done()

	shutdownCtx, cancel := a.shutdownContext()
	defer cancel()

	return a.Shutdown(shutdownCtx)
}

// Shutdown 优雅关闭 App 持有的全部组件，并在 App 拥有连接时关闭连接。重复调用安全。
func (a *App) Shutdown(ctx context.Context) error {
	if a == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	a.closeOnce.Do(func() {
		a.closed.Store(true)

		// closed 置位后再与懒创建互斥地读取组件指针：
		// 拿到锁时要么组件已创建完成，要么懒创建路径会观察到 closed 而放弃创建。
		a.mu.Lock()
		producer, worker, scheduler := a.producer.Load(), a.worker.Load(), a.scheduler.Load()
		a.mu.Unlock()

		a.closeErr = a.shutdownComponents(ctx, worker, scheduler, producer)
	})

	return a.closeErr
}

// Close 以 Config.ShutdownTimeout 为预算优雅关闭 App，等价于 Shutdown。重复调用安全。
func (a *App) Close() error {
	if a == nil {
		return nil
	}

	ctx, cancel := a.shutdownContext()
	defer cancel()

	return a.Shutdown(ctx)
}

// rollbackWorker 在 Scheduler 启动失败后回滚已启动的 Worker，
// 避免调用方拿到错误后 Worker 仍在后台消费；回滚失败时聚合两个错误返回。
func (a *App) rollbackWorker(worker *Worker, startErr error) error {
	if worker == nil {
		return startErr
	}

	shutdownCtx, cancel := a.shutdownContext()
	defer cancel()

	rollbackErr := worker.Shutdown(shutdownCtx)
	if rollbackErr != nil {
		return errors.Join(startErr, rollbackErr)
	}

	return startErr
}

func (a *App) shutdownContext() (context.Context, context.CancelFunc) {
	if a.cfg.ShutdownTimeout <= 0 {
		return context.Background(), func() {}
	}

	return context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
}

// shutdownComponents 按"先消费侧、再投递侧、最后共享连接"的顺序关闭并聚合错误。
// Inspector 仅包装共享连接、无独立资源，由统一关闭 rdb 释放，因此无需单独 Close。
func (a *App) shutdownComponents(ctx context.Context, worker *Worker, scheduler *Scheduler, producer *Producer) error {
	var errs []error

	if worker != nil {
		errs = appendNonNil(errs, worker.Shutdown(ctx))
	}

	if scheduler != nil {
		errs = appendNonNil(errs, scheduler.Shutdown(ctx))
	}

	if producer != nil {
		errs = appendNonNil(errs, producer.Shutdown(ctx))
	}

	if a.ownsRDB {
		errs = appendNonNil(errs, a.rdb.Close())
	}

	return errors.Join(errs...)
}

func appendNonNil(errs []error, err error) []error {
	if err != nil {
		return append(errs, err)
	}

	return errs
}
