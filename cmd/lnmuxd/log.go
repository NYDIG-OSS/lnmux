package main

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var log *zap.SugaredLogger

// initLogger constructs a new SugaredLogger.
// By default, the level is 'debug', the format is 'console' and it doesn't display the caller.
func initLogger(level string, format string, withCaller bool) error {
	var l zapcore.Level
	switch strings.TrimSpace(strings.ToLower(level)) {
	case "", "d", "debug":
		l = zap.DebugLevel
	case "info", "i":
		l = zap.InfoLevel
	case "w", "warn", "warning":
		l = zap.WarnLevel
	case "e", "error", "errors":
		l = zap.ErrorLevel
	case "dp", "dpanic":
		l = zap.DPanicLevel
	case "p", "panic", "panics":
		l = zap.PanicLevel
	case "f", "fatal", "fatals":
		l = zap.FatalLevel
	default:
		return fmt.Errorf("unknown level %s", level)
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.TimeKey = "time"

	if !withCaller {
		encoderConfig.EncodeCaller = nil
	}

	// format is 'console' by default
	if format == "" {
		// Add color to the level
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		format = "console"
	}

	config := zap.Config{
		Level:       zap.NewAtomicLevelAt(l),
		Development: false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         format,
		EncoderConfig:    encoderConfig,
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}

	lo, err := config.Build()
	if err != nil {
		return err
	}

	log = lo.Sugar()

	return nil
}
