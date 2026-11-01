package usage

import (
	"time"

	"github.com/google/uuid"
)

// RecordEvent adalah helper untuk mencatat usage event ke store.
// Mengenerate ID otomatis menggunakan UUID dan mengisi timestamp
// dengan waktu sekarang.
//
// Metadata bertipe map[string]string untuk memastikan serialisasi
// JSON konsisten dan tidak ada tipe data yang ambigu.
//
// Error handling: jika penyimpanan gagal, error dikembalikan ke caller.
// Caller (middleware) tidak boleh gagalkan request bisnis hanya karena
// metering gagal, tapi harus mencatat error untuk observability.
func RecordEvent(
	store EventStore,
	tenantID string,
	eventType EventType,
	metadata map[string]string,
) error {
	if tenantID == "" {
		// Tenant ID kosong tidak perlu dicatat
		return nil
	}

	if store == nil {
		return nil
	}

	event := &UsageEvent{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		EventType: eventType,
		Timestamp: time.Now(),
		Metadata:  metadata,
	}

	return store.Save(event)
}
