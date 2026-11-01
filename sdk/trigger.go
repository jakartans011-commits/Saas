// sdk/trigger.go
package sdk

import "time"

// Trigger adalah mekanisme untuk memicu executable
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Trigger interface {
	// Type mengembalikan jenis trigger ("interval", "cron", "event", "manual")
	Type() string

	// Next mengembalikan waktu berikutnya trigger akan aktif
	Next(now time.Time) *time.Time

	// Match mengecek apakah trigger aktif pada waktu tertentu
	Match(now time.Time) bool

	// Name mengembalikan nama trigger
	Name() string

	// Enabled mengembalikan status trigger
	Enabled() bool

	// SetEnabled mengaktifkan/menonaktifkan trigger
	SetEnabled(enabled bool)
}

// TriggerDeclaration adalah deklarasi trigger di manifest (data, bukan interface)
type TriggerDeclaration struct {
	Type    string                 `json:"type"`              // "interval", "cron", "event", "manual"
	Config  map[string]interface{} `json:"config"`            // {"interval": "30s"}, {"cron": "0 * * * *"}
	Enabled bool                   `json:"enabled"`           // default: true
	Timeout string                 `json:"timeout,omitempty"` // "30s"
	Retry   *RetryPolicy           `json:"retry,omitempty"`
}

// RetryPolicy adalah kebijakan retry untuk executable
type RetryPolicy struct {
	MaxAttempts int    `json:"max_attempts"` // default: 3
	Backoff     string `json:"backoff"`      // "linear", "exponential" (default: "exponential")
	Delay       string `json:"delay"`        // "1s", "5s" (default: "1s")
}

// TriggerFactory adalah factory untuk membuat trigger dari deklarasi
type TriggerFactory func(config map[string]interface{}) (Trigger, error)
