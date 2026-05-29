package asynqx

import (
	"crypto/tls"
	"strings"
	"time"

	"github.com/hibiken/asynq"
)

// ConfigOption 表示作用于共享基础配置的选项。
type ConfigOption func(*Config) error

// BrokerOption 表示 Broker 使用的配置选项。
type BrokerOption = ConfigOption

// WorkerOption 表示 Worker 使用的配置选项。
type WorkerOption = ConfigOption

// SchedulerOption 表示 Scheduler 使用的配置选项。
type SchedulerOption = ConfigOption

// WithRedisAddr 设置共享配置中的 Redis 地址。
func WithRedisAddr(addr string) ConfigOption {
	return func(cfg *Config) error {
		opt, err := redisClientOptions(cfg)
		if err != nil {
			return err
		}

		opt.Addr = addr
		cfg.Redis = opt

		return nil
	}
}

// WithRedisUser 设置共享配置中的 Redis 用户名。
func WithRedisUser(userName string) ConfigOption {
	return func(cfg *Config) error {
		opt, err := redisClientOptions(cfg)
		if err != nil {
			return err
		}

		opt.Username = userName
		cfg.Redis = opt

		return nil
	}
}

// WithRedisPassword 设置共享配置中的 Redis 密码。
func WithRedisPassword(password string) ConfigOption {
	return func(cfg *Config) error {
		opt, err := redisClientOptions(cfg)
		if err != nil {
			return err
		}

		opt.Password = password
		cfg.Redis = opt

		return nil
	}
}

// WithRedisDB 设置共享配置中的 Redis 数据库编号。
func WithRedisDB(database int) ConfigOption {
	return func(cfg *Config) error {
		opt, err := redisClientOptions(cfg)
		if err != nil {
			return err
		}

		opt.DB = database
		cfg.Redis = opt

		return nil
	}
}

// WithRedisPoolSize 设置共享配置中的 Redis 连接池大小。
func WithRedisPoolSize(size int) ConfigOption {
	return func(cfg *Config) error {
		opt, err := redisClientOptions(cfg)
		if err != nil {
			return err
		}

		opt.PoolSize = size
		cfg.Redis = opt

		return nil
	}
}

// WithDialTimeout 设置共享配置中的 Redis 拨号超时。
func WithDialTimeout(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		opt, err := redisClientOptions(cfg)
		if err != nil {
			return err
		}

		opt.DialTimeout = timeout
		cfg.Redis = opt

		return nil
	}
}

// WithReadTimeout 设置共享配置中的 Redis 读取超时。
func WithReadTimeout(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		opt, err := redisClientOptions(cfg)
		if err != nil {
			return err
		}

		opt.ReadTimeout = timeout
		cfg.Redis = opt

		return nil
	}
}

// WithWriteTimeout 设置共享配置中的 Redis 写入超时。
func WithWriteTimeout(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		opt, err := redisClientOptions(cfg)
		if err != nil {
			return err
		}

		opt.WriteTimeout = timeout
		cfg.Redis = opt

		return nil
	}
}

// WithTLSConfig 设置共享配置中的 Redis TLS 配置。
func WithTLSConfig(cfg *tls.Config) ConfigOption {
	return func(target *Config) error {
		opt, err := redisClientOptions(target)
		if err != nil {
			return err
		}

		opt.TLSConfig = cloneTLSConfig(cfg)
		target.Redis = opt

		return nil
	}
}

// WithRedis 设置完整的 Redis 连接配置，支持单机、Sentinel 和 Cluster。
func WithRedis(opt asynq.RedisConnOpt) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis = cloneRedisOptions(opt)

		return nil
	}
}

// WithRedisClient 设置单机 Redis 连接配置。
func WithRedisClient(opt asynq.RedisClientOpt) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis = cloneRedisClientOptions(opt)

		return nil
	}
}

// WithRedisFailover 设置 Sentinel Redis 连接配置。
func WithRedisFailover(opt asynq.RedisFailoverClientOpt) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis = cloneRedisFailoverOptions(opt)

		return nil
	}
}

// WithRedisCluster 设置 Cluster Redis 连接配置。
func WithRedisCluster(opt asynq.RedisClusterClientOpt) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis = cloneRedisClusterOptions(opt)

		return nil
	}
}

// WithConcurrency 设置共享配置中的并发数。
func WithConcurrency(concurrency int) ConfigOption {
	return func(cfg *Config) error {
		cfg.Concurrency = concurrency

		return nil
	}
}

// WithQueues 设置共享配置中的队列权重，并复制底层 map。
func WithQueues(queues map[string]int) ConfigOption {
	return func(cfg *Config) error {
		cfg.Queues = copyQueueWeights(queues)

		return nil
	}
}

// WithRetryDelayFunc 设置共享配置中的重试延迟函数。
func WithRetryDelayFunc(fn asynq.RetryDelayFunc) ConfigOption {
	return func(cfg *Config) error {
		cfg.RetryDelayFunc = fn

		return nil
	}
}

