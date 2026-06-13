# Changelog

## v1.3.1 - 2026-06-14

### Changed

- 依赖升级：`github.com/gtkit/json` v0.2.11 → `github.com/gtkit/json/v2` v2.0.7，导入路径随之改为 `github.com/gtkit/json/v2`。v2 默认后端仍为 `encoding/json`，序列化行为保持一致；可通过 `sonic` / `go_json` / `jsoniter` 构建标签切换后端，本包已在四种后端下交叉验证通过。
- 依赖升级：`github.com/redis/go-redis/v9` v9.18.0 → v9.20.1，间接依赖 `github.com/rogpeppe/go-internal` → v1.15.0；go.mod 内其余依赖均为最新兼容版。配合 Go 工具链升级至 1.26.4，`govulncheck ./...` 报告无漏洞。
- `Producer.Enqueue` 在 `ctx` 已取消时提前返回 `ctx` 错误，与 `Scheduler.Register` 行为保持一致，避免无谓的 payload 序列化与在途计数占用。

### Fixed

- 修复复用外部共享客户端（`WithRedisInstance`）时 `Producer.Close` / `Producer.Shutdown` 返回 asynq "redis connection is shared" 错误的问题。外部客户端的生命周期由调用方负责，asynqx 不再对其调用 `Close`，关闭返回 `nil`。
- 消除 `Scheduler` 关闭时的误导性错误日志：asynqx 自建客户端时改用 `asynq.NewScheduler`（由 asynq 自行干净关闭其内部连接），不再触发 asynq 在 `Scheduler.Shutdown` 中记录的 "Failed to close redis client connection: redis connection is shared"。注：复用外部客户端时该日志由 asynq 内部产生，无法在本包层面消除。

### Added

- 新增 `InspectorOption`（`ConfigOption` 的别名），与 `ProducerOption` / `WorkerOption` / `SchedulerOption` 命名保持一致；`NewInspector` 的 GoDoc 补充了外部共享客户端下 `Close` 行为的说明。

## v1.3.0 - 2026-06-03

### Added

- 新增 `WithTaskRawOptions(opts ...asynq.Option)`：透传原生 `asynq.Option` 的逃生口，使投递端能使用 asynqx 尚未镜像的任务选项，无需等待本包补充对应的 `WithTask*`。透传选项在镜像选项之后应用（与 asynq「后者覆盖前者」语义一致），其中的超时/截止选项会被默认超时注入逻辑正确识别。
- 新增 `WithRedisInstance(client redis.UniversalClient)`（及 `Config.RedisClient` 字段）：允许 Producer / Worker / Scheduler / Inspector 复用调用方已创建的 go-redis 客户端，与项目其它部分共享同一个连接池，底层走 asynq 的 `NewClientFromRedisClient` / `NewServerFromRedisClient` / `NewSchedulerFromRedisClient` / `NewInspectorFromRedisClient`。该客户端优先于连接参数选项，且生命周期由调用方负责——asynqx 的 `Shutdown` / `Close` 不会关闭外部传入的客户端。
- 新增类型安全的任务定义 `TaskType[T]` 与 `NewTaskType[T](name)`：把任务类型名与 payload 类型 `T` 绑定，投递端、调度端、消费端共享同一份定义，编译期保证类型名与 payload 类型一致。提供 `.Enqueue`、`.Register`、`.Handle`、`.Name` 方法，薄委托到 Producer / Scheduler / Worker。
- 新增处理器内读取任务运行期元信息的便捷函数：`MetadataFromContext`（一次性取 ID / Queue / RetryCount / MaxRetry）、`TaskID`、`QueueName`、`RetryCount`、`MaxRetry`、`IsLastAttempt`，无需在处理器中直接依赖 `asynq` 包。
- 新增 `NewLogErrorHandler(logger Logger)`：基于 `Logger` 的 `asynq.ErrorHandler`，仅在重试耗尽的终态失败时以 Error 级别记录，避免任务最终失败被静默吞掉；通过 `WithErrorHandler` 注入。
- 新增 `CappedExponentialBackoff(base, maxDelay)`：返回带上限和等量抖动的指数退避 `asynq.RetryDelayFunc`，弥补 asynq 默认退避随重试次数无上限增长的问题；通过 `WithRetryDelayFunc` 注入。

