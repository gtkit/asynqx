package asynqx

import (
	"context"
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
)

// NewLogErrorHandler 基于 Logger 构造一个 asynq.ErrorHandler，把任务处理的终态失败
// 记录下来，避免其被静默吞掉。
//
// asynq 自身会 recover handler 中的 panic 并触发重试，但当 handler 正常返回 error
// 且不再有下一次尝试时，若未配置 ErrorHandler，这类终态失败不会有任何通知。
// 终态失败包括两类：重试耗尽（最后一次尝试仍失败），以及 handler 返回包裹
// asynq.SkipRetry 的错误（asynq 会跳过重试直接归档，Handle 的 payload 解码失败即属此类）。
// 该处理器仅在终态失败时以 Error 级别记录，重试路径上的失败不打日志，避免噪音。
//
// 通过 WithErrorHandler 注入：
//
//	asynqx.WithErrorHandler(asynqx.NewLogErrorHandler(logger))
func NewLogErrorHandler(logger Logger) asynq.ErrorHandler {
	return asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
		if isNilInterface(logger) {
			return
		}

		skipRetry := errors.Is(err, asynq.SkipRetry)
		if !skipRetry && !IsLastAttempt(ctx) {
			return
		}

		reason := "retries exhausted"
		if skipRetry {
			reason = "skip retry"
		}

		meta := MetadataFromContext(ctx)
		logger.Error(fmt.Sprintf(
			"asynqx: task failed permanently (%s): type=%s id=%s queue=%s retry=%d/%d: %v",
			reason, task.Type(), meta.ID, meta.Queue, meta.RetryCount, meta.MaxRetry, err,
		))
	})
}
