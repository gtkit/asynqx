package asynqx

import (
	"crypto/tls"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

type stubLogger struct {
	id string
}

func (l *stubLogger) Debug(...any)          {}
func (l *stubLogger) Info(...any)           {}
func (l *stubLogger) Warn(...any)           {}
func (l *stubLogger) Error(...any)          {}
func (l *stubLogger) Fatal(...any)          {}
func (l *stubLogger) Debugf(string, ...any) {}
func (l *stubLogger) Infof(string, ...any)  {}
func (l *stubLogger) Errorf(string, ...any) {}

func TestNewConfigDefaults(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Redis.Addr != defaultRedisAddress {
		t.Fatalf("expected default redis addr %q, got %q", defaultRedisAddress, cfg.Redis.Addr)
	}

	if cfg.Concurrency != defaultConcurrency {
		t.Fatalf("expected default concurrency %d, got %d", defaultConcurrency, cfg.Concurrency)
	}
}

func TestNewConfigAppliesOptions(t *testing.T) {
	cfg, err := NewConfig(
		WithRedisAddrOption("127.0.0.1:6380"),
		WithConcurrencyOption(32),
		WithLocationOption("Asia/Shanghai"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Redis.Addr != "127.0.0.1:6380" {
		t.Fatalf("expected redis addr to be overridden, got %q", cfg.Redis.Addr)
	}

	if cfg.Concurrency != 32 {
		t.Fatalf("expected concurrency 32, got %d", cfg.Concurrency)
	}

	if cfg.Location == nil || cfg.Location.String() != "Asia/Shanghai" {
		t.Fatalf("expected location Asia/Shanghai, got %v", cfg.Location)
	}
}

func TestNewConfigRejectsEmptyRedisAddr(t *testing.T) {
	if _, err := NewConfig(WithRedisAddrOption("")); err == nil {
		t.Fatal("expected error for empty redis addr")
	} else if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	} else if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig alias to match, got %v", err)
	}
}

func TestNewConfigCopiesQueuesMap(t *testing.T) {
	queues := map[string]int{"critical": 2}

	cfg, err := NewConfig(WithQueuesOption(queues))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	queues["critical"] = 1

	if cfg.Queues["critical"] != 2 {
		t.Fatalf("expected copied queue weight to remain 2, got %d", cfg.Queues["critical"])
	}

	if reflect.ValueOf(cfg.Queues).Pointer() == reflect.ValueOf(queues).Pointer() {
		t.Fatal("expected queues map to be copied, but underlying pointer is shared")
	}
}

func TestConfigCloneCopiesRedisTLSConfig(t *testing.T) {
	sourceTLS := &tls.Config{ServerName: "before.example"}
	cfg := Config{
		Redis: asynq.RedisClientOpt{
			Addr:      defaultRedisAddress,
			TLSConfig: sourceTLS,
		},
		Concurrency: defaultConcurrency,
		Location:    time.Local,
		TaskTimeout: defaultTaskTimeout,
	}

	cloned := cfg.clone()
	sourceTLS.ServerName = "after.example"

	if cloned.Redis.TLSConfig == nil {
		t.Fatal("expected tls config to be set")
	}

	if cloned.Redis.TLSConfig == sourceTLS {
		t.Fatal("expected tls config pointer to be copied")
	}

	if cloned.Redis.TLSConfig.ServerName != "before.example" {
		t.Fatalf("expected copied tls config to keep original server name, got %q", cloned.Redis.TLSConfig.ServerName)
	}
}

func TestNewConfigRejectsInvalidLocation(t *testing.T) {
	if _, err := NewConfig(WithLocationOption("Invalid/Zone_XXX")); err == nil {
		t.Fatal("expected error for invalid location")
	} else if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	} else if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig alias to match, got %v", err)
	}
}

func TestNewConfigRejectsNilMiddleware(t *testing.T) {
	if _, err := NewConfig(WithMiddlewareOption(nil)); err == nil {
		t.Fatal("expected error for nil middleware")
	} else if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigAppliesLoggerOption(t *testing.T) {
	logger := &stubLogger{id: "shared"}

	cfg, err := NewConfig(WithLoggerOption(logger))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Logger != logger {
		t.Fatal("expected config logger to be preserved")
	}

	if cfg.asynqConfig().Logger != logger {
		t.Fatal("expected asynq config to use shared logger")
	}

	if cfg.schedulerOptions().Logger != logger {
		t.Fatal("expected scheduler options to use shared logger")
	}
}

func TestNewConfigRejectsTooSmallGroupGracePeriod(t *testing.T) {
	if _, err := NewConfig(WithGroupGracePeriodOption(500 * time.Millisecond)); err == nil {
		t.Fatal("expected error for too small group grace period")
	} else if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigAppliesShutdownTimeoutOption(t *testing.T) {
	cfg, err := NewConfig(WithShutdownTimeoutOption(12 * time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ShutdownTimeout != 12*time.Second {
		t.Fatalf("expected shutdown timeout 12s, got %v", cfg.ShutdownTimeout)
	}

	if cfg.asynqConfig().ShutdownTimeout != 12*time.Second {
		t.Fatalf("expected asynq config shutdown timeout 12s, got %v", cfg.asynqConfig().ShutdownTimeout)
	}
}

func TestLegacyServerArtifactsRemoved(t *testing.T) {
	for _, path := range []string{"server.go", "types.go"} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("expected legacy artifact %q to be removed", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %q: %v", path, err)
		}
	}
}

func TestOptionsSourceContainsOnlyNewAPIOptions(t *testing.T) {
	body, err := os.ReadFile("options.go")
	if err != nil {
		t.Fatalf("read options.go: %v", err)
	}

	source := string(body)
	legacyMarkers := []string{
		"type ServerOption",
		"func WithRedisAddr(",
		"func WithRedisUser(",
		"func WithRedisPassword(",
		"func WithRedisDB(",
		"func WithRedisPoolSize(",
		"func WithDialTimeout(",
		"func WithReadTimeout(",
		"func WithWriteTimeout(",
		"func WithTLSConfig(",
		"func WithConcurrency(",
		"func WithQueues(",
		"func WithRetryDelayFunc(",
		"func WithStrictPriority(",
		"func WithErrorHandler(",
		"func WithHealthCheckFunc(",
		"func WithHealthCheckInterval(",
		"func WithDelayedTaskCheckInterval(",
		"func WithGroupGracePeriod(",
		"func WithGroupMaxDelay(",
		"func WithGroupMaxSize(",
		"func WithMiddleware(",
		"func WithLocation(",
		"func WithIsFailure(",
		"func WithConfig(",
		"func WithServerTaskTimeout(",
		"func WithLogger(",
	}

	for _, marker := range legacyMarkers {
		if strings.Contains(source, marker) {
			t.Fatalf("expected options.go to drop legacy marker %q", marker)
		}
	}
}

func TestPackageDocumentationExists(t *testing.T) {
	body, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("read doc.go: %v", err)
	}

	source := string(body)
	if !strings.Contains(source, "Package asynqx") {
		t.Fatal("expected doc.go to contain a package comment")
	}

	if !strings.ContainsAny(source, "中文任务调度封装配置") {
		t.Fatal("expected doc.go to provide a Chinese package overview")
	}
}
