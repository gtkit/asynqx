package asynqx

import (
	"crypto/tls"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisAddress = "127.0.0.1:6379"
	defaultTaskTimeout  = 5 * time.Minute
	defaultShutdownTime = 30 * time.Second
	defaultConcurrency  = 10
	defaultRedisDB      = 0
)

// Config 表示 Producer、Worker 和 Scheduler 共享的基础配置。
// 该配置在构造完成后应视为只读，用于统一创建底层 asynq 组件。
//
// RedisClient 非 nil 时优先于 Redis：所有组件复用调用方传入的同一个
// go-redis 客户端（共享连接池），此时 Redis 连接参数被忽略，
// 且该客户端的生命周期由调用方负责，asynqx 的 Shutdown/Close 不会关闭它。
type Config struct {
	Redis                    asynq.RedisConnOpt
	RedisClient              redis.UniversalClient
	Concurrency              int
	Queues                   map[string]int
	RetryDelayFunc           asynq.RetryDelayFunc
	StrictPriority           bool
	ErrorHandler             asynq.ErrorHandler
	HealthCheckFunc          func(error)
	HealthCheckInterval      time.Duration
	DelayedTaskCheckInterval time.Duration
	GroupGracePeriod         time.Duration
	GroupMaxDelay            time.Duration
	GroupMaxSize             int
	GroupAggregator          asynq.GroupAggregator
	IsFailure                func(error) bool
	Location                 *time.Location
	Logger                   Logger
	SchedulerPostEnqueueFunc func(info *asynq.TaskInfo, err error)
	PingOnStart              bool
	PingTimeout              time.Duration
	ShutdownTimeout          time.Duration
	TaskTimeout              time.Duration
	Middleware               []asynq.MiddlewareFunc
}

// NewConfig 构造共享基础配置，并在返回前完成默认值填充、必要字段复制和参数校验。
// 调用方应通过 ConfigOption 传入可选配置，非法配置会返回统一错误。
func NewConfig(opts ...ConfigOption) (Config, error) {
	cfg := defaultConfig()

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		err := opt(&cfg)
		if err != nil {
			return Config{}, err
		}
	}

	cfg = cfg.clone()

	err := cfg.validate()
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		Redis: asynq.RedisClientOpt{
			Addr: defaultRedisAddress,
			DB:   defaultRedisDB,
		},
		Concurrency:     defaultConcurrency,
		Location:        time.Local,
		ShutdownTimeout: defaultShutdownTime,
		TaskTimeout:     defaultTaskTimeout,
	}
}

func (c Config) clone() Config {
	cloned := c
	cloned.Redis = cloneRedisOptions(c.Redis)
	cloned.Queues = copyQueueWeights(c.Queues)
	cloned.Middleware = append([]asynq.MiddlewareFunc(nil), c.Middleware...)

	return cloned
}

func (c Config) validate() error {
	if isNilInterface(c.RedisClient) {
		err := validateRedisOptions(c.Redis)
		if err != nil {
			return err
		}
	}

	if c.Concurrency <= 0 {
		return invalidConfigurationError("concurrency", "must be > 0")
	}

	if c.ShutdownTimeout < 0 {
		return invalidConfigurationError("shutdown_timeout", "must be >= 0")
	}

	if c.TaskTimeout < 0 {
		return invalidConfigurationError("task_timeout", "must be >= 0")
	}

	if c.HealthCheckInterval < 0 {
		return invalidConfigurationError("health_check_interval", "must be >= 0")
	}

	if c.PingTimeout < 0 {
		return invalidConfigurationError("ping_timeout", "must be >= 0")
	}

	if c.DelayedTaskCheckInterval < 0 {
		return invalidConfigurationError("delayed_task_check_interval", "must be >= 0")
	}

	if c.GroupGracePeriod < 0 {
		return invalidConfigurationError("group_grace_period", "must be >= 0")
	}

	if c.GroupGracePeriod > 0 && c.GroupGracePeriod < time.Second {
		return invalidConfigurationError("group_grace_period", "must be 0 or >= 1s")
	}

	if c.GroupMaxDelay < 0 {
		return invalidConfigurationError("group_max_delay", "must be >= 0")
	}

	if c.GroupMaxSize < 0 {
		return invalidConfigurationError("group_max_size", "must be >= 0")
	}

	err := validateQueueWeights(c.Queues)
	if err != nil {
		return err
	}

	err = validateMiddleware(c.Middleware)
	if err != nil {
		return err
	}

	if c.Location == nil {
		return invalidConfigurationError("location", "must not be nil")
	}

	return nil
}

