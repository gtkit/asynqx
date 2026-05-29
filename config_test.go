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

func (l *stubLogger) Debug(...any) {}
func (l *stubLogger) Info(...any)  {}
func (l *stubLogger) Warn(...any)  {}
func (l *stubLogger) Error(...any) {}
func (l *stubLogger) Fatal(...any) {}

func TestNewConfigDefaults(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clientOpt, ok := cfg.Redis.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("expected redis client option, got %T", cfg.Redis)
	}

	if clientOpt.Addr != defaultRedisAddress {
		t.Fatalf("expected default redis addr %q, got %q", defaultRedisAddress, clientOpt.Addr)
	}

	if cfg.Concurrency != defaultConcurrency {
		t.Fatalf("expected default concurrency %d, got %d", defaultConcurrency, cfg.Concurrency)
	}
}

func TestNewConfigSupportsRedisFailoverOption(t *testing.T) {
	cfg, err := NewConfig(WithRedisFailover(asynq.RedisFailoverClientOpt{
		MasterName:    "primary",
		SentinelAddrs: []string{"127.0.0.1:26379", "127.0.0.1:26380"},
		DB:            2,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	failover, ok := cfg.Redis.(asynq.RedisFailoverClientOpt)
	if !ok {
		t.Fatalf("expected redis failover option, got %T", cfg.Redis)
	}

	if failover.MasterName != "primary" {
		t.Fatalf("expected master name primary, got %q", failover.MasterName)
	}

	if len(failover.SentinelAddrs) != 2 {
		t.Fatalf("expected copied sentinel addrs, got %v", failover.SentinelAddrs)
	}
}

func TestNewConfigSupportsRedisClusterOption(t *testing.T) {
	cfg, err := NewConfig(WithRedisCluster(asynq.RedisClusterClientOpt{
		Addrs:        []string{"127.0.0.1:6379", "127.0.0.1:6380"},
		MaxRedirects: 5,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cluster, ok := cfg.Redis.(asynq.RedisClusterClientOpt)
	if !ok {
		t.Fatalf("expected redis cluster option, got %T", cfg.Redis)
	}

	if cluster.MaxRedirects != 5 {
		t.Fatalf("expected max redirects 5, got %d", cluster.MaxRedirects)
	}

	if len(cluster.Addrs) != 2 {
		t.Fatalf("expected copied cluster addrs, got %v", cluster.Addrs)
	}
}

func TestNewConfigRejectsEmptyRedisFailoverMasterName(t *testing.T) {
	_, err := NewConfig(WithRedisFailover(asynq.RedisFailoverClientOpt{
		SentinelAddrs: []string{"127.0.0.1:26379"},
	}))
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigRejectsEmptyRedisClusterAddrs(t *testing.T) {
	_, err := NewConfig(WithRedisCluster(asynq.RedisClusterClientOpt{}))
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigRejectsEmptyRedisClusterAddr(t *testing.T) {
	_, err := NewConfig(WithRedisCluster(asynq.RedisClusterClientOpt{
		Addrs: []string{"127.0.0.1:6379", " "},
	}))
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigRejectsEmptyRedisSentinelAddr(t *testing.T) {
	_, err := NewConfig(WithRedisFailover(asynq.RedisFailoverClientOpt{
		MasterName:    "primary",
		SentinelAddrs: []string{" "},
	}))
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigRejectsNegativeRedisFailoverDB(t *testing.T) {
	_, err := NewConfig(WithRedisFailover(asynq.RedisFailoverClientOpt{
		MasterName:    "primary",
		SentinelAddrs: []string{"127.0.0.1:26379"},
		DB:            -1,
	}))
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigCopiesRedisSlicesAndTLS(t *testing.T) {
	tlsConfig := &tls.Config{ServerName: "redis.example"}
	sentinelAddrs := []string{"127.0.0.1:26379"}

	cfg, err := NewConfig(WithRedisFailover(asynq.RedisFailoverClientOpt{
		MasterName:    "primary",
		SentinelAddrs: sentinelAddrs,
		TLSConfig:     tlsConfig,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sentinelAddrs[0] = "127.0.0.1:26380"
	tlsConfig.ServerName = "mutated.example"

	failover, ok := cfg.Redis.(asynq.RedisFailoverClientOpt)
	if !ok {
		t.Fatalf("expected redis failover option, got %T", cfg.Redis)
	}

	if failover.SentinelAddrs[0] != "127.0.0.1:26379" {
		t.Fatalf("expected sentinel addrs to be copied, got %v", failover.SentinelAddrs)
	}

	if failover.TLSConfig == nil || failover.TLSConfig.ServerName != "redis.example" {
		t.Fatalf("expected tls config to be copied, got %#v", failover.TLSConfig)
	}
}

func TestNewConfigCopiesRedisClusterSlicesAndTLS(t *testing.T) {
	tlsConfig := &tls.Config{ServerName: "redis.example"}
	addrs := []string{"127.0.0.1:6379"}

	cfg, err := NewConfig(WithRedisCluster(asynq.RedisClusterClientOpt{
		Addrs:     addrs,
		TLSConfig: tlsConfig,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	addrs[0] = "127.0.0.1:6380"
	tlsConfig.ServerName = "mutated.example"

	cluster, ok := cfg.Redis.(asynq.RedisClusterClientOpt)
	if !ok {
		t.Fatalf("expected redis cluster option, got %T", cfg.Redis)
	}

	if cluster.Addrs[0] != "127.0.0.1:6379" {
		t.Fatalf("expected cluster addrs to be copied, got %v", cluster.Addrs)
	}

	if cluster.TLSConfig == nil || cluster.TLSConfig.ServerName != "redis.example" {
		t.Fatalf("expected tls config to be copied, got %#v", cluster.TLSConfig)
	}
}

func TestNewConfigRejectsSingleNodeOptionAfterCluster(t *testing.T) {
	_, err := NewConfig(
		WithRedisCluster(asynq.RedisClusterClientOpt{Addrs: []string{"127.0.0.1:6379"}}),
		WithRedisAddr("127.0.0.1:6380"),
	)
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigAppliesOptions(t *testing.T) {
	cfg, err := NewConfig(
		WithRedisAddr("127.0.0.1:6380"),
		WithConcurrency(32),
		WithLocation("Asia/Shanghai"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clientOpt, ok := cfg.Redis.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("expected redis client option, got %T", cfg.Redis)
	}

	if clientOpt.Addr != "127.0.0.1:6380" {
		t.Fatalf("expected redis addr to be overridden, got %q", clientOpt.Addr)
	}

	if cfg.Concurrency != 32 {
		t.Fatalf("expected concurrency 32, got %d", cfg.Concurrency)
	}

	if cfg.Location == nil || cfg.Location.String() != "Asia/Shanghai" {
		t.Fatalf("expected location Asia/Shanghai, got %v", cfg.Location)
	}
}

func TestNewConfigRejectsEmptyRedisAddr(t *testing.T) {
	if _, err := NewConfig(WithRedisAddr("")); err == nil {
		t.Fatal("expected error for empty redis addr")
	} else if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	} else if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig alias to match, got %v", err)
	}
}

func TestNewConfigCopiesQueuesMap(t *testing.T) {
	queues := map[string]int{"critical": 2}

	cfg, err := NewConfig(WithQueues(queues))
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

	clonedClientOpt, ok := cloned.Redis.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("expected redis client option, got %T", cloned.Redis)
	}

	if clonedClientOpt.TLSConfig == nil {
		t.Fatal("expected tls config to be set")
	}

	if clonedClientOpt.TLSConfig == sourceTLS {
		t.Fatal("expected tls config pointer to be copied")
	}

	if clonedClientOpt.TLSConfig.ServerName != "before.example" {
		t.Fatalf("expected copied tls config to keep original server name, got %q", clonedClientOpt.TLSConfig.ServerName)
	}
}

func TestNewConfigRejectsInvalidLocation(t *testing.T) {
	if _, err := NewConfig(WithLocation("Invalid/Zone_XXX")); err == nil {
		t.Fatal("expected error for invalid location")
	} else if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	} else if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig alias to match, got %v", err)
	}
}

func TestNewConfigRejectsEmptyLocation(t *testing.T) {
	if _, err := NewConfig(WithLocation("")); err == nil {
		t.Fatal("expected error for empty location")
	} else if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestWithTLSConfigCopiesTLSConfig(t *testing.T) {
	sourceTLS := &tls.Config{ServerName: "before.example"}

	cfg, err := NewConfig(WithTLSConfig(sourceTLS))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sourceTLS.ServerName = "after.example"

	clientOpt, ok := cfg.Redis.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("expected redis client option, got %T", cfg.Redis)
	}

	if clientOpt.TLSConfig == nil {
		t.Fatal("expected tls config to be set")
	}

	if clientOpt.TLSConfig == sourceTLS {
		t.Fatal("expected tls config pointer to be copied")
	}

	if clientOpt.TLSConfig.ServerName != "before.example" {
		t.Fatalf("expected copied tls config to keep original server name, got %q", clientOpt.TLSConfig.ServerName)
	}
}

func TestNewConfigRejectsNilMiddleware(t *testing.T) {
	if _, err := NewConfig(WithMiddleware(nil)); err == nil {
		t.Fatal("expected error for nil middleware")
	} else if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigAppliesLoggerOption(t *testing.T) {
	logger := &stubLogger{id: "shared"}

	cfg, err := NewConfig(WithLogger(logger))
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

func TestNewConfigAppliesPingOnStartOption(t *testing.T) {
	cfg, err := NewConfig(WithPingOnStart(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.PingOnStart {
		t.Fatal("expected ping on start to be enabled")
	}
}

func TestNewConfigRejectsTooSmallGroupGracePeriod(t *testing.T) {
	if _, err := NewConfig(WithGroupGracePeriod(500 * time.Millisecond)); err == nil {
		t.Fatal("expected error for too small group grace period")
	} else if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration, got %v", err)
	}
}

func TestNewConfigAppliesShutdownTimeoutOption(t *testing.T) {
	cfg, err := NewConfig(WithShutdownTimeout(12 * time.Second))
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

func TestOptionsSourceKeepsRecommendedAndDeprecatedOptionNames(t *testing.T) {
	body, err := os.ReadFile("options.go")
	if err != nil {
		t.Fatalf("read options.go: %v", err)
	}

	source := string(body)
	recommendedMarkers := []string{
		"func WithRedis(",
		"func WithRedisAddr(",
		"func WithConcurrency(",
		"func WithDefaultTaskTimeout(",
		"func WithLogger(",
	}

	for _, marker := range recommendedMarkers {
		if !strings.Contains(source, marker) {
			t.Fatalf("expected options.go to contain recommended marker %q", marker)
		}
	}

	deprecatedMarkers := []string{
		"var WithRedisOption = WithRedis",
		"var WithRedisAddrOption = WithRedisAddr",
		"var WithConcurrencyOption = WithConcurrency",
		"var WithTaskTimeoutOption = WithDefaultTaskTimeout",
	}

	for _, marker := range deprecatedMarkers {
		if !strings.Contains(source, marker) {
			t.Fatalf("expected options.go to keep deprecated compatibility marker %q", marker)
		}
	}

	if strings.Contains(source, "type ServerOption") {
		t.Fatal("expected options.go to keep old ServerOption artifact removed")
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
