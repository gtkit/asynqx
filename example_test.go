package asynqx_test

import (
	"context"
	"log"
	"time"

	"github.com/gtkit/asynqx"
)

type exampleEmailPayload struct {
	UserID string `json:"user_id"`
}

func ExampleNewBroker() {
	broker, err := asynqx.NewBroker(asynqx.WithRedisAddr("127.0.0.1:6379"))
	if err != nil {
		log.Fatal(err)
	}
	defer broker.Close()

	// _, err = broker.Enqueue(
	// 	context.Background(),
	// 	"email:welcome",
	// 	exampleEmailPayload{UserID: "u-1001"},
	// 	asynqx.WithTaskQueue("critical"),
	// 	asynqx.WithTaskTimeout(30*time.Second),
	// )
	// if err != nil {
	// 	log.Fatal(err)
	// }
}

func ExampleHandle() {
	worker, err := asynqx.NewWorker(asynqx.WithRedisAddr("127.0.0.1:6379"))
	if err != nil {
		log.Fatal(err)
	}
	defer worker.Shutdown(context.Background())

	err = asynqx.Handle(worker, "email:welcome", func(ctx context.Context, payload exampleEmailPayload) error {
		_ = ctx
		_ = payload

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleScheduler_Register() {
	scheduler, err := asynqx.NewScheduler(
		asynqx.WithRedisAddr("127.0.0.1:6379"),
		asynqx.WithLocation("Asia/Shanghai"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer scheduler.Shutdown(context.Background())

	_, err = scheduler.Register(
		context.Background(),
		"@every 1m",
		"email:welcome",
		exampleEmailPayload{UserID: "u-1001"},
		asynqx.WithTaskQueue("default"),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = time.Second
}
