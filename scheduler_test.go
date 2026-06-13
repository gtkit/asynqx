package asynqx

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gtkit/json/v2"
	"github.com/hibiken/asynq"
)

type stubSchedulerRunner struct {
	registerCalls   atomic.Int32
	unregisterCalls atomic.Int32
	startCalls      atomic.Int32
	shutdownCalls   atomic.Int32

	registerErr   error
	unregisterErr error
	startErr      error

	registeredSpec string
	registeredTask *asynq.Task
	registeredOpts []asynq.Option
	unregisteredID string

	started       chan struct{}
	blockStart    chan struct{}
	blockShutdown chan struct{}
	shutdownStart chan struct{}
}

func (r *stubSchedulerRunner) Register(spec string, task *asynq.Task, opts ...asynq.Option) (string, error) {
	r.registerCalls.Add(1)
	r.registeredSpec = spec
	r.registeredTask = task

	r.registeredOpts = append([]asynq.Option(nil), opts...)

	if r.registerErr != nil {
		return "", r.registerErr
	}

	return "entry-1", nil
}

func (r *stubSchedulerRunner) Unregister(entryID string) error {
	r.unregisterCalls.Add(1)
	r.unregisteredID = entryID

	return r.unregisterErr
}

func (r *stubSchedulerRunner) Start() error {
	r.startCalls.Add(1)

	if r.started != nil {
		select {
		case <-r.started:
		default:
			close(r.started)
		}
	}

	if r.blockStart != nil {
		<-r.blockStart
	}

	return r.startErr
}

func (r *stubSchedulerRunner) Shutdown() {
	r.shutdownCalls.Add(1)

	if r.shutdownStart != nil {
		select {
		case <-r.shutdownStart:
		default:
			close(r.shutdownStart)
		}
	}

	if r.blockShutdown != nil {
		<-r.blockShutdown
	}
}

type schedulerTestPayload struct {
	Name string `json:"name"`
}

func newTestScheduler(t *testing.T, runner *stubSchedulerRunner) *Scheduler {
	t.Helper()

	scheduler, err := newScheduler(defaultConfig(), func(Config) (schedulerRunner, error) {
		return runner, nil
	})
	if err != nil {
		t.Fatalf("unexpected error creating scheduler: %v", err)
	}

	return scheduler
}

func TestSchedulerRegisterRejectsEmptySpec(t *testing.T) {
	scheduler := newTestScheduler(t, &stubSchedulerRunner{})

	_, err := scheduler.Register(context.Background(), "   ", "email:welcome", schedulerTestPayload{})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestNewSchedulerUsesConfiguredFactory(t *testing.T) {
	runner := &stubSchedulerRunner{}

	restore := setSchedulerRunnerFactoryForTest(func(Config) (schedulerRunner, error) {
		return runner, nil
	})
	defer restore()

	scheduler, err := NewScheduler()
	if err != nil {
		t.Fatalf("unexpected scheduler error: %v", err)
	}

	err = scheduler.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected configured factory runner to be shut down once, got %d", got)
	}
}

func TestNewSchedulerFromConfigUsesConfig(t *testing.T) {
	cfg, err := NewConfig(WithLocation("Asia/Shanghai"))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	runner := &stubSchedulerRunner{}

	restore := setSchedulerRunnerFactoryForTest(func(got Config) (schedulerRunner, error) {
		if got.Location.String() != "Asia/Shanghai" {
			t.Fatalf("expected location Asia/Shanghai, got %v", got.Location)
		}

		return runner, nil
	})
	defer restore()

	scheduler, err := NewSchedulerFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected scheduler error: %v", err)
	}

	if err = scheduler.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}
}

