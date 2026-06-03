package asynqx

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/gtkit/json"
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
