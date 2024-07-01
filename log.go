package asynqx

import (
	"github.com/gtkit/logger"
)

// Initialize the logger for asynq.
func init() { //nolint:gochecknoinits // check log
	if logger.Zlog() == nil {
		logger.NewZap(
			logger.WithConsole(true),
		)
		logger.Info("asynq log init success")
	}
}
