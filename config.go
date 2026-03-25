package asynqx

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/hibiken/asynq"
)

const (
	defaultRedisAddress = "127.0.0.1:6379"
	defaultTaskTimeout  = 5 * time.Minute
	defaultConcurrency  = 10
	defaultRedisDB      = 0
)

// Config 表示 Broker、Worker 和 Scheduler 共享的基础配置。
// 该配置在构造完成后应视为只读，用于统一创建底层 asynq 组件。
type Config struct {
	Redis                    asynq.RedisClientOpt
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
	IsFailure                func(error) bool
	Location                 *time.Location
	Logger                   Logger
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
		if err := opt(&cfg); err != nil {
			return Config{}, err
		}
	}

	cfg = cfg.clone()
	if err := cfg.validate(); err != nil {
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
		Concurrency: defaultConcurrency,
		Location:    time.Local,
		TaskTimeout: defaultTaskTimeout,
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
	if strings.TrimSpace(c.Redis.Addr) == "" {
		return invalidConfigurationError("redis.addr", "must not be empty")
	}

	if c.Redis.DB < 0 {
		return invalidConfigurationError("redis.db", "must be >= 0")
	}

	if c.Concurrency <= 0 {
		return invalidConfigurationError("concurrency", "must be > 0")
	}

	if c.ShutdownTimeout < 0 {
		return invalidConfigurationError("shutdown_timeout", "must be >= 0")
	}

	if c.TaskTimeout <= 0 {
		return invalidConfigurationError("task_timeout", "must be > 0")
	}

	if c.HealthCheckInterval < 0 {
		return invalidConfigurationError("health_check_interval", "must be >= 0")
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

	for name, weight := range c.Queues {
		if strings.TrimSpace(name) == "" {
			return invalidConfigurationError("queues", "queue name must not be empty")
		}
		if weight <= 0 {
			return invalidConfigurationError("queues."+name, "queue weight must be > 0")
		}
	}

	for i, middleware := range c.Middleware {
		if middleware == nil {
			return invalidConfigurationError(fmt.Sprintf("middleware[%d]", i), "must not be nil")
		}
	}

	if c.Location == nil {
		return invalidConfigurationError("location", "must not be nil")
	}

	return nil
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
		IsFailure:                c.IsFailure,
		Logger:                   c.Logger,
	}
}

func (c Config) schedulerOptions() *asynq.SchedulerOpts {
	return &asynq.SchedulerOpts{
		Location: c.Location,
		Logger:   c.Logger,
	}
}

func copyQueueWeights(src map[string]int) map[string]int {
	if len(src) == 0 {
		return nil
	}

	dst := make(map[string]int, len(src))
	for name, weight := range src {
		dst[name] = weight
	}

	return dst
}

func cloneRedisOptions(opt asynq.RedisClientOpt) asynq.RedisClientOpt {
	cloned := opt
	if opt.TLSConfig != nil {
		cloned.TLSConfig = opt.TLSConfig.Clone()
	}
	return cloned
}

func cloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return nil
	}
	return cfg.Clone()
}
