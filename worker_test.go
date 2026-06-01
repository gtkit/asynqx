package asynqx

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gtkit/json"
	"github.com/hibiken/asynq"
)

var errWorkerStartFailed = errors.New("worker start failed")

type stubWorkerRunner struct {
	startCalls    atomic.Int32
	shutdownCalls atomic.Int32
	startErr      error
	started       chan struct{}
	blockStart    chan struct{}
	blockShutdown chan struct{}
	shutdownStart chan struct{}
}

func (r *stubWorkerRunner) Start(asynq.Handler) error {
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

func (r *stubWorkerRunner) Shutdown() {
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

type workerTestPayload struct {
	Name string `json:"name"`
}

func newTestWorker(t *testing.T, runner *stubWorkerRunner) *Worker {
	t.Helper()

	worker, err := newWorker(defaultConfig(), func(Config) (workerRunner, error) {
		return runner, nil
	})
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}

	return worker
}

func waitForWorkerState(t *testing.T, worker *Worker, state int32) {
	t.Helper()

	deadline := time.After(time.Second)

	for worker.state.Load() != state {
		select {
		case <-deadline:
			t.Fatalf("expected worker state %d, got %d", state, worker.state.Load())
		default:
			runtime.Gosched()
		}
	}
}

func TestHandleBeforeStartProcessesDecodedPayload(t *testing.T) {
	worker := newTestWorker(t, &stubWorkerRunner{})

	var called atomic.Bool

	if err := Handle(worker, "email:welcome", func(ctx context.Context, payload workerTestPayload) error {
		called.Store(true)

		if ctx == nil {
			t.Fatal("expected context to be passed to handler")
		}

		if payload.Name != "alice" {
			t.Fatalf("expected decoded payload name %q, got %q", "alice", payload.Name)
		}

		return nil
	}); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	body, err := json.Marshal(workerTestPayload{Name: "alice"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	task := asynq.NewTask("email:welcome", body)

	err = worker.mux.ProcessTask(context.Background(), task)
	if err != nil {
		t.Fatalf("expected task to be processed, got %v", err)
	}

	if !called.Load() {
		t.Fatal("expected handler to be called")
	}
}

func TestNewWorkerUsesConfiguredFactory(t *testing.T) {
	runner := &stubWorkerRunner{}

	restore := setWorkerRunnerFactoryForTest(func(Config) (workerRunner, error) {
		return runner, nil
	})
	defer restore()

	worker, err := NewWorker()
	if err != nil {
		t.Fatalf("unexpected worker error: %v", err)
	}

	err = worker.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected configured factory runner to be shut down once, got %d", got)
	}
}

func TestNewWorkerFromConfigUsesConfig(t *testing.T) {
	cfg, err := NewConfig(WithConcurrency(32))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	runner := &stubWorkerRunner{}

	restore := setWorkerRunnerFactoryForTest(func(got Config) (workerRunner, error) {
		if got.Concurrency != 32 {
			t.Fatalf("expected concurrency 32, got %d", got.Concurrency)
		}

		return runner, nil
	})
	defer restore()

	worker, err := NewWorkerFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected worker error: %v", err)
	}

	if err = worker.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}
}

func TestHandleReturnsSkipRetryOnInvalidPayload(t *testing.T) {
	worker := newTestWorker(t, &stubWorkerRunner{})

	if err := Handle(worker, "email:welcome", func(context.Context, workerTestPayload) error {
		t.Fatal("handler should not be called for invalid payload")

		return nil
	}); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	task := asynq.NewTask("email:welcome", []byte("{"))

	err := worker.mux.ProcessTask(context.Background(), task)
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("expected SkipRetry, got %v", err)
	}
}

func TestHandleRawRejectsRegistrationAfterStart(t *testing.T) {
	runner := &stubWorkerRunner{}
	worker := newTestWorker(t, runner)

	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	err := worker.HandleRaw("email:welcome", func(context.Context, *asynq.Task) error {
		return nil
	})
	if !errors.Is(err, ErrWorkerAlreadyRunning) {
		t.Fatalf("expected ErrWorkerAlreadyRunning, got %v", err)
	}
}

func TestHandleRawRejectsDuplicateTaskType(t *testing.T) {
	worker := newTestWorker(t, &stubWorkerRunner{})

	if err := worker.HandleRaw("email:welcome", func(context.Context, *asynq.Task) error {
		return nil
	}); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	err := worker.HandleRaw("email:welcome", func(context.Context, *asynq.Task) error {
		return nil
	})
	if !errors.Is(err, ErrHandlerAlreadyRegistered) {
		t.Fatalf("expected ErrHandlerAlreadyRegistered, got %v", err)
	}
}

func TestShutdownIsIdempotent(t *testing.T) {
	runner := &stubWorkerRunner{}
	worker := newTestWorker(t, runner)

	err := worker.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	err = worker.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	err = worker.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected second shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once, got %d", got)
	}
}

func TestShutdownBeforeStartClosesWorkerRunner(t *testing.T) {
	runner := &stubWorkerRunner{}
	worker := newTestWorker(t, runner)

	err := worker.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected idle shutdown to close runner once, got %d", got)
	}
}

