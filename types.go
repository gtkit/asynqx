package asynqx

import (
	"context"
)

type MsgPayload any

type Binder func() any

type MsgHandler func(string, MsgPayload) error

type MsgHandlerWithCtx func(context.Context, string, MsgPayload) error

type HandlerData struct {
	Handler MsgHandler
	Binder  Binder
}
type MsgHandlerMap map[string]HandlerData

type TaskHandler[T any] func(string, *T) error

type TaskHandlerWithCtx[T any] func(context.Context, string, *T) error
