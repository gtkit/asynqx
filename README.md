# asynqx

`asynqx` 是一个面向生产项目的 `asynq` 封装，围绕三个清晰角色组织能力：

- `Broker`：投递任务
- `Worker`：消费任务
- `Scheduler`：注册周期任务

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

## 架构说明

### Broker

`Broker` 只负责任务投递。

公开方法：

- `NewBroker(opts ...BrokerOption) (*Broker, error)`
- `(*Broker).Enqueue(ctx, taskType, payload, opts...)`
- `(*Broker).Close() error`

语义：

- `Close` 幂等
- 关闭后再次投递返回 `ErrClosed`
- `Enqueue` 支持 `context.Context`

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

## 配置方法

### 共享配置选项

这些选项可同时用于 `NewBroker`、`NewWorker`、`NewScheduler`：

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

### Worker 相关配置

- `WithConcurrencyOption(concurrency int)`
- `WithQueuesOption(queues map[string]int)`
- `WithRetryDelayFuncOption(fn asynq.RetryDelayFunc)`
- `WithStrictPriorityOption(val bool)`
- `WithErrorHandlerOption(fn asynq.ErrorHandler)`
- `WithHealthCheckFuncOption(fn func(error))`
- `WithHealthCheckIntervalOption(interval time.Duration)`
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
- `Broker` 使用原子计数保证 `Enqueue` 与 `Close` 不会并发操作同一个底层 client
- `Worker` 和 `Scheduler` 使用显式状态机处理 `Start/Shutdown` 竞态
- 处理器注册只允许在 `Worker` 启动前完成
- 核心运行路径尽量避免显式锁；仅在底层依赖或测试框架需要时使用最小同步原语

## 生产环境建议

- 明确区分生产者进程、消费者进程、调度进程，不要把所有角色强塞进一个服务
- 为不同业务队列配置合理的 `Queues` 权重
- 配置任务超时、重试次数、唯一窗口，避免无界重试
- 为 Redis 配置认证、TLS、连接池和超时
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
