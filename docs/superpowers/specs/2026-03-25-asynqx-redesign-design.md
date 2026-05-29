# asynqx 重构设计

## 背景

当前项目基于 `github.com/hibiken/asynq` 做了一层轻量封装，但现有实现存在以下问题：

- 单个 `Server` 同时承担生产者、消费者、调度器、检查器和运行时状态容器职责，边界不清晰。
- 公开 API 以动态类型为主，`MsgPayload`、`Binder` 等接口不符合现代 Go 的泛型风格，可读性和可维护性较弱。
- 生命周期管理不稳健，`Start/Stop` 与底层阻塞行为耦合，存在重复启动、重复关闭、竞态访问等风险。
- `waitResult` 采用无休眠递归轮询，不支持 `context`，存在性能和稳定性风险。
- 运行时使用共享可变状态和锁保护 map，整体并发模型较粗糙，不利于高并发生产环境。
- README 与测试均偏示例性质，缺少生产级项目所需的配置说明、错误边界、验证策略和最佳实践。

本次重构目标是做成一个更符合 Go 风格、公开 API 更清晰、尽量无锁且可稳定用于生产的任务队列封装库。

## 目标

- 基于 Go 1.26，代码风格遵循现代 Go 习惯。
- 对外 API 易理解、易调用，职责明确。
- 运行期默认并发安全，核心路径尽量避免显式锁。
- 支持生产级项目对可用性、稳定性、配置校验和优雅停机的要求。
- 提供完整中文注释和详细 README。
- 保留 asynq 的核心能力，包括即时任务、延迟任务、周期任务、重试、去重、优先级和中间件扩展。

## 非目标

- 不在库内部实现业务结果存储系统。
- 不实现跨进程协调的分布式注册中心。
- 不屏蔽 `asynq` 的所有原生概念，必要时仍允许通过选项透传常用能力。

## 总体架构

将当前单体 `Server` 拆分为三个显式角色：

### Producer

负责生产任务，仅暴露任务投递相关能力。

职责：

- 创建和持有 `asynq.Client`
- 负责任务序列化
- 负责单次任务选项拼装
- 负责资源关闭

不负责：

- 消费任务
- 周期任务调度
- 检查运行中的 worker 状态

### Worker

负责消费任务和运行处理器。

职责：

- 创建和持有 `asynq.Server`
- 注册任务处理函数
- 维护中间件链
- 启动、优雅关闭和错误传播

不负责：

- 主动投递任务
- 维护周期任务注册表

### Scheduler

负责注册和管理周期任务。

职责：

- 创建和持有 `asynq.Scheduler`
- 注册周期任务
- 取消周期任务
- 提供必要的调度生命周期管理

不负责：

- 执行任务消费逻辑
- 充当业务级任务结果中心

## 对外 API 设计

### 构造函数

```go
func NewProducer(opts ...ProducerOption) (*Producer, error)
func NewWorker(opts ...WorkerOption) (*Worker, error)
func NewScheduler(opts ...SchedulerOption) (*Scheduler, error)
```

所有实例均采用 option 选项函数构造。构造阶段完成配置复制、默认值填充和校验；构造完成后配置不可变。

### 任务消费注册

优先使用泛型注册 API：

```go
func Handle[T any](worker *Worker, taskType string, handler func(context.Context, T) error) error
```

设计理由：

- 调用方不再需要显式提供 `Binder`
- payload 类型由泛型参数直接表达
- 注册逻辑更符合 Go 1.18+ 的习惯

同时保留底层原始注册能力，供高级场景使用：

```go
func (w *Worker) HandleRaw(taskType string, handler Handler) error
```

### 任务投递

```go
func (b *Producer) Enqueue(ctx context.Context, taskType string, payload any, opts ...TaskOption) (*TaskInfo, error)
```

支持：

- 立即投递
- 延迟执行
- 指定执行时间
- 超时
- 截止时间
- 最大重试次数
- 唯一任务
- 队列优先级
- 保留时长

### 周期任务

```go
func (s *Scheduler) Register(ctx context.Context, spec string, taskType string, payload any, opts ...TaskOption) (string, error)
func (s *Scheduler) Unregister(ctx context.Context, entryID string) error
func (s *Scheduler) Run(ctx context.Context) error
func (s *Scheduler) Start(ctx context.Context) error
func (s *Scheduler) Shutdown(ctx context.Context) error
```