func validateQueueWeights(queues map[string]int) error {
	for name, weight := range queues {
		if strings.TrimSpace(name) == "" {
			return invalidConfigurationError("queues", "queue name must not be empty")
		}

		if weight <= 0 {
			return invalidConfigurationError("queues."+name, "queue weight must be > 0")
		}
	}

	return nil
}

func validateMiddleware(middlewares []asynq.MiddlewareFunc) error {
	for i, middleware := range middlewares {
		if middleware == nil {
			return invalidConfigurationError(fmt.Sprintf("middleware[%d]", i), "must not be nil")
		}
	}

	return nil
}

// isNilInterface 判断接口值是否为 nil，包含"带类型的 nil"（如 (asynq.GroupAggregatorFunc)(nil)）。
// 普通 v == nil 无法识别 typed nil，调用其方法会在运行时 panic。
func isNilInterface(v any) bool {
	if v == nil {
		return true
	}

	value := reflect.ValueOf(v)

	kind := value.Kind()
	if kind == reflect.Chan || kind == reflect.Func || kind == reflect.Map ||
		kind == reflect.Pointer || kind == reflect.Slice || kind == reflect.Interface ||
		kind == reflect.UnsafePointer {
		return value.IsNil()
	}

	return false
}

func (c Config) asynqConfig() asynq.Config {
	return asynq.Config{
		Concurrency:              c.Concurrency,
		Queues:                   copyQueueWeights(c.Queues),
		RetryDelayFunc:           c.RetryDelayFunc,
		StrictPriority:           c.StrictPriority,
		ErrorHandler:             c.ErrorHandler,
		HealthCheckFunc:          c.HealthCheckFunc,
		HealthCheckInterval:      c.HealthCheckInterval,
		DelayedTaskCheckInterval: c.DelayedTaskCheckInterval,
		ShutdownTimeout:          c.ShutdownTimeout,
		GroupGracePeriod:         c.GroupGracePeriod,
		GroupMaxDelay:            c.GroupMaxDelay,
		GroupMaxSize:             c.GroupMaxSize,
		GroupAggregator:          c.GroupAggregator,
		IsFailure:                c.IsFailure,
		Logger:                   c.Logger,
	}
}

func (c Config) schedulerOptions() *asynq.SchedulerOpts {
	return &asynq.SchedulerOpts{
		Location:        c.Location,
		Logger:          c.Logger,
		PostEnqueueFunc: c.SchedulerPostEnqueueFunc,
	}
}

func copyQueueWeights(src map[string]int) map[string]int {
	if len(src) == 0 {
		return nil
	}

	dst := make(map[string]int, len(src))
	maps.Copy(dst, src)

	return dst
}

func cloneRedisOptions(opt asynq.RedisConnOpt) asynq.RedisConnOpt {
	switch redisOpt := opt.(type) {
	case asynq.RedisClientOpt:
		return cloneRedisClientOptions(redisOpt)
	case *asynq.RedisClientOpt:
		if redisOpt == nil {
			return opt
		}

		return cloneRedisClientOptions(*redisOpt)
	case asynq.RedisFailoverClientOpt:
		return cloneRedisFailoverOptions(redisOpt)
	case *asynq.RedisFailoverClientOpt:
		if redisOpt == nil {
			return opt
		}

		return cloneRedisFailoverOptions(*redisOpt)
	case asynq.RedisClusterClientOpt:
		return cloneRedisClusterOptions(redisOpt)
	case *asynq.RedisClusterClientOpt:
		if redisOpt == nil {
			return opt
		}

		return cloneRedisClusterOptions(*redisOpt)
	default:
		return redisOpt
	}
}

