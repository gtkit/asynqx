package asynqx

import "github.com/hibiken/asynq"

// Logger 定义了库内使用的日志接口。
// 该接口在 asynq.Logger 的基础上补充了格式化输出能力，便于业务统一接入结构化日志。
type Logger interface {
	asynq.Logger
	Debugf(template string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(template string, args ...any)
	Fatalf(format string, args ...any)
}