关键调整：

- 周期任务删除基于 `entryID`，不依赖进程内 map。
- 若需要按业务名管理映射，交由业务层自行维护。
- 避免因进程重启导致无法取消历史注册任务。

### 生命周期

```go
func (w *Worker) Run(ctx context.Context) error
func (w *Worker) Start(ctx context.Context) error
func (w *Worker) Shutdown(ctx context.Context) error

func (b *Producer) Close() error
func (b *Producer) Shutdown(ctx context.Context) error
```

约束：

- `Run` 为阻塞方法，适合主服务进程。
- `Start` 为非阻塞方法，适合集成到已有服务框架。
- `Shutdown` 支持幂等调用。
- 所有阻塞等待必须接受 `context.Context`。
- `Worker.Run` 在收到外部取消后，会使用 `ShutdownTimeout` 作为默认优雅关闭预算。
- `Scheduler.Run` 在收到外部取消后，也会使用 `ShutdownTimeout` 作为默认关闭预算。

停机语义：

- 显式调用 `Shutdown(ctx)` 时，始终以调用方传入的 `ctx` 作为等待上界。
- `Run(ctx)` 中的 `ctx` 只负责“开始停机”的触发，不直接复用为关闭等待预算。
- `Run(ctx)` 进入关闭阶段后，默认等待预算来自 `ShutdownTimeout`。
- `Producer.Close()` 等价于无超时版本的关闭；若调用方需要控制等待上界，应使用 `Producer.Shutdown(ctx)`。
- `Worker.Shutdown(ctx)` 和 `Scheduler.Shutdown(ctx)` 若先于底层停止完成而超时，应向调用方返回 `ctx.Err()`。
- `Scheduler.Shutdown(ctx)` 即使向调用方超时返回，后台停止流程仍可能继续直到活跃操作结束。

## 配置模型

### 选项分层

按作用域拆分 option，避免混用：

- `ProducerOption`
- `WorkerOption`
- `SchedulerOption`
- `TaskOption`

### 公共连接配置

通过各实例 option 函数设置 Redis 连接信息，例如：

- `WithRedisAddr`
- `WithRedisUsername`
- `WithRedisPassword`
- `WithRedisDB`
- `WithRedisPoolSize`
- `WithRedisTLSConfig`
- `WithRedisDialTimeout`
- `WithRedisReadTimeout`
- `WithRedisWriteTimeout`

### Worker 配置

- `WithConcurrency`
- `WithQueues`
- `WithStrictPriority`
- `WithRetryDelayFunc`
- `WithErrorHandler`
- `WithHealthCheckFunc`
- `WithHealthCheckInterval`
- `WithShutdownTimeout`
- `WithDelayedTaskCheckInterval`
- `WithGroupGracePeriod`
- `WithGroupMaxDelay`
- `WithGroupMaxSize`
- `WithMiddleware`
- `WithLogger`

### Scheduler 配置

- `WithLocation`
- `WithLogger`

### Task 配置

- `WithTaskQueue`
- `WithTaskGroup`
- `WithTaskTimeout`
- `WithTaskDeadline`
- `WithTaskDelay`
- `WithTaskProcessAt`
- `WithTaskMaxRetry`
- `WithTaskUnique`
- `WithTaskRetention`
- `WithTaskTaskID`

说明：

- option 仅在构造期或单次调用期生效，不允许用于运行时热修改实例。
- 所有 option 在进入实例或任务之前统一校验。

## 并发模型

设计原则是“默认不可变、最少共享、能用原子就不用锁、能通过 API 消除共享状态就不引入状态”。

### 具体策略

- 配置对象在构造阶段深拷贝，实例创建后不再暴露可变配置。
- `Producer`、`Worker`、`Scheduler` 不维护业务态共享 map。
- 启停状态通过 `atomic.Bool` 与 `sync.Once` 管理，避免对核心路径加互斥锁。
- 处理器注册只允许在启动前完成，运行中不支持动态修改，避免读写竞争。
- 核心任务处理链依赖 `asynq` 原生并发能力，不在应用层增加多余 goroutine 包装。

