# asynqx 修复发现记录

## 已确认事实

- 当前 `go test ./...` 在进入测试前失败，根因是多处 `err :=` 重复声明。
- asynq v0.26.0 的 `NewClient`、`NewServer`、`NewScheduler`、`NewInspector` 都接收 `asynq.RedisConnOpt`。
- `GO-2026-5024` 影响 `golang.org/x/sys/windows`，修复版本是 `v0.44.0`；当前依赖为 `v0.42.0`。
- `.golangci.yml` 的 `default: all` 会启用 depguard，默认规则会拦截 `github.com/hibiken/asynq` 和 `github.com/gtkit/json`。
- 当前没有 `CHANGELOG.md`、`example_test.go` 和 gtkit/logger 接入示例。

## 风险点

- 修改 `Config.Redis` 为接口类型会改变公开字段类型，是合理但破坏性的生产修复。
- asynq 原生 `Shutdown` 对未启动的 Server/Scheduler 是 no-op，wrapper 必须保证自己的 runner close 行为可测。

