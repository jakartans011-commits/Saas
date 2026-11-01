package audit

import "time"

// ============================================================
// AUDIT EVENT — mencatat siapa melakukan apa, kapan, dan hasilnya
// untuk kepentingan forensics, compliance, dan accountability.
// ============================================================

type EventType string

const (
	// EventTypeAPIRequest dicatat otomatis oleh middleware.AuditLogger
	// untuk setiap request yang lolos RequireAuth.
	EventTypeAPIRequest EventType = "api_request"

	// EventTypeLogin dicatat saat user login
	EventTypeLogin EventType = "login"

	// EventTypeUserCreated dicatat saat user dibuat
	EventTypeUserCreated EventType = "user_created"
)

// Outcome represents the result of an audit event
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeDenied  Outcome = "denied"
)

// AuditEvent — satu baris kejadian audit.
type AuditEvent struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	UserID    string            `json:"user_id"`  // siapa yang melakukan
	Action    string            `json:"action"`   // apa yang dilakukan
	Resource  string            `json:"resource"` // resource apa
	Outcome   Outcome           `json:"outcome"`  // success/failure/denied
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Store — interface untuk audit event storage
type Store interface {
	Save(event *AuditEvent) error
	ListForTenant(tenantID string, limit int) ([]*AuditEvent, error)

	// CleanupForTenant menghapus/anonymize event sesuai RetentionPolicy
	// tenant. Ditambahkan di sini (bukan cuma jadi method konkret di
	// BoltAuditStore) supaya PostgresAuditStore bisa dipakai lewat
	// interface ini juga oleh audit_retention_handlers.go.
	CleanupForTenant(tenantID string, policy *RetentionPolicy) (deleted int, anonymized int, err error)

	// DeleteAllForTenant menghapus SELURUH audit event tenant tanpa
	// syarat retensi — dipakai compliance.PurgeTenant saat GDPR purge.
	DeleteAllForTenant(tenantID string) error
}
