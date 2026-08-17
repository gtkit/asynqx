package asynqx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
)

var errHandlerTestFailure = errors.New("task processing failed")

type recordingLogger struct {
	errorCalls int
	lastError  string
}

func (l *recordingLogger) Debug(...any) {}
func (l *recordingLogger) Info(...any)  {}
func (l *recordingLogger) Warn(...any)  {}
func (l *recordingLogger) Fatal(...any) {}

func (l *recordingLogger) Error(args ...any) {
	l.errorCalls++

	if len(args) > 0 {
		if msg, ok := args[0].(string); ok {
			l.lastError = msg
		}
	}
}

func TestNewLogErrorHandlerReturnsHandler(t *testing.T) {
	if NewLogErrorHandler(&recordingLogger{}) == nil {
		t.Fatal("expected non-nil error handler")
	}
}

func TestNewLogErrorHandlerNilLoggerDoesNotPanic(_ *testing.T) {
	handler := NewLogErrorHandler(nil)

	task := asynq.NewTask("email:welcome", nil)
	handler.HandleError(context.Background(), task, errHandlerTestFailure)
}

func TestNewLogErrorHandlerSkipsNonTerminalFailure(t *testing.T) {
	logger := &recordingLogger{}
	handler := NewLogErrorHandler(logger)

	// 非任务流程的 context 中 IsLastAttempt 为 false，处理器不应记录日志。
	task := asynq.NewTask("email:welcome", nil)
	handler.HandleError(context.Background(), task, errHandlerTestFailure)

	if logger.errorCalls != 0 {
		t.Fatalf("expected no error log for non-terminal failure, got %d", logger.errorCalls)
	}
}

func TestNewLogErrorHandlerLogsSkipRetryFailure(t *testing.T) {
	logger := &recordingLogger{}
	handler := NewLogErrorHandler(logger)

	// SkipRetry 是终态失败：即使不是最后一次尝试（此处 ctx 无重试信息），也必须记录。
	task := asynq.NewTask("email:welcome", nil)
	err := fmt.Errorf("unmarshal task payload: %w: %w", errHandlerTestFailure, asynq.SkipRetry)
	handler.HandleError(context.Background(), task, err)

	if logger.errorCalls != 1 {
		t.Fatalf("expected 1 error log for skip-retry failure, got %d", logger.errorCalls)
	}

	if !strings.Contains(logger.lastError, "skip retry") {
		t.Fatalf("expected log message to mention skip retry, got %q", logger.lastError)
	}
}
