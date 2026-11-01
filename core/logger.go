// core/logger.go
package core

import (
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

// ============================================================================
// SDK LOGGER INTERFACE (DARI logger_impl.go)
// ============================================================================

// Logger adalah interface untuk logging (SDK compatible)
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	With(fields map[string]interface{}) Logger
	WithField(key string, value interface{}) Logger
}

// ============================================================================
// LOG CONFIG (SUDAH ADA)
// ============================================================================

type LogConfig struct {
	Level      string `json:"level"`        // debug, info, warn, error
	LogFile    string `json:"log_file"`     // Path file log
	MaxSize    int    `json:"max_size"`     // MB
	MaxBackups int    `json:"max_backups"`  // Jumlah file backup
	MaxAge     int    `json:"max_age"`      // Hari
	Compress   bool   `json:"compress"`     // Gzip compress
	Console    bool   `json:"console"`      // Output ke console juga
}

// DefaultLogConfig - konfigurasi default
func DefaultLogConfig() *LogConfig {
	return &LogConfig{
		Level:      "info",
		LogFile:    "logs/agent.log",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
		Console:    true,
	}
}

// ============================================================================
// LOGGER IMPLEMENTASI (GABUNGAN)
// ============================================================================

// loggerImpl adalah implementasi Logger interface
type loggerImpl struct {
	logger *slog.Logger
}

// NewLogger - SATU SATUNYA! (yang ini dipertahankan, dengan config)
func NewLogger(config *LogConfig) (*slog.Logger, error) {
	if config == nil {
		config = DefaultLogConfig()
	}

	// Parse log level
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

	// Buat log rotation writer
	logWriter := &lumberjack.Logger{
		Filename:   config.LogFile,
		MaxSize:    config.MaxSize,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAge,
		Compress:   config.Compress,
	}

	// Gabungkan dengan console jika diperlukan
	var writer io.Writer
	if config.Console {
		writer = io.MultiWriter(os.Stdout, logWriter)
	} else {
		writer = logWriter
	}

	// Buat handler
	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	})

	logger := slog.New(handler)

	// Set sebagai default logger
	slog.SetDefault(logger)

	logger.Info("Logger initialized",
		"level", config.Level,
		"log_file", config.LogFile,
		"max_size_mb", config.MaxSize,
		"max_backups", config.MaxBackups,
		"max_age_days", config.MaxAge,
		"compress", config.Compress,
	)

	return logger, nil
}

// ============================================================================
// SDK LOGGER IMPLEMENTASI (DARI logger_impl.go)
// ============================================================================

// NewSDKLogger membuat Logger interface untuk SDK
func NewSDKLogger() Logger {
	return &loggerImpl{
		logger: slog.Default(),
	}
}

// NewSDKLoggerWithConfig membuat Logger interface dengan config
func NewSDKLoggerWithConfig(config *LogConfig) (Logger, error) {
	logger, err := NewLogger(config)
	if err != nil {
		return nil, err
	}
	return &loggerImpl{
		logger: logger,
	}, nil
}

func (l *loggerImpl) Debug(msg string, args ...interface{}) {
	l.logger.Debug(msg, args...)
}

func (l *loggerImpl) Info(msg string, args ...interface{}) {
	l.logger.Info(msg, args...)
}

func (l *loggerImpl) Warn(msg string, args ...interface{}) {
	l.logger.Warn(msg, args...)
}

func (l *loggerImpl) Error(msg string, args ...interface{}) {
	l.logger.Error(msg, args...)
}

func (l *loggerImpl) With(fields map[string]interface{}) Logger {
	// Buat slog logger dengan fields
	attrs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		attrs = append(attrs, k, v)
	}
	newLogger := l.logger.With(attrs...)
	return &loggerImpl{
		logger: newLogger,
	}
}

func (l *loggerImpl) WithField(key string, value interface{}) Logger {
	newLogger := l.logger.With(key, value)
	return &loggerImpl{
		logger: newLogger,
	}
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// GetLogConfigFromEnv - load config dari environment
func GetLogConfigFromEnv() *LogConfig {
	config := DefaultLogConfig()

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Level = level
	}

	if logFile := os.Getenv("LOG_FILE"); logFile != "" {
		config.LogFile = logFile
	}

	return config
}