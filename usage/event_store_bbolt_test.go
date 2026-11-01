// usage/event_store_bbolt_test.go
package usage

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openTestEventBoltDB(t *testing.T, path string) *bolt.DB {
	t.Helper()
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open test bolt db: %v", err)
	}
	return db
}

func TestBoltEventStore_OrderingAndIsolation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage_test.db")
	db := openTestEventBoltDB(t, dbPath)
	defer db.Close()

	store, err := NewBoltEventStore(db)
	if err != nil {
		t.Fatalf("NewBoltEventStore failed: %v", err)
	}

	base := time.Now()

	events := []*UsageEvent{
		{ID: "evt-2", TenantID: "tenant-a", EventType: EventTypeAPIRequest, Timestamp: base.Add(2 * time.Second)},
		{ID: "evt-1", TenantID: "tenant-a", EventType: EventTypeAPIRequest, Timestamp: base.Add(1 * time.Second)},
		{ID: "evt-3", TenantID: "tenant-a", EventType: EventTypeAPIRequest, Timestamp: base.Add(3 * time.Second)},
		{ID: "evt-b1", TenantID: "tenant-b", EventType: EventTypeAPIRequest, Timestamp: base.Add(5 * time.Second)},
	}
	for _, e := range events {
		if err := store.Save(e); err != nil {
			t.Fatalf("Save failed for %s: %v", e.ID, err)
		}
	}

	got, err := store.ListForTenant("tenant-a", 10)
	if err != nil {
		t.Fatalf("ListForTenant failed: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("SECURITY/ISOLATION FAILURE: expected 3 events for tenant-a, got %d: %+v", len(got), got)
	}

	wantOrder := []string{"evt-3", "evt-2", "evt-1"}
	for i, w := range wantOrder {
		if got[i].ID != w {
			t.Fatalf("ORDERING FAILURE at index %d: expected %s, got %s (full: %+v)", i, w, got[i].ID, got)
		}
	}
	t.Log("OK: events returned newest-first, correctly ordered")

	for _, e := range got {
		if e.TenantID != "tenant-a" {
			t.Fatalf("LEAK DETECTED: event from tenant %s appeared in tenant-a query", e.TenantID)
		}
	}

	limited, err := store.ListForTenant("tenant-a", 2)
	if err != nil {
		t.Fatalf("ListForTenant with limit failed: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("LIMIT FAILURE: expected 2 events, got %d", len(limited))
	}
	if limited[0].ID != "evt-3" || limited[1].ID != "evt-2" {
		t.Fatalf("LIMIT+ORDER FAILURE: expected [evt-3, evt-2], got [%s, %s]", limited[0].ID, limited[1].ID)
	}
	t.Log("OK: limit respected, still newest-first")

	gotB, err := store.ListForTenant("tenant-b", 10)
	if err != nil {
		t.Fatalf("ListForTenant tenant-b failed: %v", err)
	}
	if len(gotB) != 1 || gotB[0].ID != "evt-b1" {
		t.Fatalf("ISOLATION FAILURE: expected exactly [evt-b1] for tenant-b, got %+v", gotB)
	}
	t.Log("OK: tenant-b isolated correctly")
}

func TestBoltEventStore_PrefixCollisionSafety(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage_prefix_test.db")
	db := openTestEventBoltDB(t, dbPath)
	defer db.Close()

	store, err := NewBoltEventStore(db)
	if err != nil {
		t.Fatalf("NewBoltEventStore failed: %v", err)
	}

	now := time.Now()
	if err := store.Save(&UsageEvent{ID: "e1", TenantID: "tenant-a", EventType: EventTypeAPIRequest, Timestamp: now}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.Save(&UsageEvent{ID: "e2", TenantID: "tenant-ab", EventType: EventTypeAPIRequest, Timestamp: now}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	gotA, err := store.ListForTenant("tenant-a", 10)
	if err != nil {
		t.Fatalf("ListForTenant failed: %v", err)
	}
	if len(gotA) != 1 || gotA[0].ID != "e1" {
		t.Fatalf("PREFIX COLLISION DETECTED: tenant-a query got %+v, expected only e1", gotA)
	}
	t.Log("OK: no prefix collision between tenant-a and tenant-ab")
}