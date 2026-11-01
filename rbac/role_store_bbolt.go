// rbac/role_store_bbolt.go
package rbac

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// BoltRoleStore — persistent, menggantikan InMemoryRoleStore.
// Memenuhi RoleAdminStore interface — bisa dipasang di
// tenant/provisioning.go tanpa mengubah kode di sana.
//
// Key format SAMA dengan InMemoryRoleStore: "<tenantID>:<roleID>",
// supaya makna "role tidak ditemukan DI TENANT INI" (bukan "role
// tidak pernah ada sama sekali") tetap terjaga — ini yang mencegah
// role_id collision bug (lihat rbac/role.go GetRole).
// ============================================================

// DeleteRole menghapus satu role tenant. Tidak error bila role tidak ada.
// Sengaja BUKAN bagian RoleStore/RoleAdminStore (memecahkan mock); dipakai lewat
// type assertion oleh billing.
func (s *BoltRoleStore) DeleteRole(tenantID, roleID string) error {
	key := []byte(roleKey(tenantID, roleID))
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(rolesBucket).Delete(key)
	})
}

var rolesBucket = []byte("roles")

type BoltRoleStore struct {
	db *bolt.DB
}

func NewBoltRoleStore(db *bolt.DB) (*BoltRoleStore, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(rolesBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init roles bucket: %w", err)
	}
	return &BoltRoleStore{db: db}, nil
}

func (s *BoltRoleStore) PutRole(role *Role) error {
	if role.TenantID == "" {
		return fmt.Errorf("role %s: tenant_id is required, cannot store global role", role.ID)
	}
	data, err := json.Marshal(role)
	if err != nil {
		return err
	}
	key := []byte(roleKey(role.TenantID, role.ID))
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rolesBucket)
		return b.Put(key, data)
	})
}

func (s *BoltRoleStore) GetRole(tenantID, roleID string) (*Role, error) {
	var role Role
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rolesBucket)
		data := b.Get([]byte(roleKey(tenantID, roleID)))
		if data == nil {
			return nil // found tetap false — TIDAK fallback ke tenant lain
		}
		found = true
		return json.Unmarshal(data, &role)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf(
			"role_resolution_failed: role %s not found for tenant %s",
			roleID, tenantID,
		)
	}
	return &role, nil
}

func (s *BoltRoleStore) DeleteAllForTenant(tenantID string) int {
	deleted := 0

	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rolesBucket)
		c := b.Cursor()

		// Sama seperti BoltUserStore: kumpulkan key dulu, hapus setelah
		// iterasi selesai — hapus-sambil-iterate di bbolt cursor adalah
		// undefined behavior.
		var keysToDelete [][]byte
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var role Role
			if err := json.Unmarshal(v, &role); err != nil {
				continue
			}
			if role.TenantID == tenantID {
				keyCopy := make([]byte, len(k))
				copy(keyCopy, k)
				keysToDelete = append(keysToDelete, keyCopy)
			}
		}

		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
			deleted++
		}
		return nil
	})

	return deleted
}

func (s *BoltRoleStore) CountForTenant(tenantID string) int {
	count := 0

	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rolesBucket)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var role Role
			if err := json.Unmarshal(v, &role); err != nil {
				continue
			}
			if role.TenantID == tenantID {
				count++
			}
		}
		return nil
	})

	return count
}
