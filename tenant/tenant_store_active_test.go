package tenant

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestListActive_ExcludesSoftDeletedTenants(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "tenant-active.db"), 0600, nil)
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

	got, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive failed: %v", err)
	}

	ids := map[string]bool{}
	for _, tn := range got {
		ids[tn.ID] = true
	}
	if len(got) != 2 || !ids["active-1"] || !ids["active-2"] || ids["deleted-1"] {
		t.Fatalf("expected active-1 and active-2 only, got %+v", ids)
	}
}