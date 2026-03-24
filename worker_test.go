package asynqx

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/gtkit/json"
	"github.com/hibiken/asynq"
)

type stubWorkerRunner struct {
	startCalls    atomic.Int32
	shutdownCalls atomic.Int32
	startErr      error
	started       chan struct{}
	blockStart    chan struct{}
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
	if err := worker.mux.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("expected task to be processed, got %v", err)
	}

	if !called.Load() {
		t.Fatal("expected handler to be called")
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

	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected second shutdown error: %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once, got %d", got)
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

	if err := <-runErr; err != nil {
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

	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	close(runner.blockStart)

	if err := <-startErr; !errors.Is(err, ErrWorkerStopped) {
		t.Fatalf("expected ErrWorkerStopped, got %v", err)
	}

	if got := runner.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected shutdown to be called once after start completed, got %d", got)
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
