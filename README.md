# asynqx

`asynqx` 是一个面向生产项目的 `asynq` 封装，围绕三个清晰角色组织能力：

- `Producer`：投递任务
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
- 类型安全的任务定义（`TaskType[T]`，编译期保证投递端与消费端类型一致）
- 处理器内便捷读取任务元信息（ID / 队列 / 重试状态）
- 终态失败日志处理器（`NewLogErrorHandler`，避免静默失败）
- 封顶指数退避重试预设（`CappedExponentialBackoff`）
- 周期任务注册与取消
- 队列状态检查器接入
- 幂等关闭
- 对配置错误、参数错误、状态错误提供明确错误类型

## 安装

```bash
go get github.com/gtkit/asynqx
```

## 快速开始

### 推荐：使用 App（一份配置，多种角色）

`App` 是最省心的入口：写一份配置，内部共享同一个 Redis 连接池，按需提供投递 / 消费 / 调度 / 检查能力。配合 `TaskType[T]`，做到类型安全、业务代码零 asynq 依赖。

```go
// 任务定义：单一事实来源，投递端与消费端共用
var WelcomeEmail = asynqx.NewTask[EmailPayload]("email:welcome")

// 生产者
app, err := asynqx.New(asynqx.WithRedisAddr("127.0.0.1:6379"))
if err != nil {
	log.Fatal(err)
}
defer app.Close()

_, _ = WelcomeEmail.Enqueue(context.Background(), app, EmailPayload{UserID: "u-1001"})
```

```go
// 消费者（同样一份配置写法）
app, err := asynqx.New(asynqx.WithRedisAddr("127.0.0.1:6379"))
if err != nil {
	log.Fatal(err)
}

// 注册必须在 Run 之前；handler 只见 payload，零 asynq 依赖
_ = WelcomeEmail.Handle(app, func(ctx context.Context, p EmailPayload) error {
	// 纯业务逻辑；需要元信息时用 asynqx.RetryCount(ctx) / asynqx.TaskID(ctx) 等
	return nil
})

// 启动消费、阻塞至 ctx 取消、优雅关闭全部组件与连接
if err := app.Run(ctx); err != nil {
	log.Fatal(err)
}
```

`App` 同时实现 `Enqueuer` / `Registrar` / `PeriodicRegistrar`，因此 `TaskType` 的 `Enqueue` / `Handle` / `Register` 既能传 `App`，也能传细粒度的 `Producer` / `Worker` / `Scheduler`（向后兼容）。需要底层组件时用 `app.Producer()` / `app.Worker()` / `app.Scheduler()` / `app.Inspector()`。

> `App` 让所有组件共享一个连接池，因此 `Scheduler` 关闭时 asynq 会打印一条无害的 `redis connection is shared` 日志（连接由 `App` 统一关闭）。

下面的细粒度构造器（`NewProducer` / `NewWorker` / …）适合需要单独管理各组件的进阶场景。

