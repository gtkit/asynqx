# asynqx 生产可用性修复计划

## 目标

修复当前阻塞编译、Redis 高可用配置、关闭泄漏、lint/vuln、文档与示例覆盖问题，并用测试、竞态检测、lint、漏洞扫描和跨平台构建验证稳定性。

## 阶段

- [x] 阶段 1：恢复可编译并记录根因
- [x] 阶段 2：补失败测试覆盖生产问题
- [x] 阶段 3：实现 RedisConnOpt、Inspector、Shutdown、配置校验与日志接口修复
- [x] 阶段 4：补 README、GoDoc 示例、CHANGELOG、lint 配置和依赖升级
- [x] 阶段 5：执行 go test、race、coverage、golangci-lint、govulncheck 和跨平台验证
- [x] 阶段 6：复查 diff，整理剩余风险

## 决策

- `Config.Redis` 使用 `asynq.RedisConnOpt` 保留上游三种 Redis 拓扑能力。
- 单机便捷 option 只在当前 Redis 配置为 `RedisClientOpt` 时生效；Sentinel/Cluster 使用专门 option 设置完整连接配置。
- Worker/Scheduler 的未启动关闭路径都必须调用 runner 级别关闭，并补测试证明。
- `Run(ctx)` 默认保留返回 shutdown error 的兼容行为，但在 GoDoc/README 明确说明 ctx 取消语义。

