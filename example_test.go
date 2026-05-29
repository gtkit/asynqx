package asynqx_test

import (
	"context"
	"fmt"

	"github.com/gtkit/asynqx"
)

type exampleEmailPayload struct {
	UserID string `json:"user_id"`
}

func ExampleNewBroker() {
	broker, err := asynqx.NewBroker(asynqx.WithRedisAddr("127.0.0.1:6379"))
	if err != nil {
		fmt.Println("new broker:", err)

		return
	}

	_, err = broker.Enqueue(
		context.Background(),
		"email:welcome",
		exampleEmailPayload{UserID: "u-1001"},
		asynqx.WithTaskQueue("critical"),
	)
	if err != nil {
		fmt.Println("enqueue:", err)
	}

	if err = broker.Close(); err != nil {
		fmt.Println("close broker:", err)
	}
}

func ExampleHandle() {
	worker, err := asynqx.NewWorker(asynqx.WithRedisAddr("127.0.0.1:6379"))
	if err != nil {
		fmt.Println("new worker:", err)

		return
	}

	err = asynqx.Handle(worker, "email:welcome", func(_ context.Context, _ exampleEmailPayload) error {
		return nil
	})
	if err != nil {
		fmt.Println("handle:", err)
	}

	if err = worker.Shutdown(context.Background()); err != nil {
		fmt.Println("shutdown worker:", err)
	}
}

func ExampleScheduler_Register() {
	scheduler, err := asynqx.NewScheduler(
		asynqx.WithRedisAddr("127.0.0.1:6379"),
		asynqx.WithLocation("Asia/Shanghai"),
	)
	if err != nil {
		fmt.Println("new scheduler:", err)

		return
	}

	_, err = scheduler.Register(
		context.Background(),
		"@every 1m",
		"email:welcome",
		exampleEmailPayload{UserID: "u-1001"},
		asynqx.WithTaskQueue("default"),
	)
	if err != nil {
		fmt.Println("register:", err)
	}

	if err = scheduler.Shutdown(context.Background()); err != nil {
		fmt.Println("shutdown scheduler:", err)
	}
}
