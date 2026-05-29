package asynqx

import "github.com/hibiken/asynq"

// Logger 定义了库内使用的日志接口。
// 当前接口直接兼容 asynq.Logger；库内部不会主动调用 Fatal。
type Logger = asynq.Logger
