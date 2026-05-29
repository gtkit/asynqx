# asynqx

`asynqx` 是一个面向生产项目的 `asynq` 封装，围绕三个清晰角色组织能力：

- `Broker`：投递任务
- `Worker`：消费任务
- `Scheduler`：注册周期任务
- `Inspector`：检查队列状态

它的目标不是再包一层“大全对象”，而是提供更符合 Go 风格的 API、更清晰的生命周期语义，以及更容易在服务中落地的配置方式。

## 设计目标

- 基于 Go 1.26
- 公开 API 采用 option 函数风格
- 默认遵循 Go 风格：小职责、少共享状态、明确错误
- 核心生命周期使用原子状态机，尽量避免锁
- 默认测试不依赖真实 Redis，方便 CI 和本地开发
- 适合生产项目集成，而不是演示型封装

## 核心能力

- 立即任务投递
- 延迟任务投递
- 指定时间投递
- 任务超时、截止时间、最大重试、唯一任务、保留期
- 泛型任务处理器注册
- 原始 `asynq.Task` 处理器注册
- 周期任务注册与取消
- 队列状态检查器接入
- 幂等关闭
- 对配置错误、参数错误、状态错误提供明确错误类型

## 安装

```bash
go get github.com/gtkit/asynqx
```

## 快速开始

### 1. 创建 Broker

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/gtkit/asynqx"
)

type EmailPayload struct {
	UserID string `json:"user_id"`
}

func main() {
	broker, err := asynqx.NewBroker(
		asynqx.WithRedisAddrOption("127.0.0.1:6379"),
		asynqx.WithRedisDBOption(0),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer broker.Close()

	_, err = broker.Enqueue(
		context.Background(),
		"email:welcome",
		EmailPayload{UserID: "u-1001"},
		asynqx.WithTaskQueue("critical"),
		asynqx.WithTaskTimeout(30*time.Second),
		asynqx.WithTaskMaxRetry(5),
	)
	if err != nil {
		log.Fatal(err)
	}
}
```

### 2. 创建 Worker

```go
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/gtkit/asynqx"
)

type EmailPayload struct {
	UserID string `json:"user_id"`
}

