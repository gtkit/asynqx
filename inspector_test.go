package asynqx

import (
	"errors"
	"testing"

	"github.com/hibiken/asynq"
)

func TestNewInspectorRejectsNilFactory(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	_, err = newInspector(cfg, nil)
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewInspectorCreatesAndClosesInspector(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	expected := asynq.NewInspector(cfg.Redis)
	defer expected.Close()

	inspector, err := newInspector(cfg, func(Config) (*Inspector, error) {
		return expected, nil
	})
	if err != nil {
		t.Fatalf("unexpected inspector error: %v", err)
	}

	if inspector != expected {
		t.Fatal("expected inspector returned from factory")
	}
}

func TestNewInspectorUsesConfiguredFactory(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	expected := asynq.NewInspector(cfg.Redis)
	defer expected.Close()

	restore := setInspectorClientFactoryForTest(func(Config) (*Inspector, error) {
		return expected, nil
	})
	defer restore()

	inspector, err := NewInspector()
	if err != nil {
		t.Fatalf("unexpected inspector error: %v", err)
	}

	if inspector != expected {
		t.Fatal("expected inspector returned from configured factory")
	}
}

func TestNewInspectorFromConfigUsesConfig(t *testing.T) {
	cfg, err := NewConfig(WithRedisDB(3))
	if err != nil {
		t.Fatalf("unexpected config error: %v", err)
	}

	expected := asynq.NewInspector(cfg.Redis)
	defer expected.Close()

	restore := setInspectorClientFactoryForTest(func(got Config) (*Inspector, error) {
		clientOpt, ok := got.Redis.(asynq.RedisClientOpt)
		if !ok {
			t.Fatalf("expected redis client option, got %T", got.Redis)
		}

		if clientOpt.DB != 3 {
			t.Fatalf("expected redis db 3, got %d", clientOpt.DB)
		}

		return expected, nil
	})
	defer restore()

	inspector, err := NewInspectorFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected inspector error: %v", err)
	}

	if inspector != expected {
		t.Fatal("expected inspector returned from configured factory")
	}
}
