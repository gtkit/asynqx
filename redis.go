package asynqx

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

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
