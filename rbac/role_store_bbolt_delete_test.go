package rbac

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestBoltRoleStore_DeleteRole(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "roles.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := NewBoltRoleStore(db)
	if err != nil {
		t.Fatal(err)
	}

	_ = store.PutRole(&Role{ID: "admin", TenantID: "t1"})
	_ = store.PutRole(&Role{ID: "admin", TenantID: "t2"})

	if err := store.DeleteRole("t1", "admin"); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if _, err := store.GetRole("t1", "admin"); err == nil {
		t.Error("role t1/admin seharusnya sudah terhapus")
	}
	if _, err := store.GetRole("t2", "admin"); err != nil {
		t.Errorf("role tenant lain tidak boleh ikut terhapus: %v", err)
	}
	if err := store.DeleteRole("t1", "tidak-ada"); err != nil {
		t.Errorf("hapus role yang tidak ada harus nil, got %v", err)
	}
}
