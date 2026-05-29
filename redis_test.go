package asynqx

import (
	"errors"
	"testing"
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
