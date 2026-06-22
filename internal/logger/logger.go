// Package logger configures the process-wide zap logger used across GoTok.
package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New returns a zap.Logger tuned for local/terminal use: human-readable console
// encoding, Info level, and ISO8601 timestamps. Stacktraces are disabled to keep
// operational errors (e.g. a failed view increment) concise in the console.
func New() (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "console"
	cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	cfg.DisableStacktrace = true
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.EncodeDuration = zapcore.StringDurationEncoder
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	return cfg.Build()
}
