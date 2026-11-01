package tenant

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestListDeleted_ReturnsOnlySoftDeletedTenants(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "tenant-deleted.db"), 0600, nil)
	if err != nil {
		t.Fatalf("bolt.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore failed: %v", err)
	}

	deletedAt := time.Now().Add(-48 * time.Hour)
	for _, tn := range []*Tenant{
		{ID: "active-1"},
		{ID: "deleted-1", DeletedAt: &deletedAt},
		{ID: "active-2"},
	} {
		if err := store.Put(tn); err != nil {
			t.Fatalf("Put %s failed: %v", tn.ID, err)
		}
	}

	got, err := store.ListDeleted()
	if err != nil {
		t.Fatalf("ListDeleted failed: %v", err)
	}
	if len(got) != 1 || got[0].ID != "deleted-1" {
		t.Fatalf("expected only deleted-1, got %d tenants: %+v", len(got), got)
	}
}

func TestListDeleted_EmptyStore(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "tenant-empty.db"), 0600, nil)
	if err != nil {
		t.Fatalf("bolt.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore failed: %v", err)
	}

	got, err := store.ListDeleted()
	if err != nil {
		t.Fatalf("ListDeleted failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 tenants, got %d", len(got))
	}
}
