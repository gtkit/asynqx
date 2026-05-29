package asynqx

import "github.com/hibiken/asynq"

// Inspector 是 asynq 队列检查器，用于运维检查、排障和统计。
type Inspector = asynq.Inspector

type inspectorClientFactory func(Config) (*Inspector, error)

// NewInspector 基于共享配置创建 asynq 队列检查器。
// 调用成功后，调用方应调用 Close 释放底层资源。
func NewInspector(opts ...ConfigOption) (*Inspector, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	return newInspector(cfg, defaultInspectorClientFactory)
}

func newInspector(cfg Config, factory inspectorClientFactory) (*Inspector, error) {
	if factory == nil {
		return nil, invalidConfigurationError("inspector.client_factory", "must not be nil")
	}

	inspector, err := factory(cfg.clone())
	if err != nil {
		return nil, err
	}

	if inspector == nil {
		return nil, invalidConfigurationError("inspector.client", "must not be nil")
	}

	return inspector, nil
}

var defaultInspectorClientFactory inspectorClientFactory = func(cfg Config) (*Inspector, error) {
	return asynq.NewInspector(cfg.Redis), nil
}
