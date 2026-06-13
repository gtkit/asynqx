package asynqx

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
)

// recordingTarget 同时实现 Enqueuer / Registrar / PeriodicRegistrar（与 App 一样），
// 用于验证 TaskType 通过接口正确转发任务类型名与 payload。
type recordingTarget struct {
	enqueuedType    string
	enqueuedPayload any
	handledType     string
	registeredSpec  string
	registeredType  string
}

func (r *recordingTarget) Enqueue(
	_ context.Context, taskType string, payload any, _ ...TaskOption,
) (*asynq.TaskInfo, error) {
	r.enqueuedType = taskType
	r.enqueuedPayload = payload

	return &asynq.TaskInfo{ID: "rec"}, nil
}

func (r *recordingTarget) HandleRaw(taskType string, _ func(context.Context, *asynq.Task) error) error {
	r.handledType = taskType

	return nil
}

func (r *recordingTarget) Register(
	_ context.Context, spec, taskType string, _ any, _ ...TaskOption,
) (string, error) {
	r.registeredSpec = spec
	r.registeredType = taskType

	return "entry-1", nil
}

func TestTaskTypeForwardsThroughInterfaces(t *testing.T) {
	type emailPayload struct{ UserID string }

	def := NewTask[emailPayload]("email:welcome")
	target := &recordingTarget{}

	if _, err := def.Enqueue(context.Background(), target, emailPayload{UserID: "u-1"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if target.enqueuedType != "email:welcome" {
		t.Fatalf("expected enqueued type email:welcome, got %q", target.enqueuedType)
	}

	if err := def.Handle(target, func(context.Context, emailPayload) error { return nil }); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if target.handledType != "email:welcome" {
		t.Fatalf("expected handled type email:welcome, got %q", target.handledType)
	}

	if _, err := def.Register(context.Background(), target, "@every 1h", emailPayload{UserID: "u-2"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if target.registeredSpec != "@every 1h" || target.registeredType != "email:welcome" {
		t.Fatalf("unexpected register args: spec=%q type=%q", target.registeredSpec, target.registeredType)
	}
}

func TestAppLazyComponentsAreSingletons(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	defer app.Close()

	producer1, err := app.Producer()
	if err != nil {
		t.Fatalf("producer: %v", err)
	}

	producer2, err := app.Producer()
	if err != nil {
		t.Fatalf("producer (repeat): %v", err)
	}

	if producer1 != producer2 {
		t.Fatal("expected Producer to be a lazily-created singleton")
	}

	worker1, err := app.Worker()
	if err != nil {
		t.Fatalf("worker: %v", err)
	}

	worker2, err := app.Worker()
	if err != nil {
		t.Fatalf("worker (repeat): %v", err)
	}

	if worker1 != worker2 {
		t.Fatal("expected Worker to be a lazily-created singleton")
	}

	scheduler1, err := app.Scheduler()
	if err != nil {
		t.Fatalf("scheduler: %v", err)
	}

	scheduler2, err := app.Scheduler()
	if err != nil {
		t.Fatalf("scheduler (repeat): %v", err)
	}

	if scheduler1 != scheduler2 {
		t.Fatal("expected Scheduler to be a lazily-created singleton")
	}

	inspector1, err := app.Inspector()
	if err != nil {
		t.Fatalf("inspector: %v", err)
	}

	inspector2, err := app.Inspector()
	if err != nil {
		t.Fatalf("inspector (repeat): %v", err)
	}

	if inspector1 != inspector2 {
		t.Fatal("expected Inspector to be a lazily-created singleton")
	}
}

func TestAppCloseIsIdempotentAndRejectsAfterClose(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if _, err = app.Producer(); err != nil {
		t.Fatalf("producer: %v", err)
	}

	if err = app.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	if err = app.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	if _, err = app.Producer(); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed after close, got %v", err)
	}

	if _, err = app.Enqueue(context.Background(), "email:welcome", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed from Enqueue after close, got %v", err)
	}
}

func TestAppStartWithoutRegistrationFails(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	defer app.Close()

	err = app.Start(context.Background())
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument starting app without registrations, got %v", err)
	}
}

func TestAppOwnsConnectionWhenBuiltFromAddr(t *testing.T) {
	app, err := New(WithRedisAddr(defaultRedisAddress))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if !app.ownsRDB {
		t.Fatal("expected app to own a self-built connection")
	}

	if err = app.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestAppDoesNotOwnExternalConnection(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if app.ownsRDB {
		t.Fatal("expected app not to own an external client")
	}

	if err = app.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
