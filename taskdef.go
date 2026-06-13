package asynqx

import (
	"context"

	"github.com/hibiken/asynq"
)

// Enqueuer 表示可投递任务的目标。*Producer 与 *App 均实现该接口，
// 因此 TaskType.Enqueue 既能用细粒度的 Producer，也能用 App 容器，
// 也便于在测试中注入 mock。
type Enqueuer interface {
	Enqueue(ctx context.Context, taskType string, payload any, opts ...TaskOption) (*asynq.TaskInfo, error)
}

// Registrar 表示可注册任务处理器的目标。*Worker 与 *App 均实现该接口。
type Registrar interface {
	HandleRaw(taskType string, handler func(context.Context, *asynq.Task) error) error
}

// PeriodicRegistrar 表示可注册周期任务的目标。*Scheduler 与 *App 均实现该接口。
type PeriodicRegistrar interface {
	Register(ctx context.Context, spec, taskType string, payload any, opts ...TaskOption) (string, error)
}

// TaskType 将任务类型名与其 payload 类型 T 绑定在一起，让投递端、调度端和
// 消费端共享同一份定义，从而在编译期保证任务类型名与 payload 类型一致，
// 避免裸字符串拼写错误或两端 payload 类型不匹配。
//
// 推荐在包级集中声明所有任务类型：
//
//	var WelcomeEmail = asynqx.NewTask[EmailPayload]("email:welcome")
//
//	WelcomeEmail.Enqueue(ctx, app, EmailPayload{UserID: "u-1"})
//	WelcomeEmail.Handle(app, func(ctx context.Context, p EmailPayload) error { return nil })
//
// Enqueue / Register / Handle 接受接口而非具体类型，因此既可传 App 容器，
// 也可传细粒度的 Producer / Worker / Scheduler。
type TaskType[T any] struct {
	name string
}

// NewTaskType 创建一个绑定 payload 类型 T 的任务类型定义。
// name 的合法性在 Enqueue、Register 和 Handle 调用时统一校验，
// 因此可以安全地用字面量在包级变量中声明。
func NewTaskType[T any](name string) TaskType[T] {
	return TaskType[T]{name: name}
}

// NewTask 是 NewTaskType 的简写别名，便于日常声明任务类型。
func NewTask[T any](name string) TaskType[T] {
	return TaskType[T]{name: name}
}

// Name 返回任务类型名。
func (t TaskType[T]) Name() string {
	return t.name
}

// Enqueue 使用绑定的任务类型名，通过任意 Enqueuer（*Producer 或 *App）投递一个 payload 为 T 的任务。
func (t TaskType[T]) Enqueue(
	ctx context.Context,
	enqueuer Enqueuer,
	payload T,
	opts ...TaskOption,
) (*asynq.TaskInfo, error) {
	return enqueuer.Enqueue(ctx, t.name, payload, opts...)
}

// Register 使用绑定的任务类型名，通过任意 PeriodicRegistrar（*Scheduler 或 *App）注册一个 payload 为 T 的周期任务。
func (t TaskType[T]) Register(
	ctx context.Context,
	registrar PeriodicRegistrar,
	spec string,
	payload T,
	opts ...TaskOption,
) (string, error) {
	return registrar.Register(ctx, spec, t.name, payload, opts...)
}

// Handle 使用绑定的任务类型名，通过任意 Registrar（*Worker 或 *App）注册一个带泛型 payload 解码的处理器。
func (t TaskType[T]) Handle(registrar Registrar, handler func(context.Context, T) error) error {
	return Handle[T](registrar, t.name, handler)
}
