package asynqx

import (
	"context"

	"github.com/hibiken/asynq"
)

// TaskMetadata 聚合了任务在处理时可从 context 读取的运行期元信息，
// 便于处理器在不直接依赖 asynq 包的情况下获取任务的身份与重试状态。
type TaskMetadata struct {
	ID         string
	Queue      string
	RetryCount int
	MaxRetry   int
}

// MetadataFromContext 从处理器 context 中提取任务运行期元信息。
// 当 ctx 不是由 asynq 处理流程派生时，返回各字段的零值。
func MetadataFromContext(ctx context.Context) TaskMetadata {
	taskID, _ := asynq.GetTaskID(ctx)
	queue, _ := asynq.GetQueueName(ctx)
	retryCount, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)

	return TaskMetadata{
		ID:         taskID,
		Queue:      queue,
		RetryCount: retryCount,
		MaxRetry:   maxRetry,
	}
}

// TaskID 返回当前任务的 ID；不在任务处理流程中时返回空字符串。
func TaskID(ctx context.Context) string {
	id, _ := asynq.GetTaskID(ctx)

	return id
}

// QueueName 返回当前任务所属队列名；不在任务处理流程中时返回空字符串。
func QueueName(ctx context.Context) string {
	queue, _ := asynq.GetQueueName(ctx)

	return queue
}

// RetryCount 返回当前任务已经重试的次数；不在任务处理流程中时返回 0。
func RetryCount(ctx context.Context) int {
	retryCount, _ := asynq.GetRetryCount(ctx)

	return retryCount
}

// MaxRetry 返回当前任务允许的最大重试次数；不在任务处理流程中时返回 0。
func MaxRetry(ctx context.Context) int {
	maxRetry, _ := asynq.GetMaxRetry(ctx)

	return maxRetry
}

// IsLastAttempt 判断当前是否为任务的最后一次尝试，即再次返回错误后将不再重试。
// 当 ctx 不是由 asynq 处理流程派生时返回 false。
func IsLastAttempt(ctx context.Context) bool {
	retryCount, hasRetryCount := asynq.GetRetryCount(ctx)
	maxRetry, hasMaxRetry := asynq.GetMaxRetry(ctx)

	return hasRetryCount && hasMaxRetry && retryCount >= maxRetry
}
