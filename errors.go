package asynqx

import (
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
)

// ErrInvalidTaskOption 表示任务选项不合法。
var ErrInvalidTaskOption = errors.New("asynqx: invalid task option")

// ErrInvalidArgument 表示公开方法收到的普通参数不合法。
var ErrInvalidArgument = errors.New("asynqx: invalid argument")

// ErrClosed 表示组件（Producer 或 App）已关闭，不能再继续使用。
var ErrClosed = errors.New("asynqx: closed")

// ErrInvalidConfiguration 表示传入的基础配置不合法。
var ErrInvalidConfiguration = errors.New("asynqx: invalid configuration")

// ErrInvalidConfig 是 ErrInvalidConfiguration 的兼容别名。
var ErrInvalidConfig = ErrInvalidConfiguration

// ErrWorkerAlreadyRunning 表示 Worker 已启动或正在启动，不能重复启动或继续注册处理器。
var ErrWorkerAlreadyRunning = errors.New("asynqx: worker already running")

// ErrWorkerStopped 表示 Worker 已停止，或在启动完成前已收到停止请求。
var ErrWorkerStopped = errors.New("asynqx: worker stopped")

// ErrSchedulerAlreadyRunning 表示 Scheduler 已启动或正在启动，不能重复启动。
var ErrSchedulerAlreadyRunning = errors.New("asynqx: scheduler already running")

// ErrSchedulerStopped 表示 Scheduler 已停止，或在启动完成前已收到停止请求。
var ErrSchedulerStopped = errors.New("asynqx: scheduler stopped")

// ErrHandlerAlreadyRegistered 表示同一个 taskType 已经注册过处理器。
var ErrHandlerAlreadyRegistered = errors.New("asynqx: handler already registered")

// SkipRetry 与 asynq.SkipRetry 是同一个哨兵错误：handler 返回包裹它的错误时，
// 任务跳过剩余重试直接归档（终态失败，会被 NewLogErrorHandler 记录）。
// 重导出后业务 handler 无需直接依赖 asynq 包：
//
//	return fmt.Errorf("invalid payload: %w", asynqx.SkipRetry)
//
//nolint:errname // 必须与 asynq.SkipRetry 同名镜像，方便两边对照
var SkipRetry = asynq.SkipRetry

// ErrDuplicateTask 与 asynq.ErrDuplicateTask 是同一个哨兵错误：配合 WithTaskUnique
// 投递时，唯一性窗口内的重复任务会使 Enqueue 返回包裹它的错误，
// 用 errors.Is(err, asynqx.ErrDuplicateTask) 识别去重命中。
var ErrDuplicateTask = asynq.ErrDuplicateTask

// ErrTaskIDConflict 与 asynq.ErrTaskIDConflict 是同一个哨兵错误：配合 WithTaskID
// 投递时，任务 ID 与既有任务冲突会使 Enqueue 返回包裹它的错误。
var ErrTaskIDConflict = asynq.ErrTaskIDConflict

func invalidConfigurationError(field, reason string) error {
	if reason == "" {
		return fmt.Errorf("%w: %s", ErrInvalidConfiguration, field)
	}

	return fmt.Errorf("%w: %s: %s", ErrInvalidConfiguration, field, reason)
}

func invalidTaskOptionError(field, reason string) error {
	if reason == "" {
		return fmt.Errorf("%w: %s", ErrInvalidTaskOption, field)
	}

	return fmt.Errorf("%w: %s: %s", ErrInvalidTaskOption, field, reason)
}

func invalidArgumentError(field, reason string) error {
	if reason == "" {
		return fmt.Errorf("%w: %s", ErrInvalidArgument, field)
	}

	return fmt.Errorf("%w: %s: %s", ErrInvalidArgument, field, reason)
}
