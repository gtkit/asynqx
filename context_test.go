package asynqx

import (
	"context"
	"testing"
)

// 任务流程内的分支由 asynq 在处理器中注入 context 元信息，依赖其 internal 包，
// 无法在纯单测中构造，相关取值是对 asynq.GetX 的薄委托。这里覆盖非任务流程的零值路径。

func TestMetadataFromContextReturnsZeroOutsideTaskFlow(t *testing.T) {
	meta := MetadataFromContext(context.Background())

	if meta != (TaskMetadata{}) {
		t.Fatalf("expected zero metadata outside task flow, got %+v", meta)
	}
}

func TestContextGettersReturnZeroOutsideTaskFlow(t *testing.T) {
	ctx := context.Background()

	if id := TaskID(ctx); id != "" {
		t.Fatalf("expected empty task id, got %q", id)
	}

	if queue := QueueName(ctx); queue != "" {
		t.Fatalf("expected empty queue name, got %q", queue)
	}

	if n := RetryCount(ctx); n != 0 {
		t.Fatalf("expected retry count 0, got %d", n)
	}

	if n := MaxRetry(ctx); n != 0 {
		t.Fatalf("expected max retry 0, got %d", n)
	}
}

func TestIsLastAttemptFalseOutsideTaskFlow(t *testing.T) {
	if IsLastAttempt(context.Background()) {
		t.Fatal("expected IsLastAttempt to be false outside task flow")
	}
}
