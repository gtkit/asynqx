package asynqx

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/gtkit/json/v2"
	"github.com/hibiken/asynq"
)

func TestTaskTypeName(t *testing.T) {
	def := NewTaskType[workerTestPayload]("email:welcome")
	if def.Name() != "email:welcome" {
		t.Fatalf("expected name %q, got %q", "email:welcome", def.Name())
	}
}

func TestTaskTypeEnqueueUsesBoundType(t *testing.T) {
	client := &stubProducerClient{}

	producer, err := newProducer(defaultConfig(), func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	def := NewTaskType[workerTestPayload]("email:welcome")

	_, err = def.Enqueue(context.Background(), producer, workerTestPayload{Name: "alice"})
	if err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	if client.enqueueCalls != 1 {
		t.Fatalf("expected enqueue to be called once, got %d", client.enqueueCalls)
	}

	if client.lastTask.Type() != "email:welcome" {
		t.Fatalf("expected task type %q, got %q", "email:welcome", client.lastTask.Type())
	}

	var decoded workerTestPayload

	if err = json.Unmarshal(client.lastTask.Payload(), &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if decoded.Name != "alice" {
		t.Fatalf("expected payload name %q, got %q", "alice", decoded.Name)
	}
}

func TestTaskTypeHandleProcessesDecodedPayload(t *testing.T) {
	worker := newTestWorker(t, &stubWorkerRunner{})

	def := NewTaskType[workerTestPayload]("email:welcome")

	var called atomic.Bool

	err := def.Handle(worker, func(_ context.Context, payload workerTestPayload) error {
		called.Store(true)

		if payload.Name != "alice" {
			t.Fatalf("expected decoded payload name %q, got %q", "alice", payload.Name)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("unexpected handle error: %v", err)
	}

	body, err := json.Marshal(workerTestPayload{Name: "alice"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = worker.mux.ProcessTask(context.Background(), asynq.NewTask("email:welcome", body))
	if err != nil {
		t.Fatalf("expected task to be processed, got %v", err)
	}

	if !called.Load() {
		t.Fatal("expected handler to be called")
	}
}

func TestTaskTypeRegisterUsesBoundType(t *testing.T) {
	runner := &stubSchedulerRunner{}
	scheduler := newTestScheduler(t, runner)

	def := NewTaskType[workerTestPayload]("email:welcome")

	entryID, err := def.Register(context.Background(), scheduler, "@every 1m", workerTestPayload{Name: "alice"})
	if err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	if entryID != "entry-1" {
		t.Fatalf("expected entry id %q, got %q", "entry-1", entryID)
	}

	if runner.registeredSpec != "@every 1m" {
		t.Fatalf("expected spec %q, got %q", "@every 1m", runner.registeredSpec)
	}

	if runner.registeredTask.Type() != "email:welcome" {
		t.Fatalf("expected task type %q, got %q", "email:welcome", runner.registeredTask.Type())
	}
}

func TestTaskTypeDefaultOptionsApplied(t *testing.T) {
	client := &stubProducerClient{}

	producer, err := newProducer(defaultConfig(), func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	def := NewTask[workerTestPayload]("email:welcome", WithTaskQueue("low"), WithTaskMaxRetry(3))

	if _, err = def.Enqueue(context.Background(), producer, workerTestPayload{Name: "alice"}); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	// 默认选项 queue/max_retry + defaultConfig 注入的默认超时。
	if len(client.lastOpts) != 3 {
		t.Fatalf("expected 3 task options, got %d", len(client.lastOpts))
	}

	assertAsynqOption(t, client.lastOpts[0], asynq.QueueOpt, "low")
	assertAsynqOption(t, client.lastOpts[1], asynq.MaxRetryOpt, 3)
	assertAsynqOption(t, client.lastOpts[2], asynq.TimeoutOpt, defaultTaskTimeout)
}

func TestTaskTypeCallSiteOptionsOverrideDefaults(t *testing.T) {
	client := &stubProducerClient{}

	producer, err := newProducer(defaultConfig(), func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	def := NewTask[workerTestPayload]("email:welcome", WithTaskQueue("low"))

	if _, err = def.Enqueue(
		context.Background(),
		producer,
		workerTestPayload{Name: "alice"},
		WithTaskQueue("high"),
	); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	// 后应用的调用时选项覆盖默认选项，最终只产生一个 queue 选项。
	if len(client.lastOpts) != 2 {
		t.Fatalf("expected 2 task options, got %d", len(client.lastOpts))
	}

	assertAsynqOption(t, client.lastOpts[0], asynq.QueueOpt, "high")
	assertAsynqOption(t, client.lastOpts[1], asynq.TimeoutOpt, defaultTaskTimeout)
}

func TestTaskTypeRegisterAppliesDefaultOptions(t *testing.T) {
	runner := &stubSchedulerRunner{}
	scheduler := newTestScheduler(t, runner)

	def := NewTask[workerTestPayload]("email:welcome", WithTaskQueue("low"))

	if _, err := def.Register(
		context.Background(), scheduler, "@every 1m", workerTestPayload{Name: "alice"},
	); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	if len(runner.registeredOpts) != 2 {
		t.Fatalf("expected 2 task options, got %d", len(runner.registeredOpts))
	}

	if got := runner.registeredOpts[0].Value(); got != "low" {
		t.Fatalf("expected default queue option value %q, got %v", "low", got)
	}
}

func TestTaskTypeMustHandleRegistersHandler(t *testing.T) {
	worker := newTestWorker(t, &stubWorkerRunner{})

	def := NewTask[workerTestPayload]("email:welcome")
	def.MustHandle(worker, func(context.Context, workerTestPayload) error { return nil })

	if _, loaded := worker.handlers.Load("email:welcome"); !loaded {
		t.Fatal("expected handler to be registered")
	}
}

func TestTaskTypeMustHandlePanicsOnError(t *testing.T) {
	worker := newTestWorker(t, &stubWorkerRunner{})

	def := NewTask[workerTestPayload]("email:welcome")

	defer func() {
		if recover() == nil {
			t.Fatal("expected MustHandle to panic on nil handler")
		}
	}()

	def.MustHandle(worker, nil)
}

func TestTaskTypeEnqueueRejectsNilEnqueuer(t *testing.T) {
	def := NewTask[workerTestPayload]("email:welcome")

	_, err := def.Enqueue(context.Background(), nil, workerTestPayload{Name: "alice"})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for nil enqueuer, got %v", err)
	}

	// typed-nil 接口同样必须被拦下，不允许 panic。
	_, err = def.Enqueue(context.Background(), (*Producer)(nil), workerTestPayload{Name: "alice"})
	if err == nil {
		t.Fatal("expected error for typed-nil enqueuer")
	}
}

func TestTaskTypeRegisterRejectsNilRegistrar(t *testing.T) {
	def := NewTask[workerTestPayload]("email:welcome")

	_, err := def.Register(context.Background(), nil, "@every 1m", workerTestPayload{Name: "alice"})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for nil registrar, got %v", err)
	}

	_, err = def.Register(context.Background(), (*Scheduler)(nil), "@every 1m", workerTestPayload{Name: "alice"})
	if err == nil {
		t.Fatal("expected error for typed-nil registrar")
	}
}
