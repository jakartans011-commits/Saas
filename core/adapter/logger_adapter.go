// core/adapter/logger_adapter.go
package adapter

import (
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"

	"trailseal/sdk"
)

// LoggerAdapter adalah adapter dari sdk.Logger ke slog.Logger
type LoggerAdapter struct {
	logger *slog.Logger
}

// LoggerConfig adalah konfigurasi untuk logger
type LoggerConfig struct {
	Level      string `json:"level"`
	LogFile    string `json:"log_file"`
	MaxSize    int    `json:"max_size"`
	MaxBackups int    `json:"max_backups"`
	MaxAge     int    `json:"max_age"`
	Compress   bool   `json:"compress"`
	Console    bool   `json:"console"`
}

// DefaultLoggerConfig mengembalikan konfigurasi default
func DefaultLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Level:      "info",
		LogFile:    "logs/agent.log",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
		Console:    true,
	}
}

// NewLoggerAdapter membuat adapter baru dengan config
func NewLoggerAdapter(config *LoggerConfig) sdk.Logger {
	if config == nil {
		config = DefaultLoggerConfig()
	}

	var level slog.Level
	switch config.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var writer io.Writer
	if config.LogFile != "" {
		logWriter := &lumberjack.Logger{
			Filename:   config.LogFile,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.Compress,
		}
		if config.Console {
			writer = io.MultiWriter(os.Stdout, logWriter)
		} else {
			writer = logWriter
		}
	} else {
		writer = os.Stdout
	}

	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	})

	logger := slog.New(handler)

	return &LoggerAdapter{
		logger: logger,
	}
}

func (a *LoggerAdapter) Debug(msg string, args ...interface{}) {
	if a.logger != nil {
		a.logger.Debug(msg, args...)
	}
}

func (a *LoggerAdapter) Info(msg string, args ...interface{}) {
	if a.logger != nil {
		a.logger.Info(msg, args...)
	}
}

func (a *LoggerAdapter) Warn(msg string, args ...interface{}) {
	if a.logger != nil {
		a.logger.Warn(msg, args...)
	}
}

func (a *LoggerAdapter) Error(msg string, args ...interface{}) {
	if a.logger != nil {
		a.logger.Error(msg, args...)
	}
}

func (a *LoggerAdapter) With(fields map[string]interface{}) sdk.Logger {
	if a.logger == nil {
		return a
	}
	attrs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		attrs = append(attrs, k, v)
	}
	newLogger := a.logger.With(attrs...)
	return &LoggerAdapter{logger: newLogger}
}

func (a *LoggerAdapter) WithField(key string, value interface{}) sdk.Logger {
	if a.logger == nil {
		return a
	}
	newLogger := a.logger.With(key, value)
	return &LoggerAdapter{logger: newLogger}
}