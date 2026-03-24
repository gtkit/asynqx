package asynqx

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/hibiken/asynq"
)

// Broker 负责投递任务到 asynq。
type Broker struct {
	cfg    Config
	client atomic.Pointer[brokerClientRef]
	closed atomic.Bool
	active atomic.Int64
}

type brokerClient interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type brokerClientFactory func(Config) (brokerClient, error)

type brokerClientRef struct {
	client brokerClient
}

// NewBroker 基于共享配置创建任务投递器。
func NewBroker(opts ...BrokerOption) (*Broker, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	return newBroker(cfg, func(cfg Config) (brokerClient, error) {
		return asynq.NewClient(cfg.Redis), nil
	})
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

	broker := &Broker{cfg: cfg.clone()}
	broker.client.Store(&brokerClientRef{client: client})

	return broker, nil
}

// Enqueue 编码 payload 并投递任务。
func (b *Broker) Enqueue(ctx context.Context, taskType string, payload any, opts ...TaskOption) (*asynq.TaskInfo, error) {
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
	if b == nil {
		return nil
	}
	if !b.closed.CompareAndSwap(false, true) {
		return nil
	}

	clientRef := b.client.Swap(nil)
	if clientRef == nil || clientRef.client == nil {
		return nil
	}

	for b.active.Load() > 0 {
		runtime.Gosched()
	}

	return clientRef.client.Close()
}

func (b *Broker) acquireClient() (*brokerClientRef, func(), error) {
	for {
		if b.closed.Load() {
			return nil, nil, ErrClosed
		}

		current := b.active.Load()
		if !b.active.CompareAndSwap(current, current+1) {
			continue
		}

		if b.closed.Load() {
			b.active.Add(-1)
			return nil, nil, ErrClosed
		}

		clientRef := b.client.Load()
		if clientRef == nil || clientRef.client == nil {
			b.active.Add(-1)
			return nil, nil, ErrClosed
		}

		return clientRef, func() {
			b.active.Add(-1)
		}, nil
	}
}
