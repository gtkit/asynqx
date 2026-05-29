package asynqx_test

import (
	"context"
	"time"

	"github.com/gtkit/asynqx"
)

type exampleEmailPayload struct {
	UserID string `json:"user_id"`
}

func ExampleNewBroker() {
	broker, err := asynqx.NewBroker(asynqx.WithRedisAddrOption("127.0.0.1:6379"))
	_ = broker
	_ = err

	// _, err = broker.Enqueue(
	// 	context.Background(),
	// 	"email:welcome",
	// 	exampleEmailPayload{UserID: "u-1001"},
	// 	asynqx.WithTaskQueue("critical"),
	// 	asynqx.WithTaskTimeout(30*time.Second),
	// )
}

func ExampleHandle() {
	worker, err := asynqx.NewWorker(asynqx.WithRedisAddrOption("127.0.0.1:6379"))
	_ = err

	err = asynqx.Handle(worker, "email:welcome", func(ctx context.Context, payload exampleEmailPayload) error {
		_ = ctx
		_ = payload

		return nil
	})
	_ = err
}

func ExampleScheduler_Register() {
	scheduler, err := asynqx.NewScheduler(
		asynqx.WithRedisAddrOption("127.0.0.1:6379"),
		asynqx.WithLocationOption("Asia/Shanghai"),
	)
	_ = err

	_, err = scheduler.Register(
		context.Background(),
		"@every 1m",
		"email:welcome",
		exampleEmailPayload{UserID: "u-1001"},
		asynqx.WithTaskQueue("default"),
	)
	_ = err
	_ = time.Second
}
