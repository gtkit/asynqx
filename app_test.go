package asynqx

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// recordingTarget 同时实现 Enqueuer / Registrar / PeriodicRegistrar（与 App 一样），
// 用于验证 TaskType 通过接口正确转发任务类型名与 payload。
type recordingTarget struct {
	enqueuedType    string
	enqueuedPayload any
	handledType     string
	registeredSpec  string
	registeredType  string
}

func (r *recordingTarget) Enqueue(
	_ context.Context, taskType string, payload any, _ ...TaskOption,
) (*asynq.TaskInfo, error) {
	r.enqueuedType = taskType
	r.enqueuedPayload = payload

	return &asynq.TaskInfo{ID: "rec"}, nil
}

func (r *recordingTarget) HandleRaw(taskType string, _ func(context.Context, *asynq.Task) error) error {
	r.handledType = taskType

	return nil
}

func (r *recordingTarget) Register(
	_ context.Context, spec, taskType string, _ any, _ ...TaskOption,
) (string, error) {
	r.registeredSpec = spec
	r.registeredType = taskType

	return "entry-1", nil
}

func TestTaskTypeForwardsThroughInterfaces(t *testing.T) {
	type emailPayload struct{ UserID string }

	def := NewTask[emailPayload]("email:welcome")
	target := &recordingTarget{}

	if _, err := def.Enqueue(context.Background(), target, emailPayload{UserID: "u-1"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if target.enqueuedType != "email:welcome" {
		t.Fatalf("expected enqueued type email:welcome, got %q", target.enqueuedType)
	}

	if err := def.Handle(target, func(context.Context, emailPayload) error { return nil }); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if target.handledType != "email:welcome" {
		t.Fatalf("expected handled type email:welcome, got %q", target.handledType)
	}

	if _, err := def.Register(context.Background(), target, "@every 1h", emailPayload{UserID: "u-2"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if target.registeredSpec != "@every 1h" || target.registeredType != "email:welcome" {
		t.Fatalf("unexpected register args: spec=%q type=%q", target.registeredSpec, target.registeredType)
	}
}

func TestAppLazyComponentsAreSingletons(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	defer app.Close()

	producer1, err := app.Producer()
	if err != nil {
		t.Fatalf("producer: %v", err)
	}

	producer2, err := app.Producer()
	if err != nil {
		t.Fatalf("producer (repeat): %v", err)
	}

	if producer1 != producer2 {
		t.Fatal("expected Producer to be a lazily-created singleton")
	}

	worker1, err := app.Worker()
	if err != nil {
		t.Fatalf("worker: %v", err)
	}

	worker2, err := app.Worker()
	if err != nil {
		t.Fatalf("worker (repeat): %v", err)
	}

	if worker1 != worker2 {
		t.Fatal("expected Worker to be a lazily-created singleton")
	}

	scheduler1, err := app.Scheduler()
	if err != nil {
		t.Fatalf("scheduler: %v", err)
	}

	scheduler2, err := app.Scheduler()
	if err != nil {
		t.Fatalf("scheduler (repeat): %v", err)
	}

	if scheduler1 != scheduler2 {
		t.Fatal("expected Scheduler to be a lazily-created singleton")
	}

	inspector1, err := app.Inspector()
	if err != nil {
		t.Fatalf("inspector: %v", err)
	}

	inspector2, err := app.Inspector()
	if err != nil {
		t.Fatalf("inspector (repeat): %v", err)
	}

	if inspector1 != inspector2 {
		t.Fatal("expected Inspector to be a lazily-created singleton")
	}
}

func TestAppCloseIsIdempotentAndRejectsAfterClose(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if _, err = app.Producer(); err != nil {
		t.Fatalf("producer: %v", err)
	}

	if err = app.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	if err = app.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	if _, err = app.Producer(); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed after close, got %v", err)
	}

	if _, err = app.Enqueue(context.Background(), "email:welcome", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed from Enqueue after close, got %v", err)
	}
}

func TestAppStartWithoutRegistrationFails(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	defer app.Close()

	err = app.Start(context.Background())
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument starting app without registrations, got %v", err)
	}
}

func TestAppOwnsConnectionWhenBuiltFromAddr(t *testing.T) {
	app, err := New(WithRedisAddr(defaultRedisAddress))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if !app.ownsRDB {
		t.Fatal("expected app to own a self-built connection")
	}

	if err = app.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestAppDoesNotOwnExternalConnection(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if app.ownsRDB {
		t.Fatal("expected app not to own an external client")
	}

	if err = app.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// errPingRefused 是 Ping 相关测试使用的静态错误。
var errPingRefused = errors.New("ping connection refused")

// pingStubRedisClient 提供可控的 Ping 行为，用于 App.Ping 测试。
type pingStubRedisClient struct {
	redis.UniversalClient

	pingErr error
}

func (p *pingStubRedisClient) Ping(context.Context) *redis.StatusCmd {
	return redis.NewStatusResult("PONG", p.pingErr)
}

func TestAppPing(t *testing.T) {
	stub := &pingStubRedisClient{}

	app, err := New(WithRedisInstance(stub))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if err = app.Ping(context.Background()); err != nil {
		t.Fatalf("expected ping to succeed, got %v", err)
	}

	stub.pingErr = errPingRefused

	if err = app.Ping(context.Background()); !errors.Is(err, errPingRefused) {
		t.Fatalf("expected ping to propagate redis error, got %v", err)
	}

	if err = app.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err = app.Ping(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed after close, got %v", err)
	}
}

func TestAppStartRollsBackWorkerWhenSchedulerStartFails(t *testing.T) {
	stubWorker := &stubWorkerRunner{}
	stubScheduler := &stubSchedulerRunner{startErr: errSchedulerStartFailed}

	restoreWorker := setWorkerRunnerFactoryForTest(func(Config) (workerRunner, error) {
		return stubWorker, nil
	})
	defer restoreWorker()

	restoreScheduler := setSchedulerRunnerFactoryForTest(func(Config) (schedulerRunner, error) {
		return stubScheduler, nil
	})
	defer restoreScheduler()

	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	defer app.Close()

	if err = app.HandleRaw("email:welcome", func(context.Context, *asynq.Task) error {
		return nil
	}); err != nil {
		t.Fatalf("handle raw: %v", err)
	}

	if _, err = app.Register(context.Background(), "@every 1m", "email:welcome", nil); err != nil {
		t.Fatalf("register: %v", err)
	}

	err = app.Start(context.Background())
	if !errors.Is(err, errSchedulerStartFailed) {
		t.Fatalf("expected scheduler start error, got %v", err)
	}

	if got := stubWorker.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected started worker to be rolled back exactly once, got %d shutdown calls", got)
	}
}

func TestAppUnregisterForwardsToScheduler(t *testing.T) {
	stubScheduler := &stubSchedulerRunner{}

	restore := setSchedulerRunnerFactoryForTest(func(Config) (schedulerRunner, error) {
		return stubScheduler, nil
	})
	defer restore()

	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	defer app.Close()

	entryID, err := app.Register(context.Background(), "@every 1m", "email:welcome", nil)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if err = app.Unregister(context.Background(), entryID); err != nil {
		t.Fatalf("unregister: %v", err)
	}

	if stubScheduler.unregisteredID != entryID {
		t.Fatalf("expected unregistered entry %q, got %q", entryID, stubScheduler.unregisteredID)
	}
}

// TestAppLazyComponentConcurrentAccess 验证懒创建的无锁读路径在并发下仍返回同一实例，
// 并由 race 检测器兜底数据竞争。
func TestAppLazyComponentConcurrentAccess(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	defer app.Close()

	const goroutines = 32

	producers := make([]*Producer, goroutines)

	var waitGroup sync.WaitGroup

	for index := range goroutines {
		waitGroup.Go(func() {
			producer, producerErr := app.Producer()
			if producerErr != nil {
				t.Errorf("producer: %v", producerErr)

				return
			}

			producers[index] = producer
		})
	}

	waitGroup.Wait()

	for index := 1; index < goroutines; index++ {
		if producers[index] != producers[0] {
			t.Fatal("expected all goroutines to observe the same Producer instance")
		}
	}
}

func TestNewFromConfigCreatesAppAndRejectsInvalid(t *testing.T) {
	cfg, err := NewConfig(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new config: %v", err)
	}

	app, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("new from config: %v", err)
	}

	if err = app.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err = NewFromConfig(Config{}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected ErrInvalidConfiguration for zero config, got %v", err)
	}
}

func TestAppRunStartsAndShutsDownOnContextCancel(t *testing.T) {
	stubWorker := &stubWorkerRunner{started: make(chan struct{})}

	restore := setWorkerRunnerFactoryForTest(func(Config) (workerRunner, error) {
		return stubWorker, nil
	})
	defer restore()

	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if err = app.HandleRaw("email:welcome", func(context.Context, *asynq.Task) error {
		return nil
	}); err != nil {
		t.Fatalf("handle raw: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)

	go func() {
		runErr <- app.Run(ctx)
	}()

	<-stubWorker.started
	cancel()

	if err = <-runErr; err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	if got := stubWorker.shutdownCalls.Load(); got != 1 {
		t.Fatalf("expected worker shutdown once, got %d", got)
	}

	// Run 结束后 App 已整体关闭。
	if _, err = app.Producer(); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed after run, got %v", err)
	}
}

func TestAppRunShutsDownWhenStartFails(t *testing.T) {
	app, err := New(WithRedisInstance(&stubRedisClient{}))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	// 未注册任何处理器/周期任务时 Run 应快速失败并完成清理。
	err = app.Run(context.Background())
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	if _, err = app.Producer(); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed after failed run, got %v", err)
	}
}
