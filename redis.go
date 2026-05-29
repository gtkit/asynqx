package asynqx

import (
	"fmt"

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