// WithStrictPriority 设置共享配置中的严格优先级。
func WithStrictPriority(val bool) ConfigOption {
	return func(cfg *Config) error {
		cfg.StrictPriority = val

		return nil
	}
}

// WithErrorHandler 设置共享配置中的错误处理器。
func WithErrorHandler(fn asynq.ErrorHandler) ConfigOption {
	return func(cfg *Config) error {
		cfg.ErrorHandler = fn

		return nil
	}
}

// WithHealthCheckFunc 设置共享配置中的健康检查回调。
func WithHealthCheckFunc(fn func(error)) ConfigOption {
	return func(cfg *Config) error {
		cfg.HealthCheckFunc = fn

		return nil
	}
}

// WithHealthCheckInterval 设置共享配置中的健康检查间隔。
func WithHealthCheckInterval(interval time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.HealthCheckInterval = interval

		return nil
	}
}

// WithShutdownTimeout 设置共享配置中的优雅关闭超时时间。
func WithShutdownTimeout(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.ShutdownTimeout = timeout

		return nil
	}
}

// WithDelayedTaskCheckInterval 设置共享配置中的延迟任务检查间隔。
func WithDelayedTaskCheckInterval(interval time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.DelayedTaskCheckInterval = interval

		return nil
	}
}

// WithGroupGracePeriod 设置共享配置中的聚合宽限期。
func WithGroupGracePeriod(interval time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.GroupGracePeriod = interval

		return nil
	}
}

// WithGroupMaxDelay 设置共享配置中的聚合最大延迟。
func WithGroupMaxDelay(interval time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.GroupMaxDelay = interval

		return nil
	}
}

// WithGroupMaxSize 设置共享配置中的聚合最大尺寸。
func WithGroupMaxSize(size int) ConfigOption {
	return func(cfg *Config) error {
		cfg.GroupMaxSize = size

		return nil
	}
}

// WithMiddleware 设置共享配置中的中间件。
func WithMiddleware(middlewares ...asynq.MiddlewareFunc) ConfigOption {
	return func(cfg *Config) error {
		cfg.Middleware = append([]asynq.MiddlewareFunc(nil), middlewares...)

		return nil
	}
}

// WithLocation 设置共享配置中的时区位置。
func WithLocation(name string) ConfigOption {
	return func(cfg *Config) error {
		if strings.TrimSpace(name) == "" {
			return invalidConfigurationError("location", "must not be empty")
		}

		loc, err := time.LoadLocation(name)
		if err != nil {
			return invalidConfigurationError("location", err.Error())
		}

		cfg.Location = loc

		return nil
	}
}

// WithIsFailure 设置共享配置中的失败判定函数。
func WithIsFailure(fn func(error) bool) ConfigOption {
	return func(cfg *Config) error {
		cfg.IsFailure = fn

		return nil
	}
}

// WithDefaultTaskTimeout 设置共享配置中的默认任务超时时间。
func WithDefaultTaskTimeout(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.TaskTimeout = timeout

		return nil
	}
}

// WithLogger 设置共享配置中的日志实现。
func WithLogger(log Logger) ConfigOption {
	return func(cfg *Config) error {
		cfg.Logger = log

		return nil
	}
}

// WithPingOnStart 设置组件创建时是否立即探测 Redis 连接。
func WithPingOnStart(enabled bool) ConfigOption {
	return func(cfg *Config) error {
		cfg.PingOnStart = enabled

		return nil
	}
}

// WithRedisOption 已弃用：请改用 WithRedis。
//
// Deprecated: 请改用 WithRedis。
var WithRedisOption = WithRedis

// WithRedisAddrOption 已弃用：请改用 WithRedisAddr。
//
// Deprecated: 请改用 WithRedisAddr。
var WithRedisAddrOption = WithRedisAddr

// WithRedisUserOption 已弃用：请改用 WithRedisUser。
//
// Deprecated: 请改用 WithRedisUser。
var WithRedisUserOption = WithRedisUser

// WithRedisPasswordOption 已弃用：请改用 WithRedisPassword。
//
// Deprecated: 请改用 WithRedisPassword。
var WithRedisPasswordOption = WithRedisPassword

// WithRedisDBOption 已弃用：请改用 WithRedisDB。
//
// Deprecated: 请改用 WithRedisDB。
var WithRedisDBOption = WithRedisDB

// WithRedisPoolSizeOption 已弃用：请改用 WithRedisPoolSize。
//
// Deprecated: 请改用 WithRedisPoolSize。
var WithRedisPoolSizeOption = WithRedisPoolSize

// WithDialTimeoutOption 已弃用：请改用 WithDialTimeout。
//
// Deprecated: 请改用 WithDialTimeout。
var WithDialTimeoutOption = WithDialTimeout

// WithReadTimeoutOption 已弃用：请改用 WithReadTimeout。
//
// Deprecated: 请改用 WithReadTimeout。
var WithReadTimeoutOption = WithReadTimeout

// WithWriteTimeoutOption 已弃用：请改用 WithWriteTimeout。
//
// Deprecated: 请改用 WithWriteTimeout。
var WithWriteTimeoutOption = WithWriteTimeout

