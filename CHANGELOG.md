# Changelog

## Unreleased

### Breaking Changes

- `Config.Redis` 从 `asynq.RedisClientOpt` 改为 `asynq.RedisConnOpt`，以支持单机、Sentinel 和 Cluster 三种 Redis 连接形态。
- `Logger` 接口补齐 `Warnf` 和 `Fatalf`，自定义日志适配器需要实现这两个格式化方法。

### Added

- 新增 `WithRedisOption`、`WithRedisClientOption`、`WithRedisFailoverOption`、`WithRedisClusterOption`。
- 新增 `NewInspector`，直接返回底层 `asynq.Inspector`，用于队列状态检查。
- 新增 GoDoc 示例，覆盖 `NewBroker`、`Handle` 和 `Scheduler.Register`。
- README 增加 Redis Sentinel、Redis Cluster 和 gtkit/logger 适配器示例。

### Fixed

- 修复 Worker 未启动时直接 `Shutdown` 不释放底层 Redis 连接的问题。
- 修复 Scheduler 未启动时直接 `Shutdown` 的连接释放保障。
- 修复 `WithLocationOption("")` 静默使用 UTC 的问题，现在会返回配置错误。
- 修复 `.golangci.yml` 中 depguard 默认规则阻断核心依赖导入的问题。
- 升级 `golang.org/x/sys` 到 `v0.45.0`，覆盖 `GO-2026-5024` 的修复版本要求。