func cloneRedisClientOptions(opt asynq.RedisClientOpt) asynq.RedisClientOpt {
	cloned := opt
	cloned.TLSConfig = cloneTLSConfig(opt.TLSConfig)

	return cloned
}

func cloneRedisFailoverOptions(opt asynq.RedisFailoverClientOpt) asynq.RedisFailoverClientOpt {
	cloned := opt
	cloned.SentinelAddrs = slices.Clone(opt.SentinelAddrs)
	cloned.TLSConfig = cloneTLSConfig(opt.TLSConfig)

	return cloned
}

func cloneRedisClusterOptions(opt asynq.RedisClusterClientOpt) asynq.RedisClusterClientOpt {
	cloned := opt
	cloned.Addrs = slices.Clone(opt.Addrs)
	cloned.TLSConfig = cloneTLSConfig(opt.TLSConfig)

	return cloned
}

func cloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return nil
	}

	return cfg.Clone()
}

func validateRedisOptions(opt asynq.RedisConnOpt) error {
	if opt == nil {
		return invalidConfigurationError("redis", "must not be nil")
	}

	switch redisOpt := opt.(type) {
	case asynq.RedisClientOpt:
		return validateRedisClientOptions(redisOpt)
	case *asynq.RedisClientOpt:
		if redisOpt == nil {
			return invalidConfigurationError("redis", "must not be nil")
		}

		return validateRedisClientOptions(*redisOpt)
	case asynq.RedisFailoverClientOpt:
		return validateRedisFailoverOptions(redisOpt)
	case *asynq.RedisFailoverClientOpt:
		if redisOpt == nil {
			return invalidConfigurationError("redis", "must not be nil")
		}

		return validateRedisFailoverOptions(*redisOpt)
	case asynq.RedisClusterClientOpt:
		return validateRedisClusterOptions(redisOpt)
	case *asynq.RedisClusterClientOpt:
		if redisOpt == nil {
			return invalidConfigurationError("redis", "must not be nil")
		}

		return validateRedisClusterOptions(*redisOpt)
	default:
		if reflect.ValueOf(opt).Kind() == reflect.Pointer && reflect.ValueOf(opt).IsNil() {
			return invalidConfigurationError("redis", "must not be nil")
		}

		return nil
	}
}

func validateRedisClientOptions(opt asynq.RedisClientOpt) error {
	if strings.TrimSpace(opt.Addr) == "" {
		return invalidConfigurationError("redis.addr", "must not be empty")
	}

	if opt.DB < 0 {
		return invalidConfigurationError("redis.db", "must be >= 0")
	}

	return nil
}

func validateRedisFailoverOptions(opt asynq.RedisFailoverClientOpt) error {
	if strings.TrimSpace(opt.MasterName) == "" {
		return invalidConfigurationError("redis.master_name", "must not be empty")
	}

	if len(opt.SentinelAddrs) == 0 {
		return invalidConfigurationError("redis.sentinel_addrs", "must not be empty")
	}

	for i, addr := range opt.SentinelAddrs {
		if strings.TrimSpace(addr) == "" {
			return invalidConfigurationError(fmt.Sprintf("redis.sentinel_addrs[%d]", i), "must not be empty")
		}
	}

	if opt.DB < 0 {
		return invalidConfigurationError("redis.db", "must be >= 0")
	}

	return nil
}

func validateRedisClusterOptions(opt asynq.RedisClusterClientOpt) error {
	if len(opt.Addrs) == 0 {
		return invalidConfigurationError("redis.addrs", "must not be empty")
	}

	for i, addr := range opt.Addrs {
		if strings.TrimSpace(addr) == "" {
			return invalidConfigurationError(fmt.Sprintf("redis.addrs[%d]", i), "must not be empty")
		}
	}

	return nil
}
