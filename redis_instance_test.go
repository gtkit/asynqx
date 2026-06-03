package asynqx

import (
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// stubRedisClient 通过嵌入 redis.UniversalClient 接口（值为 nil）满足类型，
// 无需实现接口的全部方法，仅用作"外部传入的共享客户端"占位。
type stubRedisClient struct {
	redis.UniversalClient
}

func TestWithRedisInstanceSetsClient(t *testing.T) {
	external := &stubRedisClient{}

	cfg, err := NewConfig(WithRedisInstance(external))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.RedisClient != external {
		t.Fatal("expected RedisClient to hold the external instance")
	}
}

func TestWithRedisInstanceRejectsNil(t *testing.T) {
	_, err := NewConfig(WithRedisInstance(nil))
	if err == nil {
		t.Fatal("expected error for nil redis client")
	}

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

// 设置外部 client 后，即便连接参数非法，validate 也应跳过连接参数校验。
func TestValidateSkipsConnOptWhenClientSet(t *testing.T) {
	cfg := Config{
		RedisClient: &stubRedisClient{},
		Redis:       nil, // 连接参数缺失，但有外部 client 时应被忽略
		Concurrency: defaultConcurrency,
		Location:    defaultConfig().Location,
	}

	err := cfg.validate()
	if err != nil {
		t.Fatalf("expected validate to skip conn opt check, got %v", err)
	}
}

func TestResolveRedisClientUsesExternalClientUnowned(t *testing.T) {
	external := &stubRedisClient{}

	client, owned, err := resolveRedisClient(Config{RedisClient: external})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if owned {
		t.Fatal("expected external client to be not owned by asynqx")
	}

	if client != external {
		t.Fatal("expected resolveRedisClient to return the external instance")
	}
}

func TestResolveRedisClientBuildsOwnedFromConnOpt(t *testing.T) {
	client, owned, err := resolveRedisClient(Config{
		Redis: asynq.RedisClientOpt{Addr: defaultRedisAddress},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !owned {
		t.Fatal("expected client built from conn opt to be owned by asynqx")
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	_ = client.Close()
}
