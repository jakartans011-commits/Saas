package audit

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// TEST HELPERS
// ============================================================

func openTestAuditStoreDB(t *testing.T) (*bolt.DB, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit_store_test.db")
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	return db, func() { db.Close() }
}

// ============================================================
// TEST AUDIT STORE - SAVE SUCCESS
// ============================================================

func TestAuditStore_Save_Success(t *testing.T) {
	db, cleanup := openTestAuditStoreDB(t)
	defer cleanup()

	store, err := NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("NewBoltAuditStore failed: %v", err)
	}

	event := &AuditEvent{
		ID:        "evt-1",
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Action:    "test-action",
		Resource:  "/api/test",
		Outcome:   OutcomeSuccess,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"key": "value",
		},
	}

	err = store.Save(event)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verifikasi dengan ListForTenant
	events, err := store.ListForTenant("tenant-1", 10)
	if err != nil {
		t.Fatalf("ListForTenant failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].ID != "evt-1" {
		t.Errorf("expected ID evt-1, got %s", events[0].ID)
	}
	if events[0].TenantID != "tenant-1" {
		t.Errorf("expected TenantID tenant-1, got %s", events[0].TenantID)
	}
	if events[0].Action != "test-action" {
		t.Errorf("expected Action test-action, got %s", events[0].Action)
	}
	if events[0].Outcome != OutcomeSuccess {
		t.Errorf("expected Outcome success, got %s", events[0].Outcome)
	}
}

// ============================================================
// TEST AUDIT STORE - SAVE EMPTY TENANT ID
// ============================================================

func TestAuditStore_Save_EmptyTenantID(t *testing.T) {
	db, cleanup := openTestAuditStoreDB(t)
	defer cleanup()

	store, err := NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("NewBoltAuditStore failed: %v", err)
	}

	event := &AuditEvent{
		ID:        "evt-1",
		UserID:    "user-1",
		Action:    "test-action",
		Resource:  "/api/test",
		Outcome:   OutcomeSuccess,
		Timestamp: time.Now(),
	}

	err = store.Save(event)
	if err == nil {
		t.Error("expected error for empty tenant_id")
	}
}

// ============================================================
// TEST AUDIT STORE - SAVE EMPTY ID
// ============================================================

func TestAuditStore_Save_EmptyID(t *testing.T) {
	db, cleanup := openTestAuditStoreDB(t)
	defer cleanup()

	store, err := NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("NewBoltAuditStore failed: %v", err)
	}

	event := &AuditEvent{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Action:    "test-action",
		Resource:  "/api/test",
		Outcome:   OutcomeSuccess,
		Timestamp: time.Now(),
	}

	err = store.Save(event)
	if err == nil {
		t.Error("expected error for empty ID")
	}
}

// ============================================================
// TEST AUDIT STORE - LIST EMPTY TENANT
// ============================================================

