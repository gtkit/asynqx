package asynqx

import (
	"errors"
	"fmt"
)

// ErrInvalidTaskOption 表示任务选项不合法。
var ErrInvalidTaskOption = errors.New("asynqx: invalid task option")

// ErrInvalidArgument 表示公开方法收到的普通参数不合法。
var ErrInvalidArgument = errors.New("asynqx: invalid argument")

// ErrClosed 表示 Producer 已关闭，不能再投递任务。
var ErrClosed = errors.New("asynqx: producer closed")

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
