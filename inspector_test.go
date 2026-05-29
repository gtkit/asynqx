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
