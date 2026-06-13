//go:build integration

// 这些是需要真实 Redis 的集成冒烟测试，默认构建/测试不包含本文件。
// 显式启用方式：
//
//	go test -tags integration -run Integration ./...
//
// Redis 连接通过环境变量配置（默认 127.0.0.1:6379、DB 15）：
//
//	ASYNQX_TEST_REDIS_ADDR=127.0.0.1:6379
//	ASYNQX_TEST_REDIS_DB=15
//
// 注意：测试会对所选 DB 执行 FLUSHDB 以隔离用例，请勿指向生产库。
// Redis 不可用时用例会自动 Skip，不会失败。
package asynqx_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gtkit/asynqx"
	"github.com/redis/go-redis/v9"
)

// capturingLogger 线程安全地捕获 asynqx/asynq 写出的日志，用于断言关闭路径是否产生误导性错误。
type capturingLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *capturingLogger) record(level string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.lines = append(l.lines, level+": "+fmt.Sprint(args...))
}

func (l *capturingLogger) Debug(args ...any) { l.record("DEBUG", args...) }
func (l *capturingLogger) Info(args ...any)  { l.record("INFO", args...) }
func (l *capturingLogger) Warn(args ...any)  { l.record("WARN", args...) }
func (l *capturingLogger) Error(args ...any) { l.record("ERROR", args...) }
func (l *capturingLogger) Fatal(args ...any) { l.record("FATAL", args...) }

func (l *capturingLogger) contains(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, line := range l.lines {
		if strings.Contains(line, sub) {
			return true
		}
	}

	return false
}

func (l *capturingLogger) dump() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return strings.Join(l.lines, "\n")
}

func testRedisAddr() string {
	if v := os.Getenv("ASYNQX_TEST_REDIS_ADDR"); v != "" {
		return v
	}

	return "127.0.0.1:6379"
}

func testRedisDB(t *testing.T) int {
	t.Helper()

	v := os.Getenv("ASYNQX_TEST_REDIS_DB")
	if v == "" {
		return 15
	}

	db, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("invalid ASYNQX_TEST_REDIS_DB %q: %v", v, err)
	}

	return db
}

// newTestRedisClient 连接测试 Redis；不可用时跳过用例。成功后会 FLUSHDB 以隔离用例。
func newTestRedisClient(t *testing.T) (*redis.Client, int) {
	t.Helper()

	db := testRedisDB(t)
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr(), DB: db})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("跳过集成测试：redis 不可用 (%s db=%d): %v", testRedisAddr(), db, err)
	}

	if err := client.FlushDB(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("flush test db: %v", err)
	}

	return client, db
}

// TestIntegrationSchedulerOwnedShutdownNoSharedConnError 验证：asynqx 自建连接的 Scheduler
// 在优雅关闭时不再触发 asynq 的 "redis connection is shared" / "Failed to close redis
// client connection" 错误日志（对应工厂改用 asynq.NewScheduler 的修复）。
func TestIntegrationSchedulerOwnedShutdownNoSharedConnError(t *testing.T) {
	_, db := newTestRedisClient(t)

	logger := &capturingLogger{}

	scheduler, err := asynqx.NewScheduler(
		asynqx.WithRedisAddr(testRedisAddr()),
		asynqx.WithRedisDB(db),
		asynqx.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}

	if err = scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}

	if _, err = scheduler.Register(
		context.Background(), "@every 1h", "integration:noop", map[string]string{"k": "v"},
	); err != nil {
		t.Fatalf("register: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err = scheduler.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown scheduler: %v", err)
	}

	for _, marker := range []string{"redis connection is shared", "Failed to close redis client connection"} {
		if logger.contains(marker) {
			t.Fatalf("自建 Scheduler 关闭出现共享连接错误日志 %q\n全部日志:\n%s", marker, logger.dump())
		}
	}
}

// TestIntegrationProducerExternalClientCloseReturnsNil 验证：复用外部共享客户端
// （WithRedisInstance）时 Producer.Close 返回 nil，且外部客户端未被 asynqx 关闭、仍可用。
func TestIntegrationProducerExternalClientCloseReturnsNil(t *testing.T) {
	client, _ := newTestRedisClient(t)
	defer client.Close()

	producer, err := asynqx.NewProducer(asynqx.WithRedisInstance(client))
	if err != nil {
		t.Fatalf("new producer: %v", err)
	}

	if _, err = producer.Enqueue(
		context.Background(), "integration:ext", map[string]string{"id": "1"}, asynqx.WithTaskQueue("integration"),
	); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err = producer.Close(); err != nil {
		t.Fatalf("外部共享客户端下 Producer.Close 应返回 nil，实际: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err = client.Ping(ctx).Err(); err != nil {
		t.Fatalf("外部客户端在 Producer.Close 后应仍然可用，Ping 失败: %v", err)
	}
}

// TestIntegrationEndToEnd 验证端到端链路与 json/v2 序列化往返：Producer 投递 → Worker 解码消费。
func TestIntegrationEndToEnd(t *testing.T) {
	_, db := newTestRedisClient(t)

	const (
		taskType = "integration:e2e"
		queue    = "integration"
	)

	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	got := make(chan payload, 1)

	worker, err := asynqx.NewWorker(
		asynqx.WithRedisAddr(testRedisAddr()),
		asynqx.WithRedisDB(db),
		asynqx.WithQueues(map[string]int{queue: 1}),
	)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err = asynqx.Handle(worker, taskType, func(_ context.Context, p payload) error {
		select {
		case got <- p:
		default:
		}

		return nil
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if err = worker.Start(context.Background()); err != nil {
		t.Fatalf("start worker: %v", err)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = worker.Shutdown(ctx)
	}()

	producer, err := asynqx.NewProducer(
		asynqx.WithRedisAddr(testRedisAddr()),
		asynqx.WithRedisDB(db),
	)
	if err != nil {
		t.Fatalf("new producer: %v", err)
	}

	defer producer.Close()

	want := payload{Name: "alice", Count: 7}

	if _, err = producer.Enqueue(context.Background(), taskType, want, asynqx.WithTaskQueue(queue)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case p := <-got:
		if p != want {
			t.Fatalf("payload 往返不一致：期望 %+v，实际 %+v", want, p)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("超时：任务未在 15s 内被 Worker 消费")
	}
}

// TestIntegrationInspectorExternalClientCloseKeepsClientUsable 记录并验证外部共享客户端下
// Inspector.Close 的行为：asynq 会返回 "redis connection is shared" 错误且不关闭连接，
// 外部客户端在此之后仍可继续使用（生命周期由调用方负责）。
func TestIntegrationInspectorExternalClientCloseKeepsClientUsable(t *testing.T) {
	client, _ := newTestRedisClient(t)
	defer client.Close()

	inspector, err := asynqx.NewInspector(asynqx.WithRedisInstance(client))
	if err != nil {
		t.Fatalf("new inspector: %v", err)
	}

	if closeErr := inspector.Close(); closeErr == nil {
		t.Log("注意：外部客户端下 Inspector.Close 返回 nil（asynq 行为可能已变化）")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err = client.Ping(ctx).Err(); err != nil {
		t.Fatalf("外部客户端在 Inspector.Close 后应仍然可用: %v", err)
	}
}
