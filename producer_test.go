package asynqx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

type stubProducerClient struct {
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

func (s *stubProducerClient) EnqueueContext(
	ctx context.Context,
	task *asynq.Task,
	opts ...asynq.Option,
) (*asynq.TaskInfo, error) {
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

func (s *stubProducerClient) Close() error {
	s.closeCalls++

	return s.closeErr
}

func TestProducerCloseIsIdempotent(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubProducerClient{}

	producer, err := newProducer(cfg, func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	err = producer.Close()
	if err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	err = producer.Close()
	if err != nil {
		t.Fatalf("unexpected second close error: %v", err)
	}

	if client.closeCalls != 1 {
		t.Fatalf("expected client close to be called once, got %d", client.closeCalls)
	}
}

func TestNewProducerUsesConfiguredFactory(t *testing.T) {
	client := &stubProducerClient{}

	restore := setProducerClientFactoryForTest(func(Config) (producerClient, error) {
		return client, nil
	})
	defer restore()

	producer, err := NewProducer()
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	err = producer.Close()
	if err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	if client.closeCalls != 1 {
		t.Fatalf("expected configured factory client to be closed once, got %d", client.closeCalls)
	}
}

func TestNewProducerFromConfigUsesConfig(t *testing.T) {
	cfg, err := NewConfig(WithDefaultTaskTimeout(45 * time.Second))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubProducerClient{}

	restore := setProducerClientFactoryForTest(func(got Config) (producerClient, error) {
		if got.TaskTimeout != 45*time.Second {
			t.Fatalf("expected task timeout 45s, got %v", got.TaskTimeout)
		}

		return client, nil
	})
	defer restore()

	producer, err := NewProducerFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	if err = producer.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
}

func TestProducerEnqueueAfterCloseReturnsErrClosed(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubProducerClient{}

	producer, err := newProducer(cfg, func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	err = producer.Close()
	if err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	_, err = producer.Enqueue(context.Background(), "email:send", map[string]string{"id": "1"})
	if err == nil {
		t.Fatal("expected ErrClosed")
	}

	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestProducerEnqueueMarshalsPayloadAndPassesOptions(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubProducerClient{}

	producer, err := newProducer(cfg, func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	processAt := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)

	info, err := producer.Enqueue(
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

func TestProducerEnqueueAppliesDefaultTaskTimeout(t *testing.T) {
	cfg, err := NewConfig(WithDefaultTaskTimeout(45 * time.Second))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubProducerClient{}

	producer, err := newProducer(cfg, func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	_, err = producer.Enqueue(context.Background(), "email:send", map[string]string{"id": "7"})
	if err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	if len(client.lastOpts) != 1 {
		t.Fatalf("expected 1 default task option, got %d", len(client.lastOpts))
	}

	assertAsynqOption(t, client.lastOpts[0], asynq.TimeoutOpt, 45*time.Second)
}

func TestProducerEnqueueAllowsDisablingDefaultTaskTimeout(t *testing.T) {
	cfg, err := NewConfig(WithDefaultTaskTimeout(0))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubProducerClient{}

	producer, err := newProducer(cfg, func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	_, err = producer.Enqueue(context.Background(), "email:send", map[string]string{"id": "7"})
	if err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	if len(client.lastOpts) != 0 {
		t.Fatalf("expected no default task option, got %d", len(client.lastOpts))
	}
}

func TestProducerCloseWaitsForInflightEnqueue(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubProducerClient{
		started:      make(chan struct{}),
		blockEnqueue: make(chan struct{}),
	}

	producer, err := newProducer(cfg, func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	enqueueDone := make(chan error, 1)

	go func() {
		_, enqueueErr := producer.Enqueue(context.Background(), "email:send", map[string]string{"id": "9"})
		enqueueDone <- enqueueErr
	}()

	<-client.started

	closeDone := make(chan error, 1)

	go func() {
		closeDone <- producer.Close()
	}()

	select {
	case closeErr := <-closeDone:
		t.Fatalf("close returned before inflight enqueue completed: %v", closeErr)
	case <-time.After(20 * time.Millisecond):
	}

	close(client.blockEnqueue)

	err = <-enqueueDone
	if err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	err = <-closeDone
	if err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	if client.closeCalls != 1 {
		t.Fatalf("expected client close to be called once, got %d", client.closeCalls)
	}
}

func TestProducerShutdownRespectsContext(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	client := &stubProducerClient{
		started:      make(chan struct{}),
		blockEnqueue: make(chan struct{}),
	}

	producer, err := newProducer(cfg, func(Config) (producerClient, error) {
		return client, nil
	})
	if err != nil {
		t.Fatalf("unexpected producer error: %v", err)
	}

	enqueueDone := make(chan error, 1)

	go func() {
		_, enqueueErr := producer.Enqueue(context.Background(), "email:send", map[string]string{"id": "10"})
		enqueueDone <- enqueueErr
	}()

	<-client.started

	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err = producer.Shutdown(shutdownCtx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}

	close(client.blockEnqueue)

	err = <-enqueueDone
	if err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	err = producer.Close()
	if err != nil {
		t.Fatalf("unexpected close error after cancellation: %v", err)
	}

	if client.closeCalls != 1 {
		t.Fatalf("expected client close to be called once, got %d", client.closeCalls)
	}
}
