package asynqx

import (
	"math"
	"testing"
	"time"
)

// 停机预算契约：预算 = timeout + 余量（timeout/10，上限 maxShutdownGrace）。
// 若余量丢失（预算退回 timeout 本身），本测试的下界断言会失败。
func TestNewShutdownContextAddsGrace(t *testing.T) {
	const timeout = 10 * time.Second

	before := time.Now()

	ctx, cancel := newShutdownContext(timeout)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected a deadline for positive timeout")
	}

	budget := deadline.Sub(before)
	if budget <= timeout {
		t.Fatalf("expected budget to exceed timeout %v by grace, got %v", timeout, budget)
	}

	if budget > timeout+timeout/10+time.Second {
		t.Fatalf("expected grace of about %v, got total budget %v", timeout/10, budget)
	}
}

func TestNewShutdownContextCapsGrace(t *testing.T) {
	const timeout = 100 * time.Second

	before := time.Now()

	ctx, cancel := newShutdownContext(timeout)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected a deadline for positive timeout")
	}

	budget := deadline.Sub(before)
	if budget <= timeout || budget > timeout+maxShutdownGrace+time.Second {
		t.Fatalf("expected budget in (%v, %v], got %v", timeout, timeout+maxShutdownGrace, budget)
	}
}

func TestNewShutdownContextZeroTimeoutHasNoDeadline(t *testing.T) {
	ctx, cancel := newShutdownContext(0)
	defer cancel()

	if _, ok := ctx.Deadline(); ok {
		t.Fatal("expected no deadline for zero timeout")
	}
}

// timeout 接近 time.Duration 上限时，加余量不得溢出为负导致 context 立即过期。
func TestNewShutdownContextHugeTimeoutDoesNotOverflow(t *testing.T) {
	ctx, cancel := newShutdownContext(time.Duration(math.MaxInt64))
	defer cancel()

	if err := ctx.Err(); err != nil {
		t.Fatalf("expected live context for huge timeout, got %v", err)
	}
}