func TestRunCancelsAndTriggersShutdown(t *testing.T) {
	runner := &stubWorkerRunner{started: make(chan struct{})}
	worker := newTestWorker(t, runner)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)

	go func() {
		runErr <- worker.Run(ctx)
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

func TestShutdownWhileStartInProgressStopsWorker(t *testing.T) {
	runner := &stubWorkerRunner{
		started:    make(chan struct{}),
		blockStart: make(chan struct{}),
	}
	worker := newTestWorker(t, runner)

	startErr := make(chan error, 1)

	go func() {
		startErr <- worker.Start(context.Background())
	}()

	<-runner.started

	shutdownErr := make(chan error, 1)

	go func() {
		shutdownErr <- worker.Shutdown(context.Background())
	}()

	close(runner.blockStart)

	err := <-shutdownErr
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	err = <-startErr
	if err != nil && !errors.Is(err, ErrWorkerStopped) {
		t.Fatalf("expected nil or ErrWorkerStopped, got %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once after start completed, got %d", got)
	}
}

func TestWorkerShutdownWhileStartWaitingForRegistrationsClosesRunner(t *testing.T) {
	runner := &stubWorkerRunner{}
	worker := newTestWorker(t, runner)

	if err := worker.beginRegistration(); err != nil {
		t.Fatalf("unexpected registration error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)

	go func() {
		startErr <- worker.Start(ctx)
	}()

	waitForWorkerState(t, worker, workerStateStarting)

	shutdownErr := make(chan error, 1)

	go func() {
		shutdownErr <- worker.Shutdown(context.Background())
	}()

	waitForWorkerState(t, worker, workerStateStopping)
	cancel()
	worker.endRegistration()

	err := <-startErr
	if !errors.Is(err, ErrWorkerStopped) {
		t.Fatalf("expected ErrWorkerStopped, got %v", err)
	}

	err = <-shutdownErr
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once after start cancellation, got %d", got)
	}
}

func TestWorkerShutdownWhileStartReturnsErrorClosesRunner(t *testing.T) {
	runner := &stubWorkerRunner{
		startErr:   errWorkerStartFailed,
		started:    make(chan struct{}),
		blockStart: make(chan struct{}),
	}
	worker := newTestWorker(t, runner)

	startErr := make(chan error, 1)

	go func() {
		startErr <- worker.Start(context.Background())
	}()

	<-runner.started

	shutdownErr := make(chan error, 1)

	go func() {
		shutdownErr <- worker.Shutdown(context.Background())
	}()

	waitForWorkerState(t, worker, workerStateStopping)
	close(runner.blockStart)

	err := <-startErr
	if !errors.Is(err, ErrWorkerStopped) {
		t.Fatalf("expected ErrWorkerStopped, got %v", err)
	}

	err = <-shutdownErr
	if err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once after start error, got %d", got)
	}
}

// TestWorkerStartErrorDoesNotOverrideConcurrentShutdown 压测 Start 失败路径与并发
// Shutdown 的逻辑竞态。若 Start 在 runner.Start 失败后无条件把状态写回 idle，会覆盖
// 并发 Shutdown 刚 CAS 写入的 stopping，使 Shutdown 永久阻塞在 waitStopped。
//
// 该竞态发生在失败路径相邻的两条原子操作（读状态、写状态）之间，无法用通道确定性切分，
// 因此以高频并发压测兜底：对正确实现而言 Shutdown 必定及时返回、循环很快跑完；只有当
// 回归出现状态覆盖时，并发 Shutdown 才会挂起并触发超时。该测试不会对正确实现误报。
func TestWorkerStartErrorDoesNotOverrideConcurrentShutdown(t *testing.T) {
	for iteration := range 5000 {
		runner := &stubWorkerRunner{startErr: errWorkerStartFailed}
		worker := newTestWorker(t, runner)

		ready := make(chan struct{})
		startErr := make(chan error, 1)
		shutdownErr := make(chan error, 1)

		go func() {
			<-ready

			startErr <- worker.Start(context.Background())
		}()

		go func() {
			<-ready

			shutdownErr <- worker.Shutdown(context.Background())
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

func TestHandleRawRejectsRegistrationAfterShutdown(t *testing.T) {
	worker := newTestWorker(t, &stubWorkerRunner{})

	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	err := worker.HandleRaw("email:welcome", func(context.Context, *asynq.Task) error {
		return nil
	})
	if !errors.Is(err, ErrWorkerStopped) {
		t.Fatalf("expected ErrWorkerStopped, got %v", err)
	}
}

func TestShutdownRespectsContextWhileRunning(t *testing.T) {
	runner := &stubWorkerRunner{
		blockShutdown: make(chan struct{}),
		shutdownStart: make(chan struct{}),
	}
	worker := newTestWorker(t, runner)

	err := worker.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err = worker.Shutdown(shutdownCtx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}

	<-runner.shutdownStart
	close(runner.blockShutdown)

	err = worker.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected shutdown wait error: %v", err)
	}
}

func TestRunUsesConfiguredShutdownTimeout(t *testing.T) {
	cfg, err := NewConfig(WithShutdownTimeout(20 * time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	runner := &stubWorkerRunner{
		started:       make(chan struct{}),
		blockShutdown: make(chan struct{}),
		shutdownStart: make(chan struct{}),
	}

	worker, err := newWorker(cfg, func(Config) (workerRunner, error) {
		return runner, nil
	})
	if err != nil {
		t.Fatalf("unexpected worker error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)

	go func() {
		runErr <- worker.Run(ctx)
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
