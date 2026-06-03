package asynqx

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"
)

// NewLogErrorHandler 基于 Logger 构造一个 asynq.ErrorHandler，把任务处理失败记录下来，
// 避免重试耗尽后的终态失败被静默吞掉。
//
// asynq 自身会 recover handler 中的 panic 并触发重试，但当 handler 正常返回 error
// 且重试次数耗尽时，若未配置 ErrorHandler，这类终态失败不会有任何通知。
// 该处理器仅在最后一次尝试（重试耗尽）时以 Error 级别记录，避免每次重试都打日志造成噪音。
//
// 通过 WithErrorHandler 注入：
//
//	asynqx.WithErrorHandler(asynqx.NewLogErrorHandler(logger))
func NewLogErrorHandler(logger Logger) asynq.ErrorHandler {
	return asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
		if isNilInterface(logger) || !IsLastAttempt(ctx) {
			return
		}

		meta := MetadataFromContext(ctx)
		logger.Error(fmt.Sprintf(
			"asynqx: task failed permanently (retries exhausted): type=%s id=%s queue=%s retry=%d/%d: %v",
			task.Type(), meta.ID, meta.Queue, meta.RetryCount, meta.MaxRetry, err,
		))
	})
}
