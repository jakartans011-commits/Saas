// sdk/config.go
package sdk

import "time"

// Config adalah interface untuk konfigurasi module
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Config interface {
	Get(key string) interface{}
	GetString(key string) string
	GetInt(key string) int
	GetBool(key string) bool
	GetDuration(key string) time.Duration
	GetStringSlice(key string) []string
	Set(key string, value interface{})
	Unset(key string)
	All() map[string]interface{}
}
