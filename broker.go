package asynqx

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hibiken/asynq"
)

// Broker 负责投递任务到 asynq。
type Broker struct {
	cfg       Config
	client    atomic.Pointer[brokerClientRef]
	closed    atomic.Bool
	active    activityCounter
	closeDone chan struct{}
	closeMu   sync.Mutex
	closeErr  error
}

type brokerClient interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type brokerClientFactory func(Config) (brokerClient, error)

type brokerClientRef struct {
	client brokerClient
}

// NewBroker 基于共享配置创建任务投递器。
// 调用成功后，调用方应调用 Close 或 Shutdown 释放底层资源。
func NewBroker(opts ...BrokerOption) (*Broker, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	return newBroker(cfg, defaultBrokerClientFactory)
}

var defaultBrokerClientFactory brokerClientFactory = func(cfg Config) (brokerClient, error) {
	return asynq.NewClient(cfg.Redis), nil
}

func newBroker(cfg Config, factory brokerClientFactory) (*Broker, error) {
	if factory == nil {
		return nil, invalidConfigurationError("broker.client_factory", "must not be nil")
	}

	client, err := factory(cfg.clone())
	if err != nil {
		return nil, err
	}

	if client == nil {
		return nil, invalidConfigurationError("broker.client", "must not be nil")
	}

	broker := &Broker{
		cfg:       cfg.clone(),
		active:    newActivityCounter(),
		closeDone: make(chan struct{}),
	}
	broker.client.Store(&brokerClientRef{client: client})

	return broker, nil
}

// Enqueue 编码 payload 并投递任务。
func (b *Broker) Enqueue(
	ctx context.Context,
	taskType string,
	payload any,
	opts ...TaskOption,
) (*asynq.TaskInfo, error) {
	if b == nil {
		return nil, ErrClosed
	}

	if strings.TrimSpace(taskType) == "" {
		return nil, invalidArgumentError("task_type", "must not be empty")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	clientRef, release, err := b.acquireClient()
	if err != nil {
		return nil, err
	}
	defer release()

	body, err := marshalPayload(payload)
	if err != nil {
		return nil, err
	}

	taskOpts, err := buildTaskOptions(opts...)
	if err != nil {
		return nil, err
	}

	taskOpts = applyDefaultTaskTimeout(taskOpts, b.cfg.TaskTimeout)

	task := asynq.NewTask(taskType, body)

	return clientRef.client.EnqueueContext(ctx, task, taskOpts...)
}

// Close 关闭 Broker，重复调用是安全的。
func (b *Broker) Close() error {
	return b.Shutdown(context.Background())
}

// Shutdown 关闭 Broker，并等待在途投递完成或 ctx 取消。
func (b *Broker) Shutdown(ctx context.Context) error {
	if b == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if b.closed.CompareAndSwap(false, true) {
		clientRef := b.client.Swap(nil)
		go b.finishClose(clientRef)
	}

	select {
	case <-b.closeDone:
		b.closeMu.Lock()
		defer b.closeMu.Unlock()

		return b.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Broker) acquireClient() (*brokerClientRef, func(), error) {
	if b.closed.Load() {
		return nil, nil, ErrClosed
	}

	b.active.Add()

	if b.closed.Load() {
		b.active.Done()

		return nil, nil, ErrClosed
	}

	clientRef := b.client.Load()
	if clientRef == nil || clientRef.client == nil {
		b.active.Done()

		return nil, nil, ErrClosed
	}

	return clientRef, func() {
		b.active.Done()
	}, nil
}

func (b *Broker) finishClose(clientRef *brokerClientRef) {
	defer close(b.closeDone)

	b.active.Wait()

	if clientRef == nil || clientRef.client == nil {
		return
	}

	err := clientRef.client.Close()

	b.closeMu.Lock()
	b.closeErr = err
	b.closeMu.Unlock()
}