func TestSchedulerRegisterEncodesPayloadAndPassesOptions(t *testing.T) {
	runner := &stubSchedulerRunner{}
	scheduler := newTestScheduler(t, runner)

	payload := schedulerTestPayload{Name: "alice"}

	entryID, err := scheduler.Register(
		context.Background(),
		"@every 1m",
		"email:welcome",
		payload,
		WithTaskQueue("critical"),
		WithTaskTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	if entryID != "entry-1" {
		t.Fatalf("expected entry id %q, got %q", "entry-1", entryID)
	}

	if runner.registeredSpec != "@every 1m" {
		t.Fatalf("expected spec to be passed through, got %q", runner.registeredSpec)
	}

	if runner.registeredTask == nil {
		t.Fatal("expected task to be passed to runner")
	}

	if runner.registeredTask.Type() != "email:welcome" {
		t.Fatalf("expected task type %q, got %q", "email:welcome", runner.registeredTask.Type())
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if string(runner.registeredTask.Payload()) != string(body) {
		t.Fatalf("expected payload %q, got %q", string(body), string(runner.registeredTask.Payload()))
	}

	if len(runner.registeredOpts) != 2 {
		t.Fatalf("expected 2 task options, got %d", len(runner.registeredOpts))
	}

	if got := runner.registeredOpts[0].Value(); got != "critical" {
		t.Fatalf("expected queue option value %q, got %v", "critical", got)
	}

	if got := runner.registeredOpts[1].Value(); got != 30*time.Second {
		t.Fatalf("expected timeout option value %v, got %v", 30*time.Second, got)
	}
}

func TestSchedulerRegisterAppliesDefaultTaskTimeout(t *testing.T) {
	cfg, err := NewConfig(WithDefaultTaskTimeout(20 * time.Second))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	runner := &stubSchedulerRunner{}

	scheduler, err := newScheduler(cfg, func(Config) (schedulerRunner, error) {
		return runner, nil
	})
	if err != nil {
		t.Fatalf("unexpected scheduler error: %v", err)
	}

	_, err = scheduler.Register(
		context.Background(),
		"@every 1m",
		"email:welcome",
		schedulerTestPayload{Name: "bob"},
	)
	if err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	if len(runner.registeredOpts) != 1 {
		t.Fatalf("expected 1 default task option, got %d", len(runner.registeredOpts))
	}

	if got := runner.registeredOpts[0].Value(); got != 20*time.Second {
		t.Fatalf("expected timeout option value %v, got %v", 20*time.Second, got)
	}
}

func TestSchedulerRegisterRejectsAfterShutdown(t *testing.T) {
	scheduler := newTestScheduler(t, &stubSchedulerRunner{})

	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	_, err := scheduler.Register(context.Background(), "@every 1m", "email:welcome", schedulerTestPayload{})
	if !errors.Is(err, ErrSchedulerStopped) {
		t.Fatalf("expected ErrSchedulerStopped, got %v", err)
	}
}

func TestSchedulerUnregisterRejectsEmptyEntryID(t *testing.T) {
	scheduler := newTestScheduler(t, &stubSchedulerRunner{})

	err := scheduler.Unregister(context.Background(), " ")
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestSchedulerUnregisterRejectsAfterShutdown(t *testing.T) {
	scheduler := newTestScheduler(t, &stubSchedulerRunner{})

	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	err := scheduler.Unregister(context.Background(), "entry-1")
	if !errors.Is(err, ErrSchedulerStopped) {
		t.Fatalf("expected ErrSchedulerStopped, got %v", err)
	}
}

func TestSchedulerShutdownIsIdempotent(t *testing.T) {
	runner := &stubSchedulerRunner{}
	scheduler := newTestScheduler(t, runner)

	err := scheduler.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	err = scheduler.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	err = scheduler.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected second shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once, got %d", got)
	}
}

func TestSchedulerShutdownBeforeStartClosesRunner(t *testing.T) {
	runner := &stubSchedulerRunner{}
	scheduler := newTestScheduler(t, runner)

	err := scheduler.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected idle shutdown to close runner once, got %d", got)
	}
}

func TestSchedulerRunCancelsAndTriggersShutdown(t *testing.T) {
	runner := &stubSchedulerRunner{started: make(chan struct{})}
	scheduler := newTestScheduler(t, runner)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)

	go func() {
		runErr <- scheduler.Run(ctx)
	}()

	<-runner.started
	cancel()

	err := <-runErr
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once, got %d", got)
	}
}

func TestSchedulerShutdownWhileStartInProgressStopsScheduler(t *testing.T) {
	runner := &stubSchedulerRunner{
		started:    make(chan struct{}),
		blockStart: make(chan struct{}),
	}
	scheduler := newTestScheduler(t, runner)

	startErr := make(chan error, 1)

	go func() {
		startErr <- scheduler.Start(context.Background())
	}()

	<-runner.started

	shutdownErr := make(chan error, 1)

	go func() {
		shutdownErr <- scheduler.Shutdown(context.Background())
	}()

	close(runner.blockStart)

	err := <-shutdownErr
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	err = <-startErr
	if err != nil && !errors.Is(err, ErrSchedulerStopped) {
		t.Fatalf("expected nil or ErrSchedulerStopped, got %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once after start completed, got %d", got)
	}
}

