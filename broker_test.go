package asynqx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

type stubBrokerClient struct {
	enqueueCalls int
	closeCalls   int
	lastCtx      context.Context
	lastTask     *asynq.Task
	lastOpts     []asynq.Option
	enqueueInfo  *asynq.TaskInfo
	enqueueErr   error
	closeErr     error
	started      chan struct{}
	blockEnqueue chan struct{}
}

func (s *stubBrokerClient) EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	s.enqueueCalls++
	s.lastCtx = ctx
	s.lastTask = task
	s.lastOpts = append([]asynq.Option(nil), opts...)
	if s.started != nil {
		select {
		case <-s.started:
		default:
			close(s.started)
		}
	}
	if s.blockEnqueue != nil {
		<-s.blockEnqueue
	}
	if s.enqueueInfo != nil || s.enqueueErr != nil {
		return s.enqueueInfo, s.enqueueErr
	}
	return &asynq.TaskInfo{ID: "stub-id", Queue: "default", Type: task.Type(), Payload: task.Payload()}, nil
}

func (s *stubBrokerClient) Close() error {
	s.closeCalls++
	return s.closeErr
}

func TestBrokerCloseIsIdempotent(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubBrokerClient{}
	broker, err := newBroker(cfg, func(Config) (brokerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected broker error: %v", err)
	}

	if err := broker.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
	if err := broker.Close(); err != nil {
		t.Fatalf("unexpected second close error: %v", err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("expected client close to be called once, got %d", client.closeCalls)
	}
}

func TestBrokerEnqueueAfterCloseReturnsErrClosed(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubBrokerClient{}
	broker, err := newBroker(cfg, func(Config) (brokerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected broker error: %v", err)
	}

	if err := broker.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	_, err = broker.Enqueue(context.Background(), "email:send", map[string]string{"id": "1"})
	if err == nil {
		t.Fatal("expected ErrClosed")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestBrokerEnqueueMarshalsPayloadAndPassesOptions(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubBrokerClient{}
	broker, err := newBroker(cfg, func(Config) (brokerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected broker error: %v", err)
	}

	processAt := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	info, err := broker.Enqueue(
		context.Background(),
		"email:send",
		map[string]string{"id": "42"},
		WithTaskQueue("critical"),
		WithTaskProcessAt(processAt),
		WithTaskMaxRetry(3),
	)
	if err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}
	if info == nil {
		t.Fatal("expected task info")
	}
	if client.enqueueCalls != 1 {
		t.Fatalf("expected one enqueue call, got %d", client.enqueueCalls)
	}
	if client.lastTask == nil {
		t.Fatal("expected task to be passed to client")
	}
	if client.lastTask.Type() != "email:send" {
		t.Fatalf("expected task type email:send, got %q", client.lastTask.Type())
	}
	if string(client.lastTask.Payload()) != `{"id":"42"}` {
		t.Fatalf("expected marshaled payload, got %s", string(client.lastTask.Payload()))
	}
	if len(client.lastOpts) != 4 {
		t.Fatalf("expected 4 task options, got %d", len(client.lastOpts))
	}
	assertAsynqOption(t, client.lastOpts[0], asynq.QueueOpt, "critical")
	assertAsynqOption(t, client.lastOpts[1], asynq.ProcessAtOpt, processAt)
	assertAsynqOption(t, client.lastOpts[2], asynq.MaxRetryOpt, 3)
	assertAsynqOption(t, client.lastOpts[3], asynq.TimeoutOpt, defaultTaskTimeout)
}

func TestBrokerEnqueueAppliesDefaultTaskTimeout(t *testing.T) {
	cfg, err := NewConfig(WithTaskTimeoutOption(45 * time.Second))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubBrokerClient{}
	broker, err := newBroker(cfg, func(Config) (brokerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected broker error: %v", err)
	}

	if _, err := broker.Enqueue(context.Background(), "email:send", map[string]string{"id": "7"}); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	if len(client.lastOpts) != 1 {
		t.Fatalf("expected 1 default task option, got %d", len(client.lastOpts))
	}

	assertAsynqOption(t, client.lastOpts[0], asynq.TimeoutOpt, 45*time.Second)
}

func TestBrokerCloseWaitsForInflightEnqueue(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubBrokerClient{
		started:      make(chan struct{}),
		blockEnqueue: make(chan struct{}),
	}
	broker, err := newBroker(cfg, func(Config) (brokerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected broker error: %v", err)
	}

	enqueueDone := make(chan error, 1)
	go func() {
		_, err := broker.Enqueue(context.Background(), "email:send", map[string]string{"id": "9"})
		enqueueDone <- err
	}()

	<-client.started

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- broker.Close()
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("close returned before inflight enqueue completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(client.blockEnqueue)

	if err := <-enqueueDone; err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("expected client close to be called once, got %d", client.closeCalls)
	}
}

func TestBrokerShutdownRespectsContext(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubBrokerClient{
		started:      make(chan struct{}),
		blockEnqueue: make(chan struct{}),
	}
	broker, err := newBroker(cfg, func(Config) (brokerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected broker error: %v", err)
	}

	enqueueDone := make(chan error, 1)
	go func() {
		_, err := broker.Enqueue(context.Background(), "email:send", map[string]string{"id": "10"})
		enqueueDone <- err
	}()

	<-client.started

	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := broker.Shutdown(shutdownCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}

	close(client.blockEnqueue)

	if err := <-enqueueDone; err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}
	if err := broker.Close(); err != nil {
		t.Fatalf("unexpected close error after cancellation: %v", err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("expected client close to be called once, got %d", client.closeCalls)
	}
}