func main() {
	worker, err := asynqx.NewWorker(
		asynqx.WithRedisAddrOption("127.0.0.1:6379"),
		asynqx.WithConcurrencyOption(16),
		asynqx.WithQueuesOption(map[string]int{
			"critical": 10,
			"default":  5,
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	err = asynqx.Handle(worker, "email:welcome", func(ctx context.Context, payload EmailPayload) error {
		log.Printf("send welcome email to user=%s", payload.UserID)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := worker.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
```

### 3. 创建 Scheduler

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/gtkit/asynqx"
)

type ReportPayload struct {
	Date string `json:"date"`
}

func main() {
	scheduler, err := asynqx.NewScheduler(
		asynqx.WithRedisAddrOption("127.0.0.1:6379"),
		asynqx.WithLocationOption("Asia/Shanghai"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer scheduler.Shutdown(context.Background())

	entryID, err := scheduler.Register(
		context.Background(),
		"0 */5 * * * *",
		"report:daily",
		ReportPayload{Date: time.Now().Format("2006-01-02")},
		asynqx.WithTaskQueue("default"),
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("registered scheduler entry:", entryID)

	if err := scheduler.Start(context.Background()); err != nil {
		log.Fatal(err)
	}

	select {}
}
```

### 4. 创建 Inspector

```go
package main

import (
	"log"

	"github.com/gtkit/asynqx"
)

func main() {
	inspector, err := asynqx.NewInspector(
		asynqx.WithRedisAddrOption("127.0.0.1:6379"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer inspector.Close()
}
```

## 架构说明

### Broker

`Broker` 只负责任务投递。

公开方法：

- `NewBroker(opts ...BrokerOption) (*Broker, error)`
- `(*Broker).Enqueue(ctx, taskType, payload, opts...)`
- `(*Broker).Close() error`
- `(*Broker).Shutdown(ctx) error`

语义：

- `Close` 幂等
- `Shutdown(ctx)` 允许调用方为关闭等待设置超时
- 关闭后再次投递返回 `ErrClosed`
- `Enqueue` 支持 `context.Context`

### Inspector

`Inspector` 负责创建底层 `asynq.Inspector`，用于运维检查队列、任务、重试和归档状态。

公开方法：

- `NewInspector(opts ...ConfigOption) (*Inspector, error)`
- `(*Inspector).Close() error`

语义：

- `Inspector` 使用与 `Broker`、`Worker`、`Scheduler` 相同的 Redis 配置
- 调用方不再使用时应调用 `Close`

### Worker

`Worker` 只负责处理器注册和任务消费。

公开方法：

- `NewWorker(opts ...WorkerOption) (*Worker, error)`
- `(*Worker).HandleRaw(taskType, handler) error`
- `Handle[T](worker, taskType, handler) error`
- `(*Worker).Start(ctx) error`
- `(*Worker).Run(ctx) error`
- `(*Worker).Shutdown(ctx) error`

语义：

- 启动前可以注册处理器
- 启动后继续注册返回 `ErrWorkerAlreadyRunning`
- 同一 `taskType` 重复注册返回 `ErrHandlerAlreadyRegistered`
- 停止后再次注册返回 `ErrWorkerStopped`
- `Shutdown` 幂等

### Scheduler

`Scheduler` 只负责周期任务注册和调度生命周期。

公开方法：

- `NewScheduler(opts ...SchedulerOption) (*Scheduler, error)`
- `(*Scheduler).Register(ctx, spec, taskType, payload, opts...) (string, error)`
- `(*Scheduler).Unregister(ctx, entryID) error`
- `(*Scheduler).Start(ctx) error`
- `(*Scheduler).Run(ctx) error`
- `(*Scheduler).Shutdown(ctx) error`

语义：

- 停止后注册或取消返回 `ErrSchedulerStopped`
- `Shutdown` 幂等
- `Run` 在 `ctx` 取消后会主动执行关闭流程
- `Run` 在外部取消后，会使用 `ShutdownTimeout` 作为默认关闭等待预算

## 配置方法

### 共享配置选项

这些选项可同时用于 `NewBroker`、`NewWorker`、`NewScheduler`、`NewInspector`：

- `WithRedisOption(opt asynq.RedisConnOpt)`
- `WithRedisClientOption(opt asynq.RedisClientOpt)`
- `WithRedisFailoverOption(opt asynq.RedisFailoverClientOpt)`
- `WithRedisClusterOption(opt asynq.RedisClusterClientOpt)`
- `WithRedisAddrOption(addr string)`
- `WithRedisUserOption(userName string)`
- `WithRedisPasswordOption(password string)`
- `WithRedisDBOption(db int)`
- `WithRedisPoolSizeOption(size int)`
- `WithDialTimeoutOption(timeout time.Duration)`
- `WithReadTimeoutOption(timeout time.Duration)`
- `WithWriteTimeoutOption(timeout time.Duration)`
- `WithTLSConfigOption(cfg *tls.Config)`
- `WithLocationOption(name string)`
- `WithLoggerOption(log Logger)`

### Redis 部署形态

默认配置使用单机 Redis。生产环境可以直接传入 asynq 原生连接配置：

```go
worker, err := asynqx.NewWorker(
	asynqx.WithRedisFailoverOption(asynq.RedisFailoverClientOpt{
		MasterName:    "primary",
		SentinelAddrs: []string{"10.0.0.1:26379", "10.0.0.2:26379", "10.0.0.3:26379"},
		Username:      "app",
		Password:      "secret",
		DB:            0,
	}),
)
```

```go
broker, err := asynqx.NewBroker(
	asynqx.WithRedisClusterOption(asynq.RedisClusterClientOpt{
		Addrs:    []string{"10.0.1.1:6379", "10.0.1.2:6379", "10.0.1.3:6379"},
		Username: "app",
		Password: "secret",
	}),
)
```

`WithRedisAddrOption`、`WithRedisUserOption`、`WithRedisPasswordOption` 等便捷选项只适用于单机 Redis。已经使用 Sentinel 或 Cluster 配置后，不应再叠加这些单机字段选项。

### gtkit/logger 接入示例

`Logger` 兼容 asynq 的基础日志接口，并补充格式化方法。可以在业务层放一个轻量适配器：

```go
type gtkitLoggerAdapter struct {
	log *logger.Logger
}

func (l gtkitLoggerAdapter) Debug(args ...any) { l.log.Debug(args...) }
func (l gtkitLoggerAdapter) Info(args ...any)  { l.log.Info(args...) }
func (l gtkitLoggerAdapter) Warn(args ...any)  { l.log.Warn(args...) }
func (l gtkitLoggerAdapter) Error(args ...any) { l.log.Error(args...) }
func (l gtkitLoggerAdapter) Fatal(args ...any) { l.log.Fatal(args...) }

func (l gtkitLoggerAdapter) Debugf(format string, args ...any) { l.log.Debugf(format, args...) }
func (l gtkitLoggerAdapter) Infof(format string, args ...any)  { l.log.Infof(format, args...) }
func (l gtkitLoggerAdapter) Warnf(format string, args ...any)  { l.log.Warnf(format, args...) }
func (l gtkitLoggerAdapter) Errorf(format string, args ...any) { l.log.Errorf(format, args...) }
func (l gtkitLoggerAdapter) Fatalf(format string, args ...any) { l.log.Fatalf(format, args...) }
```

### Worker 相关配置

- `WithConcurrencyOption(concurrency int)`
- `WithQueuesOption(queues map[string]int)`
- `WithRetryDelayFuncOption(fn asynq.RetryDelayFunc)`
- `WithStrictPriorityOption(val bool)`
- `WithErrorHandlerOption(fn asynq.ErrorHandler)`
- `WithHealthCheckFuncOption(fn func(error))`
- `WithHealthCheckIntervalOption(interval time.Duration)`
- `WithShutdownTimeoutOption(timeout time.Duration)`
- `WithDelayedTaskCheckIntervalOption(interval time.Duration)`
- `WithGroupGracePeriodOption(interval time.Duration)`
- `WithGroupMaxDelayOption(interval time.Duration)`
- `WithGroupMaxSizeOption(size int)`
- `WithMiddlewareOption(middlewares ...asynq.MiddlewareFunc)`
- `WithIsFailureOption(fn func(error) bool)`
- `WithTaskTimeoutOption(timeout time.Duration)`

## 任务选项

任务级配置用于 `Broker.Enqueue` 和 `Scheduler.Register`。

- `WithTaskQueue(queue string)`
- `WithTaskTimeout(timeout time.Duration)`
- `WithTaskDeadline(deadline time.Time)`
- `WithTaskDelay(delay time.Duration)`
- `WithTaskProcessAt(processAt time.Time)`
- `WithTaskMaxRetry(maxRetry int)`
- `WithTaskUnique(ttl time.Duration)`
- `WithTaskRetention(retention time.Duration)`
- `WithTaskID(taskID string)`

### 调度选项覆盖规则

- `WithTaskDelay` 和 `WithTaskProcessAt` 会互相覆盖
- 后应用的调度选项覆盖先前的调度选项

示例：

```go
_, err := broker.Enqueue(
	context.Background(),
	"job:demo",
	map[string]string{"id": "1"},
	asynqx.WithTaskDelay(time.Minute),
	asynqx.WithTaskProcessAt(time.Now().Add(10*time.Minute)),
)
```

上面的最终调度行为以 `WithTaskProcessAt` 为准。

## 调用方法

### 使用泛型处理器

适合绝大多数业务场景：

```go
err := asynqx.Handle(worker, "user:created", func(ctx context.Context, payload UserCreatedPayload) error {
	return nil
})
```

### 使用原始处理器

适合需要直接访问 `asynq.Task` 的场景：

```go
err := worker.HandleRaw("user:created", func(ctx context.Context, task *asynq.Task) error {
	return nil
})
```

### 注册周期任务

```go
entryID, err := scheduler.Register(
	context.Background(),
	"@every 1m",
	"report:refresh",
	map[string]string{"scope": "all"},
	asynqx.WithTaskQueue("default"),
)
```

### 取消周期任务

```go
err := scheduler.Unregister(context.Background(), entryID)
```

## 错误说明

可以通过 `errors.Is` 判断核心错误：

- `ErrInvalidConfiguration`
- `ErrInvalidArgument`
- `ErrInvalidTaskOption`
- `ErrClosed`
- `ErrWorkerAlreadyRunning`
- `ErrWorkerStopped`
- `ErrHandlerAlreadyRegistered`
- `ErrSchedulerAlreadyRunning`
- `ErrSchedulerStopped`

示例：

```go
if errors.Is(err, asynqx.ErrWorkerStopped) {
	// worker 已停止
}
```

## 并发安全说明

- `Broker.Close` 幂等，且关闭后会拒绝新的投递
- `Broker.Shutdown(ctx)` 可为在途投递等待设置上界
- `Broker` 在关闭期间会等待已进入底层 client 的投递调用完成，再关闭底层连接
- `Worker` 和 `Scheduler` 使用显式状态机处理 `Start/Shutdown` 竞态
- 处理器注册只允许在 `Worker` 启动前完成
- 核心运行路径尽量避免显式锁；仅在底层依赖或测试框架需要时使用最小同步原语

## 停机与超时语义

这一节描述 `ctx`、`Run`、`Shutdown` 和 `WithShutdownTimeoutOption` 的协作关系。

### 总体原则

- 调用方主动调用 `Shutdown(ctx)` 时，以调用方传入的 `ctx` 为准
- 调用方使用 `Run(ctx)` 时，`ctx` 负责“何时开始停机”
- `Run(ctx)` 进入停机阶段后，默认关闭等待预算来自 `WithShutdownTimeoutOption`
- 默认 `ShutdownTimeout` 是 `30s`
- `WithShutdownTimeoutOption(0)` 表示不额外设置默认关闭超时，内部会使用 `context.Background()`
- 如果 `Run(ctx)` 因 `ctx` 取消而触发停机且关闭成功，返回值是 `nil`；调用方需要区分退出原因时，应在外层读取传入的 `ctx.Err()`

### Broker

- `Enqueue(ctx, ...)` 的 `ctx` 只控制单次投递请求
- `Close()` 等价于 `Shutdown(context.Background())`
- `Shutdown(ctx)` 会拒绝新的投递，并等待已经进入底层 client 的投递完成
- 如果等待期间 `ctx` 超时或取消，`Shutdown(ctx)` 返回对应错误；后台关闭流程会继续完成

### Worker

- `Start(ctx)` 在真正启动前会检查 `ctx`
- 如果 `Start(ctx)` 期间 `ctx` 已取消，会直接返回，不进入底层 `asynq.Server`
- `Run(ctx)` 中的 `ctx` 只负责触发停机
- `Run(ctx)` 收到取消后，会调用内部关闭流程，并使用 `ShutdownTimeout` 作为默认等待预算
- `Shutdown(ctx)` 会等待底层 worker 退出；如果 `ctx` 先超时，返回 `context.DeadlineExceeded` 或 `context.Canceled`
- `WithShutdownTimeoutOption` 控制的是 `Run(ctx)` 触发后的默认优雅关闭预算，不覆盖显式 `Shutdown(ctx)` 传入的 `ctx`

### Scheduler

- `Start(ctx)` 当前只在进入启动前检查 `ctx`
- `Run(ctx)` 中的 `ctx` 只负责触发停机
- `Run(ctx)` 收到取消后，会使用 `ShutdownTimeout` 作为默认等待预算调用 `Shutdown`
- `Shutdown(ctx)` 会等待活跃操作结束并调用底层调度器关闭
- 如果 `Shutdown(ctx)` 先超时，会立即向调用方返回对应错误；后台停止流程仍可能继续直到自然完成

### 生产建议

- 服务主进程优先使用 `Run(signalCtx)`，并配置 `WithShutdownTimeoutOption`
- `ShutdownTimeout` 应大于业务 handler 的正常耗时上界，否则停机时任务更容易被回推 Redis
- 如果你的服务框架已经有统一停机预算，直接显式调用 `Shutdown(ctx)`，不要依赖默认预算
- 对 `Broker` 而言，若停机时不希望无限等待在途投递，应优先调用 `Shutdown(ctx)` 而不是 `Close()`

## 生产环境建议

- 明确区分生产者进程、消费者进程、调度进程，不要把所有角色强塞进一个服务
- 为不同业务队列配置合理的 `Queues` 权重
- 配置任务超时、重试次数、唯一窗口，避免无界重试
- 按业务任务耗时配置 `WithShutdownTimeoutOption`，让优雅停机预算和任务时长匹配
- 为 Redis 配置认证、TLS、连接池和超时，生产优先使用 Sentinel 或 Cluster
- 通过 `WithLoggerOption` 接入统一日志实现
- 通过 `WithMiddlewareOption` 接入 tracing、metrics、recover 等中间件
- 在服务主进程中使用 `Run(ctx)`，由外层信号管理优雅退出

## 测试

默认仓库测试不依赖真实 Redis：

```bash
go test ./...
```

竞态检测：

```bash
go test ./... -race
```

如果你要补真实 Redis 集成测试，建议：

- 使用独立的 `integration_test.go`
- 通过环境变量控制是否启用
- 不要让默认 `go test ./...` 连接真实 Redis

## 与旧版本的区别

当前版本已经移除旧的单体 `Server` API 和动态 payload 订阅接口，改为：

- `Broker`
- `Worker`
- `Scheduler`
- 泛型 `Handle[T]`
- 明确的 `ConfigOption` / `TaskOption`

这意味着：

- API 更符合 Go 风格
- 职责更清晰
- 更容易做单元测试
- 生命周期与并发语义更容易推导

## FAQ

### 为什么没有“等待任务结果”的同步接口？

因为 `asynq` 的本质是异步任务队列，不是 RPC。同步等待结果很容易把异步系统错误地包装成同步调用，增加 Redis 压力和语义复杂度。业务结果建议由业务侧自行存储和查询。

### 为什么 `Worker` 启动后不允许继续注册处理器？

因为启动后再修改 handler 集合会让生命周期和并发语义变复杂，也更容易引入竞态。统一要求在启动前注册完成，更符合生产服务的可维护性。

### 为什么默认测试不连接 Redis？

因为单元测试应当稳定、可重复、可并行。真实 Redis 依赖应当放到单独的集成测试阶段，而不是污染默认测试路径。
