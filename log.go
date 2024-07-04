package asynqx

import (
	"log"

	zap "github.com/gtkit/logger"
)

// Logger is the interface for logging.
var logger Logger

// SetLogger sets the logger for asynq.
type Logger interface {
	Debug(args ...any)
	Info(args ...any)
	Warn(args ...any)
	Error(args ...any)
	Fatal(args ...any)
	Debugf(template string, args ...any)
	Infof(template string, args ...any)
	Warnf(template string, args ...any)
	Errorf(template string, args ...any)
	Fatalf(template string, args ...any)
}

func setLogger() {
	log.Println("No logger set, using default logger [zap-logger].")
	if zap.Zlog() == nil {
		zap.NewZap(
			zap.WithConsole(true),
		)
		logger = zap.Sugar()
		return
	}
	logger = zap.Sugar()
}
