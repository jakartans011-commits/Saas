// usage/record_test.go
package usage

import (
	"errors"
	"testing"
	"time"
)

// fakeEventStore — implementasi EventStore minimal buat testing,
// tanpa bolt/DB sama sekali.
type fakeEventStore struct {
	saved   []*UsageEvent
	saveErr error
}

func (f *fakeEventStore) Save(event *UsageEvent) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, event)
	return nil
}

func (f *fakeEventStore) ListForTenant(tenantID string, limit int) ([]*UsageEvent, error) {
	return f.saved, nil
}

func (f *fakeEventStore) CountBillableSinceForTenant(tenantID string, since time.Time) (int, error) {
	return 0, nil
}

func TestRecordEvent(t *testing.T) {
	t.Run("saves_event_with_generated_id_and_timestamp", func(t *testing.T) {
		store := &fakeEventStore{}
		meta := map[string]string{"path": "/api/x"}

		err := RecordEvent(store, "tenant-1", EventTypeAPIRequest, meta)
		if err != nil {
			t.Fatalf("RecordEvent failed: %v", err)
		}

		if len(store.saved) != 1 {
			t.Fatalf("expected 1 saved event, got %d", len(store.saved))
		}
		got := store.saved[0]
		if got.TenantID != "tenant-1" {
			t.Fatalf("expected TenantID tenant-1, got %s", got.TenantID)
		}
		if got.EventType != EventTypeAPIRequest {
			t.Fatalf("expected EventTypeAPIRequest, got %s", got.EventType)
		}
		if got.ID == "" {
			t.Fatal("expected auto-generated ID, got empty string")
		}
		if got.Timestamp.IsZero() {
			t.Fatal("expected Timestamp to be set")
		}
		if got.Metadata["path"] != "/api/x" {
			t.Fatalf("expected metadata to be passed through, got %+v", got.Metadata)
		}
	})

	t.Run("empty_tenant_id_is_silently_skipped", func(t *testing.T) {
		store := &fakeEventStore{}

		err := RecordEvent(store, "", EventTypeAPIRequest, nil)
		if err != nil {
			t.Fatalf("expected nil error for empty tenant_id, got %v", err)
		}
		if len(store.saved) != 0 {
			t.Fatalf("expected no event saved for empty tenant_id, got %d", len(store.saved))
		}
	})

	t.Run("nil_store_does_not_panic", func(t *testing.T) {
		err := RecordEvent(nil, "tenant-1", EventTypeAPIRequest, nil)
		if err != nil {
			t.Fatalf("expected nil error for nil store, got %v", err)
		}
	})

	t.Run("propagates_save_error", func(t *testing.T) {
		wantErr := errors.New("disk full")
		store := &fakeEventStore{saveErr: wantErr}

		err := RecordEvent(store, "tenant-1", EventTypeAPIRequest, nil)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected save error to propagate, got %v", err)
		}
	})

	t.Run("two_calls_generate_different_ids", func(t *testing.T) {
		store := &fakeEventStore{}

		if err := RecordEvent(store, "tenant-1", EventTypeAPIRequest, nil); err != nil {
			t.Fatalf("first RecordEvent failed: %v", err)
		}
		if err := RecordEvent(store, "tenant-1", EventTypeAPIRequest, nil); err != nil {
			t.Fatalf("second RecordEvent failed: %v", err)
		}

		if store.saved[0].ID == store.saved[1].ID {
			t.Fatal("expected unique IDs across calls, got duplicate")
		}
	})
}
