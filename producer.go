package asynqx

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hibiken/asynq"
)

// Producer 负责投递任务到 asynq。
type Producer struct {
	cfg        Config
	client     atomic.Pointer[producerClientRef]
	ownsClient bool
	closed     atomic.Bool
	active     activityCounter
	closeDone  chan struct{}
	closeMu    sync.Mutex
	closeErr   error
}

type producerClient interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type producerClientFactory func(Config) (producerClient, error)

type producerClientRef struct {
	client producerClient
}

// NewProducer 基于共享配置创建任务投递器。
// 调用成功后，调用方应调用 Close 或 Shutdown 释放底层资源。
func NewProducer(opts ...ProducerOption) (*Producer, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	return newProducer(cfg, defaultProducerClientFactory)
}

// NewProducerFromConfig 基于已构造的共享配置创建任务投递器。
// 调用成功后，调用方应调用 Close 或 Shutdown 释放底层资源。
func NewProducerFromConfig(cfg Config) (*Producer, error) {
	cfg = cfg.clone()

	err := cfg.validate()
	if err != nil {
		return nil, err
	}

	return newProducer(cfg, defaultProducerClientFactory)
}

var defaultProducerClientFactory producerClientFactory = func(cfg Config) (producerClient, error) {
	if !isNilInterface(cfg.RedisClient) {
		if cfg.PingOnStart {
			// 外部共享客户端，Ping 后不关闭，生命周期由调用方负责。
			err := pingRedisOnStart(context.Background(), cfg.RedisClient, cfg.PingTimeout)
			if err != nil {
				return nil, err
			}
		}

		return asynq.NewClientFromRedisClient(cfg.RedisClient), nil
	}

	if cfg.PingOnStart {
		err := pingRedisOptionOnStart(context.Background(), cfg.Redis, cfg.PingTimeout)
		if err != nil {
			return nil, err
		}
	}

	return asynq.NewClient(cfg.Redis), nil
}

func newProducer(cfg Config, factory producerClientFactory) (*Producer, error) {
	if factory == nil {
		return nil, invalidConfigurationError("producer.client_factory", "must not be nil")
	}

	client, err := factory(cfg)
	if err != nil {
		return nil, err
	}

	if client == nil {
		return nil, invalidConfigurationError("producer.client", "must not be nil")
	}

	producer := &Producer{
		cfg: cfg.clone(),
		// 外部共享客户端（WithRedisInstance）由调用方负责关闭；仅当 asynqx 自行
		// 按连接参数创建客户端时才拥有其生命周期，Close 时才真正关闭底层连接。
		ownsClient: isNilInterface(cfg.RedisClient),
		active:     newActivityCounter(),
		closeDone:  make(chan struct{}),
	}
	producer.client.Store(&producerClientRef{client: client})

	return producer, nil
}

// Enqueue 将 payload 序列化为 JSON 后投递任务。
func (b *Producer) Enqueue(
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

	if err := ctx.Err(); err != nil {
		return nil, err
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

// Close 关闭 Producer，重复调用是安全的。
func (b *Producer) Close() error {
	return b.Shutdown(context.Background())
}

// Shutdown 关闭 Producer，并等待在途投递完成或 ctx 取消。
// ctx 取消只解除调用方等待；已经进入底层 client 的投递完成后，后台关闭流程才会释放底层资源。
func (b *Producer) Shutdown(ctx context.Context) error {
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

func (b *Producer) acquireClient() (*producerClientRef, func(), error) {
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

func (b *Producer) finishClose(clientRef *producerClientRef) {
	defer close(b.closeDone)

	b.active.Wait()

	if clientRef == nil || clientRef.client == nil {
		return
	}

	// 复用外部共享客户端时，其生命周期由调用方负责：asynq 的 Client.Close 对共享
	// 连接会返回 "redis connection is shared" 错误且不会真正关闭连接。这里直接跳过，
	// 避免把这个预期内的"错误"当作关闭失败回传给调用方。
	if !b.ownsClient {
		return
	}

	err := clientRef.client.Close()

	b.closeMu.Lock()
	b.closeErr = err
	b.closeMu.Unlock()
}
