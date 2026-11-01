// auth/user_store_owner_lookup.go
package auth

import (
	"encoding/json"
	"errors"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// HasOwner — dibutuhkan Bootstrap Owner endpoint untuk idempotency
// (Section: tenant sudah punya owner -> conflict, jangan overwrite
// diam-diam). File terpisah, pola konsisten dengan
// user_store_admin.go / user_store_lookup.go sebelumnya.
// ============================================================

const ownerRoleID = "owner"

func (s *InMemoryUserStore) HasOwner(tenantID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, u := range s.users {
		if u.TenantID == tenantID && u.RoleID == ownerRoleID {
			return true
		}
	}
	return false
}

func (s *BoltUserStore) HasOwner(tenantID string) bool {
	found := false

	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var user User
			if err := json.Unmarshal(v, &user); err != nil {
				continue
			}
			if user.TenantID == tenantID && user.RoleID == ownerRoleID {
				found = true
				return nil
			}
		}
		return nil
	})

	return found
}

// ErrOwnerNotFound dikembalikan kalau tenant tidak punya user dengan
// role owner. Dipisah dari ErrUserNotFound karena ini soal "tenant
// tidak punya owner", bukan soal "user ID spesifik tidak ada".
var ErrOwnerNotFound = errors.New("owner_not_found")

// OwnerEmail mengembalikan email user dengan role owner untuk
// tenantID. Dipakai notify-worker untuk resolve tujuan notifikasi
// customer (quota warning, chain broken) per job — bukan sekali di
// startup, karena satu worker run bisa memproses job dari banyak
// tenant berbeda.
func (s *BoltUserStore) OwnerEmail(tenantID string) (string, error) {
	users, err := s.ListAllForTenant(tenantID)
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if u.RoleID == ownerRoleID && u.DeletedAt == nil {
			return u.Email, nil
		}
	}
	return "", ErrOwnerNotFound
}
