// Package asynqx 是 github.com/hibiken/asynq 的生产级封装：
// 一份配置驱动 Producer / Worker / Scheduler / Inspector，
// 以类型安全的任务定义消除裸字符串与手写序列化样板，
// 并提供严谨的启动/优雅关闭生命周期管理。
//
// 最常见的用法是 App 统一入口配合 TaskType：
//
//	// 包级集中声明任务：类型名、payload 类型与默认投递选项绑定在一处。
//	var WelcomeEmail = asynqx.NewTask[EmailPayload]("email:welcome",
//		asynqx.WithTaskQueue("critical"),
//	)
//
//	app, err := asynqx.New(asynqx.WithRedisAddr("127.0.0.1:6379"))
//	if err != nil { ... }
//	defer app.Close()
//
//	// 投递端
//	WelcomeEmail.Enqueue(ctx, app, EmailPayload{UserID: "u-1"})
//
//	// 消费端：注册处理器后运行（Run 阻塞至 ctx 取消并优雅关闭）
//	WelcomeEmail.MustHandle(app, func(ctx context.Context, p EmailPayload) error { return nil })
//	app.Run(ctx)
//
// 需要精细控制时，可使用细粒度的 NewProducer / NewWorker / NewScheduler /
// NewInspector，它们与 App 共享同一套 ConfigOption。
package asynqx
