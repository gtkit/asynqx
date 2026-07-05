package asynqx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

// TestRedisClientOptionSetters 覆盖单机 Redis 连接参数的全部 With* 设置器。
func TestRedisClientOptionSetters(t *testing.T) {
	cfg, err := NewConfig(
		WithRedisAddr("10.0.0.1:6379"),
		WithRedisUser("user-1"),
		WithRedisPassword("secret"),
		WithRedisDB(3),
		WithRedisPoolSize(20),
		WithDialTimeout(time.Second),
		WithReadTimeout(2*time.Second),
		WithWriteTimeout(3*time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	opt, ok := cfg.Redis.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("expected RedisClientOpt, got %T", cfg.Redis)
	}

	if opt.Addr != "10.0.0.1:6379" || opt.Username != "user-1" || opt.Password != "secret" ||
		opt.DB != 3 || opt.PoolSize != 20 {
		t.Fatalf("unexpected redis client option: %+v", opt)
	}

	if opt.DialTimeout != time.Second || opt.ReadTimeout != 2*time.Second || opt.WriteTimeout != 3*time.Second {
		t.Fatalf("unexpected redis timeouts: %+v", opt)
	}
}

func TestWithRedisClientSetsFullOption(t *testing.T) {
	cfg, err := NewConfig(WithRedisClient(asynq.RedisClientOpt{Addr: "10.0.0.2:6379", DB: 2}))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	opt, ok := cfg.Redis.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("expected RedisClientOpt, got %T", cfg.Redis)
	}

	if opt.Addr != "10.0.0.2:6379" || opt.DB != 2 {
		t.Fatalf("unexpected redis client option: %+v", opt)
	}
}

// TestWorkerBehaviorOptionSetters 覆盖仅对 Worker 生效的行为类选项设置器。
func TestWorkerBehaviorOptionSetters(t *testing.T) {
	retryFn := func(int, error, *asynq.Task) time.Duration { return time.Second }
	errHandler := asynq.ErrorHandlerFunc(func(context.Context, *asynq.Task, error) {})
	healthFn := func(error) {}
	isFailureFn := func(error) bool { return true }

	cfg, err := NewConfig(
		WithRetryDelayFunc(retryFn),
		WithErrorHandler(errHandler),
		WithHealthCheckFunc(healthFn),
		WithHealthCheckInterval(15*time.Second),
		WithDelayedTaskCheckInterval(5*time.Second),
		WithStrictPriority(true),
		WithGroupMaxDelay(time.Minute),
		WithGroupMaxSize(100),
		WithIsFailure(isFailureFn),
	)
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	if cfg.RetryDelayFunc == nil || cfg.ErrorHandler == nil || cfg.HealthCheckFunc == nil || cfg.IsFailure == nil {
		t.Fatal("expected function options to be set")
	}

	if cfg.HealthCheckInterval != 15*time.Second || cfg.DelayedTaskCheckInterval != 5*time.Second {
		t.Fatalf("unexpected intervals: %+v", cfg)
	}

	if !cfg.StrictPriority || cfg.GroupMaxDelay != time.Minute || cfg.GroupMaxSize != 100 {
		t.Fatalf("unexpected worker options: %+v", cfg)
	}
}

// TestNewConfigRejectsInvalidValues 表驱动覆盖 validate 的各个负值/非法分支。
func TestNewConfigRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		opt  ConfigOption
	}{
		{"zero concurrency", WithConcurrency(0)},
		{"negative shutdown timeout", WithShutdownTimeout(-time.Second)},
		{"negative task timeout", WithDefaultTaskTimeout(-time.Second)},
		{"negative health check interval", WithHealthCheckInterval(-time.Second)},
		{"negative delayed task check interval", WithDelayedTaskCheckInterval(-time.Second)},
		{"negative group grace period", WithGroupGracePeriod(-time.Second)},
		{"negative group max delay", WithGroupMaxDelay(-time.Second)},
		{"negative group max size", WithGroupMaxSize(-1)},
		{"negative ping timeout", WithPingTimeout(-time.Second)},
		{"empty queue name", WithQueues(map[string]int{" ": 1})},
		{"non-positive queue weight", WithQueues(map[string]int{"low": 0})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConfig(tc.opt)
			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
			}
		})
	}
}