### 不再采用的设计

- 不再使用 `started bool + RWMutex` 管理实例状态。
- 不再使用 `entryIDs map[string]string` 维护周期任务 ID。
- 不再为每次 handler 执行额外启动 goroutine 再用 channel 等待结果；直接由 handler 在当前工作协程内执行。

原因：

- 额外 goroutine 无法提升吞吐，反而增加调度和超时控制复杂度。
- 对于已由 asynq 负责调度的 worker，handler 内再包一层 goroutine 没有收益。
- 去掉额外包装后，`context` 取消和 panic 恢复语义更直接。

## 错误处理

### 原则

- 构造期尽早失败。
- 公开方法返回包装后的错误，保留上下文信息。
- 避免静默吞错。

### 约束

- 配置非法值直接返回错误。
- payload 编码失败直接返回错误。
- 启动前重复注册同名任务时返回错误。
- 启动后尝试注册处理器返回错误。
- 重复启动和重复关闭为幂等，不返回伪错误。

## 安全性

生产级安全的重点不只是“线程安全”，还包括运行边界清晰和失败行为可控：

- 所有公开阻塞调用都支持 `context` 取消。
- 避免无界递归与无休眠轮询。
- 默认不将“等待任务完成”作为核心主路径能力。
- 若保留任务状态等待能力，则实现为显式可选 API，必须带超时和轮询间隔。
- 默认支持 TLS 连接 Redis。
- 日志和错误输出不主动打印敏感连接信息。

## 日志与可观测性

日志策略：

- 不再使用包级全局 logger。
- logger 作为实例级依赖注入。
- 默认提供兼容 `asynq.Logger` 的轻量标准库 logger 适配。

可观测性扩展：

- 暴露中间件注册能力，便于业务接入 tracing、metrics、recover、审计。
- README 中明确推荐接入 Prometheus、OpenTelemetry 和结构化日志的方式。

## 兼容性策略

本次为重构优先方案，不以兼容旧 API 为第一目标。

计划：

- 删除 `Server`
- 删除 `RegisterSubscriber`、`RegisterSubscriberWithCtx`
- 删除 `Binder`、`MsgPayload` 等动态类型入口
- 提供清晰迁移路径和 README 示例

## 测试策略

采用三层验证：

### 单元测试

覆盖：

- option 默认值与校验
- payload 编解码
- handler 注册
- 生命周期边界
- 错误返回

### 并发测试

覆盖：

- 并发投递
- 重复启动
- 重复关闭
- 启动后注册处理器
- 原子状态切换

必须运行：

```shell
go test ./... -race
```

### 集成测试

覆盖：

- Redis 真正投递和消费
- 延迟任务
- 周期任务
- 去重任务
- 重试任务
- 优雅停机

集成测试通过环境变量启用，避免默认测试依赖外部 Redis。

## README 规划

README 需要覆盖以下内容：

- 项目定位
- 功能列表
- 架构说明
- 安装方式
- 快速开始
- Producer 用法
- Worker 用法
- Scheduler 用法
- Option 配置说明
- TaskOption 说明
- 生产环境建议
- 并发安全说明
- 错误处理说明
- 测试方式
- 常见问题

## 实现拆分建议

建议按以下模块重构：

- `producer.go`
- `worker.go`
- `scheduler.go`
- `task.go`
- `options.go`
- `config.go`
- `logger.go`
- `errors.go`
- `doc.go`

## 风险与取舍

### 风险

- 破坏旧 API，需要迁移调用方。
- `asynq` 原生行为决定了部分能力边界，例如无法天然提供强一致业务结果返回。

### 取舍

- 优先清晰边界和生产可维护性，而不是保留历史便捷接口。
- 优先少状态和少锁，而不是为了“看起来方便”引入进程内注册表。
- 优先显式 API 和明确错误，而不是自动兜底式魔法行为。

## 验收标准

满足以下条件才认为重构完成：

- 所有公开导出类型和方法具有完整中文注释。
- README 可独立指导使用者完成生产者、消费者和调度器接入。
- 默认单元测试通过。
- 并发测试和 `-race` 验证通过。
- 集成测试在提供 Redis 环境时可通过。
- 核心路径不存在无界递归、无界轮询和不受控共享状态。
