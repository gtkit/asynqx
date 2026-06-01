package asynqx

import (
	"crypto/tls"
	"strings"
	"time"

	"github.com/hibiken/asynq"
)

// ConfigOption 表示作用于共享基础配置的选项。
type ConfigOption func(*Config) error

// ProducerOption 表示 Producer 使用的配置选项。
type ProducerOption = ConfigOption

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

// WithGroupAggregator 设置任务聚合器。
// 任务聚合（WithTaskGroup 投递的分组任务）必须配置聚合器才会真正生效：
// 未设置时 asynq 不会启动聚合协程，分组任务将滞留在 group 中不被处理。
func WithGroupAggregator(aggregator asynq.GroupAggregator) ConfigOption {
	return func(cfg *Config) error {
		if isNilInterface(aggregator) {
			return invalidConfigurationError("group_aggregator", "must not be nil")
		}

		cfg.GroupAggregator = aggregator

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

// WithSchedulerPostEnqueueFunc 设置调度器每次投递周期任务后的回调，用于观测投递结果。
// 仅对 Scheduler 生效；回调在每次投递后触发，err 非 nil 表示该次周期任务投递失败，
// 可据此对"定时任务未能投递"做告警，避免失败被静默吞掉。回调应快速返回，不要阻塞。
func WithSchedulerPostEnqueueFunc(fn func(info *asynq.TaskInfo, err error)) ConfigOption {
	return func(cfg *Config) error {
		cfg.SchedulerPostEnqueueFunc = fn

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

// WithPingTimeout 设置启动探活的等待超时时间。
// 传入 0 表示不额外设置超时，直接使用 Redis 客户端自身的超时配置。
func WithPingTimeout(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.PingTimeout = timeout

		return nil
	}
}

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