### 1. 创建 Producer

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
	producer, err := asynqx.NewProducer(
		asynqx.WithRedisAddr("127.0.0.1:6379"),
		asynqx.WithRedisDB(0),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer producer.Close()

	_, err = producer.Enqueue(
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
		asynqx.WithRedisAddr("127.0.0.1:6379"),
		asynqx.WithConcurrency(16),
		asynqx.WithQueues(map[string]int{
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
		asynqx.WithRedisAddr("127.0.0.1:6379"),
		asynqx.WithLocation("Asia/Shanghai"),
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
		asynqx.WithRedisAddr("127.0.0.1:6379"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer inspector.Close()
}
```

## 架构说明

### Producer

`Producer` 只负责任务投递。

公开方法：

- `NewProducer(opts ...ProducerOption) (*Producer, error)`
- `NewProducerFromConfig(cfg Config) (*Producer, error)`
- `(*Producer).Enqueue(ctx, taskType, payload, opts...)`
- `(*Producer).Close() error`
- `(*Producer).Shutdown(ctx) error`

语义：

- `Close` 幂等
- `Shutdown(ctx)` 允许调用方为关闭等待设置超时
- 关闭后再次投递返回 `ErrClosed`
- `Enqueue` 支持 `context.Context`

### Inspector

`Inspector` 负责创建底层 `asynq.Inspector`，用于运维检查队列、任务、重试和归档状态。

公开方法：

- `NewInspector(opts ...InspectorOption) (*Inspector, error)`
- `NewInspectorFromConfig(cfg Config) (*Inspector, error)`
- `(*Inspector).Close() error`

语义：

- `Inspector` 使用与 `Producer`、`Worker`、`Scheduler` 相同的 Redis 配置
- 调用方不再使用时应调用 `Close`
- 复用外部共享客户端（`WithRedisInstance`）时，`Close` 会返回 asynq 的 `redis connection is shared` 错误且不会关闭连接，可忽略该错误，由调用方自行关闭外部客户端

### Worker

`Worker` 只负责处理器注册和任务消费。

公开方法：

- `NewWorker(opts ...WorkerOption) (*Worker, error)`
- `NewWorkerFromConfig(cfg Config) (*Worker, error)`
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
- `NewSchedulerFromConfig(cfg Config) (*Scheduler, error)`
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

这些选项可同时用于 `NewProducer`、`NewWorker`、`NewScheduler`、`NewInspector`：

- `WithRedis(opt asynq.RedisConnOpt)`
- `WithRedisClient(opt asynq.RedisClientOpt)`
- `WithRedisInstance(client redis.UniversalClient)`
- `WithRedisFailover(opt asynq.RedisFailoverClientOpt)`
- `WithRedisCluster(opt asynq.RedisClusterClientOpt)`
- `WithRedisAddr(addr string)`
- `WithRedisUser(userName string)`
- `WithRedisPassword(password string)`
- `WithRedisDB(db int)`
- `WithRedisPoolSize(size int)`
- `WithDialTimeout(timeout time.Duration)`
- `WithReadTimeout(timeout time.Duration)`
- `WithWriteTimeout(timeout time.Duration)`
- `WithTLSConfig(cfg *tls.Config)`
- `WithLocation(name string)`
- `WithLogger(log Logger)`
- `WithPingOnStart(enabled bool)`
- `WithPingTimeout(timeout time.Duration)`

需要多个组件复用同一组配置时，可以先构造 `Config`，再传给 `NewProducerFromConfig`、`NewWorkerFromConfig`、`NewSchedulerFromConfig` 或 `NewInspectorFromConfig`。这些构造器会重新复制并校验配置，调用方后续修改原始变量不会影响已经创建的组件。

### 重试退避策略

asynq 默认的重试退避已是指数退避加抖动，但延迟随重试次数无上限增长（默认最多 25 次重试时末次延迟可达数天）。
若希望指数退避但**封顶**单次延迟，可用 `CappedExponentialBackoff` 并通过 `WithRetryDelayFunc` 注入：

```go
worker, err := asynqx.NewWorker(
	asynqx.WithRedisAddr("127.0.0.1:6379"),
	// 退避按 5s、10s、20s …… 增长，封顶 10 分钟，并在 [delay/2, delay] 内抖动
	asynqx.WithRetryDelayFunc(asynqx.CappedExponentialBackoff(5*time.Second, 10*time.Minute)),
)
```

仅 `Worker` 在重试时使用该函数（投递端不涉及）。等量抖动可避免大量任务在同一时刻集中重试冲击下游。

### Redis 部署形态

默认配置使用单机 Redis。生产环境可以直接传入 asynq 原生连接配置：

```go
worker, err := asynqx.NewWorker(
	asynqx.WithRedisFailover(asynq.RedisFailoverClientOpt{
		MasterName:    "primary",
		SentinelAddrs: []string{"10.0.0.1:26379", "10.0.0.2:26379", "10.0.0.3:26379"},
		Username:      "app",
		Password:      "secret",
		DB:            0,
	}),
)
```

```go
producer, err := asynqx.NewProducer(
	asynqx.WithRedisCluster(asynq.RedisClusterClientOpt{
		Addrs:    []string{"10.0.1.1:6379", "10.0.1.2:6379", "10.0.1.3:6379"},
		Username: "app",
		Password: "secret",
	}),
)
```

`WithRedisAddr`、`WithRedisUser`、`WithRedisPassword` 等便捷选项只适用于单机 Redis。已经使用 Sentinel 或 Cluster 配置后，不应再叠加这些单机字段选项。

默认情况下 Redis 连接保持 asynq/go-redis 的懒连接语义。需要启动时尽早暴露 Redis 不可达问题时，可以显式配置 `WithPingOnStart(true)`；该选项会在组件创建阶段执行一次 `PING`，失败时直接返回错误。`WithPingTimeout` 可限制这次探活的等待时间，传入 `0` 表示只使用 Redis 客户端自身的超时配置。

### 复用项目已有的 Redis 客户端

前面的连接参数选项（`WithRedisAddr` / `WithRedis` 等）会让每个组件各自新建连接池。如果项目里已经有一个用 `github.com/redis/go-redis/v9` 创建的客户端，希望 Producer / Worker / Scheduler / Inspector 与项目其它部分**共享同一个连接池**，用 `WithRedisInstance` 把该客户端直接传进来：

```go
rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 0})
defer rdb.Close() // 客户端由调用方关闭

cfg, err := asynqx.NewConfig(asynqx.WithRedisInstance(rdb))
if err != nil {
	// 处理错误
}

producer, _ := asynqx.NewProducerFromConfig(cfg)
worker, _ := asynqx.NewWorkerFromConfig(cfg)
```

注意事项：

- `WithRedisInstance` 优先级高于所有连接参数选项；传入后 `WithRedisAddr` / `WithRedis` 等会被忽略。
- 传入的客户端**生命周期由调用方负责**：组件的 `Shutdown` / `Close` 不会关闭它。请在所有 asynqx 组件关闭之后，再由调用方关闭该客户端，否则可能在组件仍在运行时切断连接。
- `client` 接受任意 `redis.UniversalClient`（单机 `*redis.Client`、`*redis.ClusterClient`、`*redis.FailoverClient` 等均可）。
- 复用外部客户端时，`Scheduler` 关闭会由 asynq 内部打印一条无害的 `redis connection is shared` 日志（asynq 的 `Scheduler.Shutdown` 始终尝试关闭连接），连接本身不会被关闭，可安全忽略；asynqx 自建连接的 `Scheduler` 不会有该日志。同理 `Inspector.Close` 会返回该错误。

### gtkit/logger 接入示例

`Logger` 直接兼容 asynq 的基础日志接口。asynqx 内部不会主动调用 `Fatal`，但底层 asynq 仍保留 `Fatal` 的退出语义；业务适配器应按自身日志库策略谨慎实现。

```go
type gtkitLoggerAdapter struct {
	log *logger.Logger
}

func (l gtkitLoggerAdapter) Debug(args ...any) { l.log.Debug(args...) }
func (l gtkitLoggerAdapter) Info(args ...any)  { l.log.Info(args...) }
func (l gtkitLoggerAdapter) Warn(args ...any)  { l.log.Warn(args...) }
func (l gtkitLoggerAdapter) Error(args ...any) { l.log.Error(args...) }
func (l gtkitLoggerAdapter) Fatal(args ...any) { l.log.Fatal(args...) }
```

### Worker 相关配置

- `WithConcurrency(concurrency int)`
- `WithQueues(queues map[string]int)`
- `WithRetryDelayFunc(fn asynq.RetryDelayFunc)`
- `WithStrictPriority(val bool)`
- `WithErrorHandler(fn asynq.ErrorHandler)`
- `WithHealthCheckFunc(fn func(error))`
- `WithHealthCheckInterval(interval time.Duration)`
- `WithShutdownTimeout(timeout time.Duration)`
- `WithDelayedTaskCheckInterval(interval time.Duration)`
- `WithGroupGracePeriod(interval time.Duration)`
- `WithGroupMaxDelay(interval time.Duration)`
- `WithGroupMaxSize(size int)`
- `WithGroupAggregator(aggregator asynq.GroupAggregator)`
- `WithMiddleware(middlewares ...asynq.MiddlewareFunc)`
- `WithSchedulerPostEnqueueFunc(fn func(info *asynq.TaskInfo, err error))`（仅 Scheduler；`err != nil` 表示该次周期任务投递失败）
- `WithIsFailure(fn func(error) bool)`
- `WithDefaultTaskTimeout(timeout time.Duration)`
- `WithPingOnStart(enabled bool)`
- `WithPingTimeout(timeout time.Duration)`

`WithDefaultTaskTimeout(0)` 表示不注入默认任务超时；此时只有显式传入 `WithTaskTimeout` 的任务才会携带 timeout 选项。任务已经显式配置 `WithTaskTimeout` 或 `WithTaskDeadline` 时，不会再注入默认任务超时。

### 任务聚合

使用任务聚合时，投递端用 `WithTaskGroup` 把任务放进分组，Worker 端**必须**配置 `WithGroupAggregator`：未配置聚合器时 asynq 不会启动聚合协程，分组任务只会滞留在 group 中，不会被聚合处理。`WithGroupGracePeriod` / `WithGroupMaxDelay` / `WithGroupMaxSize` 用于控制聚合触发时机。

```go
worker, err := asynqx.NewWorker(
	asynqx.WithRedisAddr("127.0.0.1:6379"),
	asynqx.WithGroupGracePeriod(2*time.Second),
	asynqx.WithGroupMaxDelay(30*time.Second),
	asynqx.WithGroupMaxSize(20),
	asynqx.WithGroupAggregator(asynq.GroupAggregatorFunc(
		func(group string, tasks []*asynq.Task) *asynq.Task {
			// 将同组多个任务合并为一个任务
			return asynq.NewTask("notification:batch", aggregatePayload(tasks))
		},
	)),
)

// 投递端把任务放进分组
_, err = producer.Enqueue(ctx, "notification:push", payload, asynqx.WithTaskGroup("user-42"))
```

## 任务选项

任务级配置用于 `Producer.Enqueue` 和 `Scheduler.Register`。

- `WithTaskQueue(queue string)`
- `WithTaskGroup(group string)`
- `WithTaskTimeout(timeout time.Duration)`
- `WithTaskDeadline(deadline time.Time)`
- `WithTaskDelay(delay time.Duration)`
- `WithTaskProcessAt(processAt time.Time)`
- `WithTaskMaxRetry(maxRetry int)`
- `WithTaskUnique(ttl time.Duration)`
- `WithTaskRetention(retention time.Duration)`
- `WithTaskID(taskID string)`
- `WithTaskRawOptions(opts ...asynq.Option)`

`WithTaskRawOptions` 是逃生口：当需要使用 asynqx 尚未镜像的原生 `asynq.Option` 时，直接透传即可，无需等待本包补充对应的 `WithTask*`。透传选项在镜像选项之后应用，与 asynq「后者覆盖前者」语义一致，因此也可用它覆盖镜像选项；其中的超时/截止选项会被默认超时注入逻辑识别。

```go
_, err := producer.Enqueue(
	ctx,
	"email:welcome",
	payload,
	asynqx.WithTaskQueue("critical"),
	asynqx.WithTaskRawOptions(asynq.Retention(24*time.Hour)),
)
```

`Producer.Enqueue` 和 `Scheduler.Register` 会将 payload 序列化为 JSON 后写入 asynq 任务；传入 `[]byte` 也会按 JSON 规则编码，而不是作为原始字节透传。

### 调度选项覆盖规则

- `WithTaskDelay` 和 `WithTaskProcessAt` 会互相覆盖
- 后应用的调度选项覆盖先前的调度选项

示例：

```go
_, err := producer.Enqueue(
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

### 类型安全的任务定义（推荐）

裸字符串 `taskType` + 各端自行约定 payload 类型时，编译期无法保证两端一致，是最容易出线上 bug 的点。
`TaskType[T]` 把「任务类型名」和「payload 类型」绑定在一起，投递端、调度端、消费端共享同一份定义，
类型写错或 payload 类型不匹配会直接编译不过：

```go
// 集中声明所有任务类型
var WelcomeEmail = asynqx.NewTaskType[EmailPayload]("email:welcome")

// 投递：payload 必须是 EmailPayload，否则编译失败
_, err := WelcomeEmail.Enqueue(ctx, producer, EmailPayload{UserID: "u-1001"},
	asynqx.WithTaskQueue("critical"))

// 消费：handler 直接拿到解码后的 EmailPayload
err = WelcomeEmail.Handle(worker, func(ctx context.Context, p EmailPayload) error {
	return nil
})

// 周期任务同理
_, err = WelcomeEmail.Register(ctx, scheduler, "@every 1m", EmailPayload{UserID: "u-1001"})
```

`NewTaskType` 不校验类型名，校验统一在 `Enqueue` / `Register` / `Handle` 时进行，因此可安全地用包级变量声明。

### 在处理器中读取任务元信息

`Handle[T]` 只把解码后的 payload 交给处理器，若还需要任务 ID、所属队列或重试状态，
可用以下便捷函数从 `context` 读取，无需直接依赖 `asynq` 包：

```go
err := asynqx.Handle(worker, "user:created", func(ctx context.Context, p UserCreatedPayload) error {
	meta := asynqx.MetadataFromContext(ctx) // 一次性取全部：ID / Queue / RetryCount / MaxRetry

	if asynqx.IsLastAttempt(ctx) {
		// 最后一次尝试，再失败将不再重试，可在此落库死信或告警
	}

	_ = asynqx.TaskID(ctx)
	_ = asynqx.QueueName(ctx)
	_ = asynqx.RetryCount(ctx)
	_ = asynqx.MaxRetry(ctx)
	_ = meta

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

- `Producer.Close` 幂等，且关闭后会拒绝新的投递
- `Producer.Shutdown(ctx)` 可为在途投递等待设置上界
- `Producer` 在关闭期间会等待已进入底层 client 的投递调用完成，再关闭底层连接
- `Worker` 和 `Scheduler` 使用显式状态机处理 `Start/Shutdown` 竞态
- 处理器注册只允许在 `Worker` 启动前完成
- 核心运行路径尽量避免显式锁；仅在底层依赖或测试框架需要时使用最小同步原语

## 停机与超时语义

这一节描述 `ctx`、`Run`、`Shutdown` 和 `WithShutdownTimeout` 的协作关系。

### 总体原则

- 调用方主动调用 `Shutdown(ctx)` 时，以调用方传入的 `ctx` 为准
- 调用方使用 `Run(ctx)` 时，`ctx` 负责“何时开始停机”
- `Run(ctx)` 进入停机阶段后，默认关闭等待预算来自 `WithShutdownTimeout`
- 默认 `ShutdownTimeout` 是 `30s`
- `WithShutdownTimeout(0)` 表示不额外设置默认关闭超时，内部会使用 `context.Background()`
- 如果 `Run(ctx)` 因 `ctx` 取消而触发停机且关闭成功，返回值是 `nil`；调用方需要区分退出原因时，应在外层读取传入的 `ctx.Err()`

### Producer

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
- `WithShutdownTimeout` 控制的是 `Run(ctx)` 触发后的默认优雅关闭预算，不覆盖显式 `Shutdown(ctx)` 传入的 `ctx`

### Scheduler

- `Start(ctx)` 当前只在进入启动前检查 `ctx`
- `Run(ctx)` 中的 `ctx` 只负责触发停机
- `Run(ctx)` 收到取消后，会使用 `ShutdownTimeout` 作为默认等待预算调用 `Shutdown`
- `Shutdown(ctx)` 会等待活跃操作结束并调用底层调度器关闭
- 如果 `Shutdown(ctx)` 先超时，会立即向调用方返回对应错误；后台停止流程仍可能继续直到自然完成

### 生产建议

- 服务主进程优先使用 `Run(signalCtx)`，并配置 `WithShutdownTimeout`
- `ShutdownTimeout` 应大于业务 handler 的正常耗时上界，否则停机时任务更容易被回推 Redis
- 如果你的服务框架已经有统一停机预算，直接显式调用 `Shutdown(ctx)`，不要依赖默认预算
- 对 `Producer` 而言，若停机时不希望无限等待在途投递，应优先调用 `Shutdown(ctx)` 而不是 `Close()`

## 生产环境建议

- 明确区分生产者进程、消费者进程、调度进程，不要把所有角色强塞进一个服务
- 为不同业务队列配置合理的 `Queues` 权重
- 配置任务超时、重试次数、唯一窗口，避免无界重试
- 按业务任务耗时配置 `WithShutdownTimeout`，让优雅停机预算和任务时长匹配
- 为 Redis 配置认证、TLS、连接池和超时，生产优先使用 Sentinel 或 Cluster
- 通过 `WithLogger` 接入统一日志实现
- 通过 `WithMiddleware` 接入 tracing、metrics 等中间件（panic 已由 asynq 自动 recover 并重试，无需自行包 recover 中间件）
- 通过 `WithErrorHandler(asynqx.NewLogErrorHandler(logger))` 记录终态失败，避免重试耗尽后的失败被静默吞掉
- 在服务主进程中使用 `Run(ctx)`，由外层信号管理优雅退出

> 关于失败处理：asynq 会自动 recover handler 中的 panic 并触发重试，因此不必再包一层 recover 中间件。
> 但当 handler 正常返回 `error` 且**重试次数耗尽**时，若未配置 `ErrorHandler`，这类终态失败不会有任何通知。
> `NewLogErrorHandler` 仅在最后一次尝试时以 Error 级别记录，避免每次重试都打日志造成噪音。

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

- `Producer`
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
