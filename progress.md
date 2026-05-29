# asynqx 修复进度

## 2026-05-29

- 复现 `go test ./...` 编译失败。
- 核实 RedisConnOpt、depguard、GO-2026-5024、Shutdown、Inspector、Logger、README、example、CHANGELOG 等问题。
- 创建任务记录文件，准备进入测试先行修复。

- 写入配置、生命周期、Inspector、公开入口工厂的失败测试；`go test ./...` 预期失败，包含原有 `:=` 编译错误和新增未实现 API。
- 完成第一轮实现：`Config.Redis` 改为 `asynq.RedisConnOpt`，新增 Sentinel/Cluster option，修复 Worker/Scheduler idle shutdown，补 Inspector 和公开入口测试工厂。
- `go test ./...` 已进入运行阶段并通过一轮。
- 完成文档与配置收敛：README、CHANGELOG、example_test、depguard、.gitignore、x/sys 升级。
- 验证通过：go test ./...、go test ./... -race、golangci-lint run ./...、govulncheck ./...、GOOS=windows govulncheck ./...、Windows/Linux 测试二进制交叉编译。
- 覆盖率：go test ./... -coverprofile=/tmp/asynqx.cover 后 total 69.1%。默认测试仍不连接真实 Redis，未强行为覆盖率引入外部依赖。
