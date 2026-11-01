// sdk/logger.go
package sdk

// Logger adalah interface untuk logging
// SEMUA module WAJIB menggunakan Logger via DI
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	With(fields map[string]interface{}) Logger
	WithField(key string, value interface{}) Logger
}
