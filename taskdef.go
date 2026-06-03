package asynqx

import (
	"context"

	"github.com/hibiken/asynq"
)

// TaskType 将任务类型名与其 payload 类型 T 绑定在一起，让投递端、调度端和
// 消费端共享同一份定义，从而在编译期保证任务类型名与 payload 类型一致，
// 避免裸字符串拼写错误或两端 payload 类型不匹配。
//
// 推荐在包级集中声明所有任务类型：
//
//	var WelcomeEmail = asynqx.NewTaskType[EmailPayload]("email:welcome")
//
//	WelcomeEmail.Enqueue(ctx, producer, EmailPayload{UserID: "u-1"})
//	WelcomeEmail.Handle(worker, func(ctx context.Context, p EmailPayload) error { return nil })
type TaskType[T any] struct {
	name string
}

// NewTaskType 创建一个绑定 payload 类型 T 的任务类型定义。
// name 的合法性在 Enqueue、Register 和 Handle 调用时统一校验，
// 因此可以安全地用字面量在包级变量中声明。
func NewTaskType[T any](name string) TaskType[T] {
	return TaskType[T]{name: name}
}

// Name 返回任务类型名。
func (t TaskType[T]) Name() string {
	return t.name
}

// Enqueue 使用绑定的任务类型名，通过 producer 投递一个 payload 为 T 的任务。
func (t TaskType[T]) Enqueue(
	ctx context.Context,
	producer *Producer,
	payload T,
	opts ...TaskOption,
) (*asynq.TaskInfo, error) {
	return producer.Enqueue(ctx, t.name, payload, opts...)
}

// Register 使用绑定的任务类型名，通过 scheduler 注册一个 payload 为 T 的周期任务。
func (t TaskType[T]) Register(
	ctx context.Context,
	scheduler *Scheduler,
	spec string,
	payload T,
	opts ...TaskOption,
) (string, error) {
	return scheduler.Register(ctx, spec, t.name, payload, opts...)
}

// Handle 使用绑定的任务类型名，向 worker 注册一个带泛型 payload 解码的处理器。
func (t TaskType[T]) Handle(worker *Worker, handler func(context.Context, T) error) error {
	return Handle[T](worker, t.name, handler)
}