// WithTLSConfigOption 已弃用：请改用 WithTLSConfig。
//
// Deprecated: 请改用 WithTLSConfig。
var WithTLSConfigOption = WithTLSConfig

// WithRedisClientOption 已弃用：请改用 WithRedisClient 或 WithRedis。
//
// Deprecated: 请改用 WithRedisClient 或 WithRedis。
var WithRedisClientOption = WithRedisClient

// WithRedisFailoverOption 已弃用：请改用 WithRedisFailover 或 WithRedis。
//
// Deprecated: 请改用 WithRedisFailover 或 WithRedis。
var WithRedisFailoverOption = WithRedisFailover

// WithRedisClusterOption 已弃用：请改用 WithRedisCluster 或 WithRedis。
//
// Deprecated: 请改用 WithRedisCluster 或 WithRedis。
var WithRedisClusterOption = WithRedisCluster

// WithConcurrencyOption 已弃用：请改用 WithConcurrency。
//
// Deprecated: 请改用 WithConcurrency。
var WithConcurrencyOption = WithConcurrency

// WithQueuesOption 已弃用：请改用 WithQueues。
//
// Deprecated: 请改用 WithQueues。
var WithQueuesOption = WithQueues

// WithRetryDelayFuncOption 已弃用：请改用 WithRetryDelayFunc。
//
// Deprecated: 请改用 WithRetryDelayFunc。
var WithRetryDelayFuncOption = WithRetryDelayFunc

// WithStrictPriorityOption 已弃用：请改用 WithStrictPriority。
//
// Deprecated: 请改用 WithStrictPriority。
var WithStrictPriorityOption = WithStrictPriority

// WithErrorHandlerOption 已弃用：请改用 WithErrorHandler。
//
// Deprecated: 请改用 WithErrorHandler。
var WithErrorHandlerOption = WithErrorHandler

// WithHealthCheckFuncOption 已弃用：请改用 WithHealthCheckFunc。
//
// Deprecated: 请改用 WithHealthCheckFunc。
var WithHealthCheckFuncOption = WithHealthCheckFunc

// WithHealthCheckIntervalOption 已弃用：请改用 WithHealthCheckInterval。
//
// Deprecated: 请改用 WithHealthCheckInterval。
var WithHealthCheckIntervalOption = WithHealthCheckInterval

// WithShutdownTimeoutOption 已弃用：请改用 WithShutdownTimeout。
//
// Deprecated: 请改用 WithShutdownTimeout。
var WithShutdownTimeoutOption = WithShutdownTimeout

// WithDelayedTaskCheckIntervalOption 已弃用：请改用 WithDelayedTaskCheckInterval。
//
// Deprecated: 请改用 WithDelayedTaskCheckInterval。
var WithDelayedTaskCheckIntervalOption = WithDelayedTaskCheckInterval

// WithGroupGracePeriodOption 已弃用：请改用 WithGroupGracePeriod。
//
// Deprecated: 请改用 WithGroupGracePeriod。
var WithGroupGracePeriodOption = WithGroupGracePeriod

// WithGroupMaxDelayOption 已弃用：请改用 WithGroupMaxDelay。
//
// Deprecated: 请改用 WithGroupMaxDelay。
var WithGroupMaxDelayOption = WithGroupMaxDelay

// WithGroupMaxSizeOption 已弃用：请改用 WithGroupMaxSize。
//
// Deprecated: 请改用 WithGroupMaxSize。
var WithGroupMaxSizeOption = WithGroupMaxSize

// WithMiddlewareOption 已弃用：请改用 WithMiddleware。
//
// Deprecated: 请改用 WithMiddleware。
var WithMiddlewareOption = WithMiddleware

// WithLocationOption 已弃用：请改用 WithLocation。
//
// Deprecated: 请改用 WithLocation。
var WithLocationOption = WithLocation

// WithIsFailureOption 已弃用：请改用 WithIsFailure。
//
// Deprecated: 请改用 WithIsFailure。
var WithIsFailureOption = WithIsFailure

// WithTaskTimeoutOption 已弃用：请改用 WithDefaultTaskTimeout。
//
// Deprecated: 请改用 WithDefaultTaskTimeout。
var WithTaskTimeoutOption = WithDefaultTaskTimeout

// WithLoggerOption 已弃用：请改用 WithLogger。
//
// Deprecated: 请改用 WithLogger。
var WithLoggerOption = WithLogger

// WithPingOnStartOption 已弃用：请改用 WithPingOnStart。
//
// Deprecated: 请改用 WithPingOnStart。
var WithPingOnStartOption = WithPingOnStart

func redisClientOptions(cfg *Config) (asynq.RedisClientOpt, error) {
	opt, ok := cfg.Redis.(asynq.RedisClientOpt)
	if !ok {
		return asynq.RedisClientOpt{}, invalidConfigurationError(
			"redis",
			"single-node redis option cannot update failover or cluster config",
		)
	}

	return cloneRedisClientOptions(opt), nil
}
