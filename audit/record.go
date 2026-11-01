package audit

import (
	"time"

	"github.com/google/uuid"
)

// RecordEvent adalah helper untuk mencatat audit event ke store.
func RecordEvent(
	store Store,
	tenantID string,
	userID string,
	action string,
	resource string,
	outcome Outcome,
	metadata map[string]string,
) error {
	if tenantID == "" || userID == "" {
		return nil
	}

	if store == nil {
		return nil
	}

	event := &AuditEvent{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		UserID:    userID,
		Action:    action,
		Resource:  resource,
		Outcome:   outcome,
		Timestamp: time.Now(),
		Metadata:  metadata,
	}

	return store.Save(event)
}
