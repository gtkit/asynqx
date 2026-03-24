package asynqx

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gtkit/json"
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

	started    chan struct{}
	blockStart chan struct{}
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
	cfg, err := NewConfig(WithTaskTimeoutOption(20 * time.Second))
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

	if _, err := scheduler.Register(context.Background(), "@every 1m", "email:welcome", schedulerTestPayload{Name: "bob"}); err != nil {
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

	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected second shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once, got %d", got)
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

	if err := <-runErr; err != nil {
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

	if err := <-shutdownErr; err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	if err := <-startErr; err != nil && !errors.Is(err, ErrSchedulerStopped) {
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

	if err := scheduler.Shutdown(shutdownCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}

	close(runner.blockStart)

	if err := <-startErr; !errors.Is(err, ErrSchedulerStopped) {
		t.Fatalf("expected ErrSchedulerStopped, got %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once after start completed, got %d", got)
	}
}
