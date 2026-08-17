package asynqx

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hibiken/asynq"
)

// 重导出哨兵必须与 asynq 原值为同一错误：errors.Is 双向成立，
// 且经 asynqx 哨兵包裹的错误能被 asynq 内部的判定链命中（反之亦然）。
func TestSentinelReexportsMatchAsynq(t *testing.T) {
	pairs := []struct {
		name   string
		local  error
		remote error
	}{
		{"SkipRetry", SkipRetry, asynq.SkipRetry},
		{"ErrDuplicateTask", ErrDuplicateTask, asynq.ErrDuplicateTask},
		{"ErrTaskIDConflict", ErrTaskIDConflict, asynq.ErrTaskIDConflict},
	}

	for _, pair := range pairs {
		if !errors.Is(pair.local, pair.remote) || !errors.Is(pair.remote, pair.local) {
			t.Fatalf("%s: expected asynqx and asynq sentinels to be the same error", pair.name)
		}

		wrapped := fmt.Errorf("business context: %w", pair.local)
		if !errors.Is(wrapped, pair.remote) {
			t.Fatalf("%s: expected wrapped asynqx sentinel to match asynq sentinel", pair.name)
		}
	}
}
