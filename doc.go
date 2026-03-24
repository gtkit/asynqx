// Package asynqx 提供基于 asynq 的任务队列封装。
//
// 这个包使用中文注释说明公开 API，并围绕 Broker、Worker、Scheduler、
// Config 和 TaskOption 构建新的生产级接口。
//
// 当前仓库默认只保留纯单元测试；需要真实 Redis 的集成验证应在独立环境中执行。
package asynqx
