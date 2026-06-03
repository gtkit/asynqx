package asynqx

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// resolveRedisClient 返回组件应使用的 redis 客户端及其归属。
// 当 cfg.RedisClient 非 nil 时复用外部传入的共享客户端（owned=false，asynqx 不负责关闭）；
// 否则按 cfg.Redis 连接参数新建客户端（owned=true，asynqx 负责关闭）。
func resolveRedisClient(cfg Config) (redis.UniversalClient, bool, error) {
	if !isNilInterface(cfg.RedisClient) {
		return cfg.RedisClient, false, nil
	}

	client, err := newRedisUniversalClient(cfg.Redis)
	if err != nil {
		return nil, false, err
	}

	return client, true, nil
}

func newRedisUniversalClient(opt asynq.RedisConnOpt) (redis.UniversalClient, error) {
	if opt == nil {
		return nil, invalidConfigurationError("redis", "must not be nil")
	}

	client, ok := opt.MakeRedisClient().(redis.UniversalClient)
	if !ok {
		return nil, invalidConfigurationError("redis", fmt.Sprintf("unsupported option type %T", opt))
	}

	return client, nil
}

func pingRedisOnStart(ctx context.Context, client redis.UniversalClient, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	err := client.Ping(ctx).Err()
	if err != nil {
		return fmt.Errorf("ping redis on start: %w", err)
	}

	return nil
}

func pingRedisOptionOnStart(ctx context.Context, opt asynq.RedisConnOpt, timeout time.Duration) error {
	client, err := newRedisUniversalClient(opt)
	if err != nil {
		return err
	}
	defer client.Close()

	return pingRedisOnStart(ctx, client, timeout)
}
