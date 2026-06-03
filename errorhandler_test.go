package asynqx

import (
	"context"
	"errors"
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
