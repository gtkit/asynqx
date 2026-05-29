# Changelog

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