## v1.2.0 - 2026-06-01

### Added

- 新增 `WithGroupAggregator`（及 `Config.GroupAggregator` 字段），打通任务聚合：此前虽有 `WithTaskGroup` 与聚合调参，但缺少服务端聚合器入口，分组任务不会被聚合处理。现在配置聚合器后 asynq 才会启动聚合协程。
- 新增 `WithSchedulerPostEnqueueFunc`（及 `Config.SchedulerPostEnqueueFunc` 字段），用于观测调度器投递周期任务的结果；`err != nil` 表示投递失败，可据此告警。采用 asynq 的 `PostEnqueueFunc`（其 `EnqueueErrorHandler` 已弃用）。

## v1.1.1 - 2026-06-01

### Fixed

- 修复 `Worker.Start` / `Scheduler.Start` 失败路径的逻辑竞态：`runner.Start` 返回错误后无条件将状态写回 `idle`，可能覆盖并发 `Shutdown` 刚写入的 `stopping`，导致 `Shutdown` 永久阻塞且底层 Redis 连接泄漏。改为 `CompareAndSwap(starting, idle)`，与 ctx 取消路径保持一致。

## v1.1.0 - 2026-05-29

### Breaking Changes

- `Config.Redis` 从 `asynq.RedisClientOpt` 改为 `asynq.RedisConnOpt`，以支持单机、Sentinel 和 Cluster 三种 Redis 连接形态。
- `Logger` 收敛为 `asynq.Logger` 别名，不再要求额外实现 `Debugf`、`Infof`、`Warnf`、`Errorf`、`Fatalf`。
- ⚠ 将 `Broker` 重命名为 `Producer`：移除 `Broker`、`NewBroker`、`BrokerOption`，请改用 `Producer`、`NewProducer`、`ProducerOption`，未保留兼容别名。
- 移除 `WithXxxOption` 系列弃用兼容别名，请改用无 `Option` 后缀的选项名（如 `WithConcurrency`、`WithRedisAddr`）。

### Added

- 新增无 `Option` 后缀的共享配置选项（如 `WithConcurrency`、`WithRedisAddr`）。
- 新增 `WithPingOnStart`，用于在组件创建阶段可选执行 Redis `PING` 探活。
- 新增 `WithPingTimeout`，用于限制 Redis 启动探活等待时间。
- 新增 `NewProducerFromConfig`、`NewWorkerFromConfig`、`NewSchedulerFromConfig`、`NewInspectorFromConfig`，便于多个组件复用同一份 `Config`。
- 新增 `NewInspector`，直接返回底层 `asynq.Inspector`，用于队列状态检查。
- 新增 GoDoc 示例，覆盖 `NewProducer`、`Handle` 和 `Scheduler.Register`。
- README 增加 Redis Sentinel、Redis Cluster 和 gtkit/logger 适配器示例。

### Changed

- `WithDefaultTaskTimeout(0)` 现在表示不注入默认任务超时。

### Fixed

- 修复 Worker 未启动时直接 `Shutdown` 不释放底层 Redis 连接的问题。
- 修复 Scheduler 未启动时直接 `Shutdown` 的连接释放保障。
- 修复 `WithLocation("")` 静默使用 UTC 的问题，现在会返回配置错误。
- 修复 `.golangci.yml` 中 depguard 默认规则阻断核心依赖导入的问题。
- 升级 `golang.org/x/sys` 到 `v0.45.0`，覆盖 `GO-2026-5024` 的修复版本要求。