func TestSchedulerShutdownRespectsContextWhileStartInProgress(t *testing.T) {
	runner := &stubSchedulerRunner{
		started:    make(chan struct{}),
		blockStart: make(chan struct{}),
	}
	scheduler := newTestScheduler(t, runner)

	startErr := make(chan error, 1)

	go func() {
		startErr <- scheduler.Start(context.Background())
	}()

	<-runner.started

	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := scheduler.Shutdown(shutdownCtx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}

	close(runner.blockStart)

	err = <-startErr
	if !errors.Is(err, ErrSchedulerStopped) {
		t.Fatalf("expected ErrSchedulerStopped, got %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once after start completed, got %d", got)
	}
}

func TestSchedulerRunUsesConfiguredShutdownTimeout(t *testing.T) {
	cfg, err := NewConfig(WithShutdownTimeout(20 * time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	runner := &stubSchedulerRunner{
		started:       make(chan struct{}),
		blockShutdown: make(chan struct{}),
		shutdownStart: make(chan struct{}),
	}

	scheduler, err := newScheduler(cfg, func(Config) (schedulerRunner, error) {
		return runner, nil
	})
	if err != nil {
		t.Fatalf("unexpected scheduler error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)

	go func() {
		runErr <- scheduler.Run(ctx)
	}()

	<-runner.started
	cancel()
	<-runner.shutdownStart

	err = <-runErr
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded from shutdown timeout, got %v", err)
	}

	close(runner.blockShutdown)
}

// TestSchedulerStartErrorDoesNotOverrideConcurrentShutdown 压测 Start 失败路径与并发
// Shutdown 的逻辑竞态。若 Start 在 runner.Start 失败后无条件把状态写回 idle，会覆盖
// 并发 Shutdown 刚 CAS 写入的 stopping，使 Shutdown 永久阻塞在 waitStopped。
//
// 该竞态发生在失败路径相邻的两条原子操作（读状态、写状态）之间，无法用通道确定性切分，
// 因此以高频并发压测兜底：对正确实现而言 Shutdown 必定及时返回、循环很快跑完；只有当
// 回归出现状态覆盖时，并发 Shutdown 才会挂起并触发超时。该测试不会对正确实现误报。
var errSchedulerStartFailed = errors.New("scheduler start failed")

func TestSchedulerStartErrorDoesNotOverrideConcurrentShutdown(t *testing.T) {
	for iteration := range 5000 {
		runner := &stubSchedulerRunner{startErr: errSchedulerStartFailed}
		scheduler := newTestScheduler(t, runner)

		ready := make(chan struct{})
		startErr := make(chan error, 1)
		shutdownErr := make(chan error, 1)

		go func() {
			<-ready

			startErr <- scheduler.Start(context.Background())
		}()

		go func() {
			<-ready

			shutdownErr <- scheduler.Shutdown(context.Background())
		}()

		close(ready)

		select {
		case <-shutdownErr:
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d: shutdown hung — start failure path overrode concurrent stopping", iteration)
		}

		<-startErr
	}
}

// TestSchedulerRunnerFactoryConstructs 覆盖 defaultSchedulerRunnerFactory 的两个分支：
// 外部共享客户端走 NewSchedulerFromRedisClient，单机连接参数走 asynq.NewScheduler
// （由 asynq 自建并在 Shutdown 时干净关闭其内部客户端，避免共享连接关闭错误日志）。
// 两条路径在构造期均为惰性，不连接 Redis；未启动的 Shutdown 在 asynq 中直接返回。
func TestSchedulerRunnerFactoryConstructs(t *testing.T) {
	extCfg, err := NewConfig(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("unexpected config error (external): %v", err)
	}

	extRunner, err := defaultSchedulerRunnerFactory(extCfg)
	if err != nil {
		t.Fatalf("unexpected factory error (external): %v", err)
	}

	if extRunner == nil {
		t.Fatal("expected non-nil runner for external client")
	}

	extRunner.Shutdown()

	ownedCfg, err := NewConfig(WithRedisAddr(defaultRedisAddress))
	if err != nil {
		t.Fatalf("unexpected config error (owned): %v", err)
	}

	ownedRunner, err := defaultSchedulerRunnerFactory(ownedCfg)
	if err != nil {
		t.Fatalf("unexpected factory error (owned): %v", err)
	}

	if ownedRunner == nil {
		t.Fatal("expected non-nil runner for owned client")
	}

	ownedRunner.Shutdown()
}