func TestAuditStore_ListForTenant_Empty(t *testing.T) {
	db, cleanup := openTestAuditStoreDB(t)
	defer cleanup()

	store, err := NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("NewBoltAuditStore failed: %v", err)
	}

	events, err := store.ListForTenant("non-existent-tenant", 10)
	if err != nil {
		t.Fatalf("ListForTenant failed: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

// ============================================================
// TEST AUDIT STORE - LIST DEFAULT LIMIT
// ============================================================

func TestAuditStore_ListForTenant_DefaultLimit(t *testing.T) {
	db, cleanup := openTestAuditStoreDB(t)
	defer cleanup()

	store, err := NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("NewBoltAuditStore failed: %v", err)
	}

	// Save 10 events
	for i := 0; i < 10; i++ {
		event := &AuditEvent{
			ID:        "evt-" + string(rune('a'+i)),
			TenantID:  "tenant-1",
			UserID:    "user-1",
			Action:    "test-action",
			Resource:  "/api/test",
			Outcome:   OutcomeSuccess,
			Timestamp: time.Now(),
		}
		if err := store.Save(event); err != nil {
			t.Fatalf("Save event %d failed: %v", i, err)
		}
	}

	// List with default limit (0 = 50)
	events, err := store.ListForTenant("tenant-1", 0)
	if err != nil {
		t.Fatalf("ListForTenant failed: %v", err)
	}

	// Should return all 10 events (since 10 < 50)
	if len(events) != 10 {
		t.Errorf("expected 10 events, got %d", len(events))
	}
}

// ============================================================
// TEST AUDIT STORE - LIST WITH LIMIT
// ============================================================

func TestAuditStore_ListForTenant_WithLimit(t *testing.T) {
	db, cleanup := openTestAuditStoreDB(t)
	defer cleanup()

	store, err := NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("NewBoltAuditStore failed: %v", err)
	}

	// Save 10 events
	for i := 0; i < 10; i++ {
		event := &AuditEvent{
			ID:        "evt-" + string(rune('a'+i)),
			TenantID:  "tenant-1",
			UserID:    "user-1",
			Action:    "test-action",
			Resource:  "/api/test",
			Outcome:   OutcomeSuccess,
			Timestamp: time.Now(),
		}
		if err := store.Save(event); err != nil {
			t.Fatalf("Save event %d failed: %v", i, err)
		}
	}

	// List with limit 5
	events, err := store.ListForTenant("tenant-1", 5)
	if err != nil {
		t.Fatalf("ListForTenant failed: %v", err)
	}

	if len(events) != 5 {
		t.Errorf("expected 5 events, got %d", len(events))
	}
}

// ============================================================
// TEST AUDIT STORE - MULTIPLE TENANTS ISOLATION
// ============================================================

func TestAuditStore_ListForTenant_Isolation(t *testing.T) {
	db, cleanup := openTestAuditStoreDB(t)
	defer cleanup()

	store, err := NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("NewBoltAuditStore failed: %v", err)
	}

	// Save events for tenant-1
	for i := 0; i < 3; i++ {
		event := &AuditEvent{
			ID:        "evt-1-" + string(rune('a'+i)),
			TenantID:  "tenant-1",
			UserID:    "user-1",
			Action:    "test-action",
			Resource:  "/api/test",
			Outcome:   OutcomeSuccess,
			Timestamp: time.Now(),
		}
		if err := store.Save(event); err != nil {
			t.Fatalf("Save event tenant-1 failed: %v", err)
		}
	}

	// Save events for tenant-2
	for i := 0; i < 2; i++ {
		event := &AuditEvent{
			ID:        "evt-2-" + string(rune('a'+i)),
			TenantID:  "tenant-2",
			UserID:    "user-2",
			Action:    "test-action",
			Resource:  "/api/test",
			Outcome:   OutcomeSuccess,
			Timestamp: time.Now(),
		}
		if err := store.Save(event); err != nil {
			t.Fatalf("Save event tenant-2 failed: %v", err)
		}
	}

	// List tenant-1
	events1, err := store.ListForTenant("tenant-1", 10)
	if err != nil {
		t.Fatalf("ListForTenant tenant-1 failed: %v", err)
	}
	if len(events1) != 3 {
		t.Errorf("expected 3 events for tenant-1, got %d", len(events1))
	}

	// List tenant-2
	events2, err := store.ListForTenant("tenant-2", 10)
	if err != nil {
		t.Fatalf("ListForTenant tenant-2 failed: %v", err)
	}
	if len(events2) != 2 {
		t.Errorf("expected 2 events for tenant-2, got %d", len(events2))
	}

	// Verifikasi tidak ada cross-tenant
	for _, e := range events1 {
		if e.TenantID != "tenant-1" {
			t.Errorf("tenant-1 list contained event with tenant_id %s", e.TenantID)
		}
	}
	for _, e := range events2 {
		if e.TenantID != "tenant-2" {
			t.Errorf("tenant-2 list contained event with tenant_id %s", e.TenantID)
		}
	}
}