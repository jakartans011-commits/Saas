package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// AUDIT ANONYMIZATION — PII removal
// ============================================================

var anonymizedBucket = []byte("audit_events_anonymized")

// AnonymizedAuditEvent represents an audit event with PII removed
type AnonymizedAuditEvent struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	UserHash  string            `json:"user_hash"` // Hashed user ID
	Action    string            `json:"action"`
	Resource  string            `json:"resource"`
	Outcome   Outcome           `json:"outcome"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// hashUserID hashes a user ID for anonymization
func hashUserID(userID string) string {
	if userID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(hash[:])[:16]
}

// sanitizeMetadata removes PII from metadata
func sanitizeMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	// Fields that may contain PII and should be removed
	piiFields := map[string]bool{
		"email":      true,
		"user_agent": true,
		"ip":         true,
		"phone":      true,
		"address":    true,
		"name":       true,
		"full_name":  true,
		"user_id":    true,
	}

	sanitized := make(map[string]string)
	for k, v := range metadata {
		if !piiFields[k] {
			sanitized[k] = v
		}
	}
	return sanitized
}

// AnonymizeEvent converts a regular audit event to an anonymized one
func AnonymizeEvent(event *AuditEvent) *AnonymizedAuditEvent {
	return &AnonymizedAuditEvent{
		ID:        event.ID,
		TenantID:  event.TenantID,
		UserHash:  hashUserID(event.UserID),
		Action:    event.Action,
		Resource:  event.Resource,
		Outcome:   event.Outcome,
		Timestamp: event.Timestamp,
		Metadata:  sanitizeMetadata(event.Metadata),
	}
}

// StoreAnonymizedEvent stores an anonymized audit event
func StoreAnonymizedEvent(db *bolt.DB, event *AnonymizedAuditEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(anonymizedBucket)
		if err != nil {
			return err
		}
		// Use same key strategy as audit events
		key := []byte(event.TenantID + ":" + event.ID)
		return b.Put(key, data)
	})
}
