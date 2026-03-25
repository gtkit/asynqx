package asynqx

import (
	"crypto/tls"
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

// WithRedisAddrOption 设置共享配置中的 Redis 地址。
func WithRedisAddrOption(addr string) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis.Addr = addr
		return nil
	}
}

// WithRedisUserOption 设置共享配置中的 Redis 用户名。
func WithRedisUserOption(userName string) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis.Username = userName
		return nil
	}
}

// WithRedisPasswordOption 设置共享配置中的 Redis 密码。
func WithRedisPasswordOption(password string) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis.Password = password
		return nil
	}
}

// WithRedisDBOption 设置共享配置中的 Redis 数据库编号。
func WithRedisDBOption(db int) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis.DB = db
		return nil
	}
}

// WithRedisPoolSizeOption 设置共享配置中的 Redis 连接池大小。
func WithRedisPoolSizeOption(size int) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis.PoolSize = size
		return nil
	}
}

// WithDialTimeoutOption 设置共享配置中的 Redis 拨号超时。
func WithDialTimeoutOption(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis.DialTimeout = timeout
		return nil
	}
}

// WithReadTimeoutOption 设置共享配置中的 Redis 读取超时。
func WithReadTimeoutOption(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis.ReadTimeout = timeout
		return nil
	}
}

// WithWriteTimeoutOption 设置共享配置中的 Redis 写入超时。
func WithWriteTimeoutOption(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.Redis.WriteTimeout = timeout
		return nil
	}
}

// WithTLSConfigOption 设置共享配置中的 Redis TLS 配置。
func WithTLSConfigOption(cfg *tls.Config) ConfigOption {
	return func(target *Config) error {
		target.Redis.TLSConfig = cloneTLSConfig(cfg)
		return nil
	}
}

// WithConcurrencyOption 设置共享配置中的并发数。
func WithConcurrencyOption(concurrency int) ConfigOption {
	return func(cfg *Config) error {
		cfg.Concurrency = concurrency
		return nil
	}
}

// WithQueuesOption 设置共享配置中的队列权重，并复制底层 map。
func WithQueuesOption(queues map[string]int) ConfigOption {
	return func(cfg *Config) error {
		cfg.Queues = copyQueueWeights(queues)
		return nil
	}
}

// WithRetryDelayFuncOption 设置共享配置中的重试延迟函数。
func WithRetryDelayFuncOption(fn asynq.RetryDelayFunc) ConfigOption {
	return func(cfg *Config) error {
		cfg.RetryDelayFunc = fn
		return nil
	}
}

// WithStrictPriorityOption 设置共享配置中的严格优先级。
func WithStrictPriorityOption(val bool) ConfigOption {
	return func(cfg *Config) error {
		cfg.StrictPriority = val
		return nil
	}
}

// WithErrorHandlerOption 设置共享配置中的错误处理器。
func WithErrorHandlerOption(fn asynq.ErrorHandler) ConfigOption {
	return func(cfg *Config) error {
		cfg.ErrorHandler = fn
		return nil
	}
}

// WithHealthCheckFuncOption 设置共享配置中的健康检查回调。
func WithHealthCheckFuncOption(fn func(error)) ConfigOption {
	return func(cfg *Config) error {
		cfg.HealthCheckFunc = fn
		return nil
	}
}

// WithHealthCheckIntervalOption 设置共享配置中的健康检查间隔。
func WithHealthCheckIntervalOption(interval time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.HealthCheckInterval = interval
		return nil
	}
}

// WithShutdownTimeoutOption 设置共享配置中的优雅关闭超时时间。
func WithShutdownTimeoutOption(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.ShutdownTimeout = timeout
		return nil
	}
}

// WithDelayedTaskCheckIntervalOption 设置共享配置中的延迟任务检查间隔。
func WithDelayedTaskCheckIntervalOption(interval time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.DelayedTaskCheckInterval = interval
		return nil
	}
}

// WithGroupGracePeriodOption 设置共享配置中的聚合宽限期。
func WithGroupGracePeriodOption(interval time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.GroupGracePeriod = interval
		return nil
	}
}

// WithGroupMaxDelayOption 设置共享配置中的聚合最大延迟。
func WithGroupMaxDelayOption(interval time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.GroupMaxDelay = interval
		return nil
	}
}

// WithGroupMaxSizeOption 设置共享配置中的聚合最大尺寸。
func WithGroupMaxSizeOption(size int) ConfigOption {
	return func(cfg *Config) error {
		cfg.GroupMaxSize = size
		return nil
	}
}

// WithMiddlewareOption 设置共享配置中的中间件。
func WithMiddlewareOption(middlewares ...asynq.MiddlewareFunc) ConfigOption {
	return func(cfg *Config) error {
		cfg.Middleware = append([]asynq.MiddlewareFunc(nil), middlewares...)
		return nil
	}
}

// WithLocationOption 设置共享配置中的时区位置。
func WithLocationOption(name string) ConfigOption {
	return func(cfg *Config) error {
		loc, err := time.LoadLocation(name)
		if err != nil {
			return invalidConfigurationError("location", err.Error())
		}
		cfg.Location = loc
		return nil
	}
}

// WithIsFailureOption 设置共享配置中的失败判定函数。
func WithIsFailureOption(fn func(error) bool) ConfigOption {
	return func(cfg *Config) error {
		cfg.IsFailure = fn
		return nil
	}
}

// WithTaskTimeoutOption 设置共享配置中的默认任务超时时间。
func WithTaskTimeoutOption(timeout time.Duration) ConfigOption {
	return func(cfg *Config) error {
		cfg.TaskTimeout = timeout
		return nil
	}
}

// WithLoggerOption 设置共享配置中的日志实现。
func WithLoggerOption(log Logger) ConfigOption {
	return func(cfg *Config) error {
		cfg.Logger = log
		return nil
	}
}