// TestCloneRedisOptionsPointerVariants 覆盖三种连接形态的指针分支与 nil 指针透传。
func TestCloneRedisOptionsPointerVariants(t *testing.T) {
	clientOpt := &asynq.RedisClientOpt{Addr: "10.0.0.1:6379"}

	cloned, isClientOpt := cloneRedisOptions(clientOpt).(asynq.RedisClientOpt)
	if !isClientOpt || cloned.Addr != clientOpt.Addr {
		t.Fatalf("unexpected clone of *RedisClientOpt: %#v", cloned)
	}

	failoverOpt := &asynq.RedisFailoverClientOpt{
		MasterName:    "master",
		SentinelAddrs: []string{"10.0.0.1:26379"},
	}

	clonedFailover, isFailoverOpt := cloneRedisOptions(failoverOpt).(asynq.RedisFailoverClientOpt)
	if !isFailoverOpt || clonedFailover.MasterName != failoverOpt.MasterName {
		t.Fatalf("unexpected clone of *RedisFailoverClientOpt: %#v", clonedFailover)
	}

	if &clonedFailover.SentinelAddrs[0] == &failoverOpt.SentinelAddrs[0] {
		t.Fatal("expected sentinel addrs slice to be cloned")
	}

	clusterOpt := &asynq.RedisClusterClientOpt{Addrs: []string{"10.0.0.1:6379"}}

	clonedCluster, isClusterOpt := cloneRedisOptions(clusterOpt).(asynq.RedisClusterClientOpt)
	if !isClusterOpt || len(clonedCluster.Addrs) != 1 {
		t.Fatalf("unexpected clone of *RedisClusterClientOpt: %#v", clonedCluster)
	}

	if cloneRedisOptions((*asynq.RedisClientOpt)(nil)) == nil {
		t.Fatal("expected nil *RedisClientOpt to pass through as-is")
	}

	if cloneRedisOptions((*asynq.RedisFailoverClientOpt)(nil)) == nil {
		t.Fatal("expected nil *RedisFailoverClientOpt to pass through as-is")
	}

	if cloneRedisOptions((*asynq.RedisClusterClientOpt)(nil)) == nil {
		t.Fatal("expected nil *RedisClusterClientOpt to pass through as-is")
	}
}

// TestValidateRedisOptionsPointerVariants 覆盖 validate 对指针形态与 nil 指针的处理。
func TestValidateRedisOptionsPointerVariants(t *testing.T) {
	if err := validateRedisOptions(&asynq.RedisClientOpt{Addr: "10.0.0.1:6379"}); err != nil {
		t.Fatalf("unexpected error for valid *RedisClientOpt: %v", err)
	}

	if err := validateRedisOptions((*asynq.RedisClientOpt)(nil)); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration for nil *RedisClientOpt, got %v", err)
	}

	if err := validateRedisOptions((*asynq.RedisFailoverClientOpt)(nil)); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration for nil *RedisFailoverClientOpt, got %v", err)
	}

	if err := validateRedisOptions((*asynq.RedisClusterClientOpt)(nil)); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration for nil *RedisClusterClientOpt, got %v", err)
	}

	if err := validateRedisOptions(&asynq.RedisFailoverClientOpt{
		MasterName:    "master",
		SentinelAddrs: []string{"10.0.0.1:26379"},
	}); err != nil {
		t.Fatalf("unexpected error for valid *RedisFailoverClientOpt: %v", err)
	}

	if err := validateRedisOptions(&asynq.RedisClusterClientOpt{
		Addrs: []string{"10.0.0.1:6379"},
	}); err != nil {
		t.Fatalf("unexpected error for valid *RedisClusterClientOpt: %v", err)
	}
}
