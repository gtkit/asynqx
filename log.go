package asynqx

import (
	"log"

	zap "github.com/gtkit/logger"
	"github.com/hibiken/asynq"
)

// Logger is the interface for logging.
var logger Logger

// SetLogger sets the logger for asynq.
type Logger interface {
	asynq.Logger
	Debugf(template string, args ...any)
	Infof(format string, args ...any)
	Errorf(template string, args ...any)
}

func initLogger(s *Server) {
	log.Println("asynqx: No logger set, using default logger [zap-logger].")
	if zap.Zlog() == nil {
		zap.NewZap(zap.WithConsole(true))
	}
	logger = zap.Sugar()
	s.asynqConfig.Logger, s.schedulerOpts.Logger = logger, logger
}
