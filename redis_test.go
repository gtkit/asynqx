package asynqx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type invalidRedisConnOpt struct{}

func (invalidRedisConnOpt) MakeRedisClient() any {
	return struct{}{}
}

func TestNewRedisUniversalClientRejectsNilOption(t *testing.T) {
	_, err := newRedisUniversalClient(nil)
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewRedisUniversalClientRejectsUnsupportedClient(t *testing.T) {
	_, err := newRedisUniversalClient(invalidRedisConnOpt{})
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestPingRedisOptionOnStartRejectsUnsupportedClient(t *testing.T) {
	err := pingRedisOptionOnStart(context.TODO(), invalidRedisConnOpt{}, 0)
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestPingRedisOnStartSucceedsAndPropagatesError(t *testing.T) {
	stub := &pingStubRedisClient{}

	if err := pingRedisOnStart(context.Background(), stub, time.Second); err != nil {
		t.Fatalf("unexpected ping error: %v", err)
	}

	stub.pingErr = errPingRefused

	err := pingRedisOnStart(nil, stub, 0) //nolint:staticcheck // 显式验证 nil ctx 防御分支
	if err == nil || !strings.Contains(err.Error(), "ping redis on start") {
		t.Fatalf("expected wrapped ping error, got %v", err)
	}
}
