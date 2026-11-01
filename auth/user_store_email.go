// auth/user_store_email.go
package auth

import (
	"encoding/json"
	"errors"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// FindByEmail — dibutuhkan endpoint login (Fase 2), karena login
// pakai email bukan user ID. Ditambahkan sebagai method BARU di
// file baru — TIDAK mengubah user.go / user_store_bbolt.go yang
// sudah lolos test, sesuai disiplin kerja yang sudah disepakati.
//
// UserEmailLookupStore adalah interface kecil terpisah (bukan
// menambah method ke UserStore/UserAdminStore yang sudah ada) —
// supaya kode lama yang bergantung pada UserStore/UserAdminStore
// tetap tidak berubah kontraknya.
// ============================================================

type UserEmailLookupStore interface {
	FindByEmail(email string) (*User, error)
}

// --- InMemoryUserStore ---

func (s *InMemoryUserStore) FindByEmail(email string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, user := range s.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, errors.New("user_not_found")
}

// --- BoltUserStore ---

func (s *BoltUserStore) FindByEmail(email string) (*User, error) {
	var found *User

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var user User
			if err := json.Unmarshal(v, &user); err != nil {
				continue // skip record korup, jangan crash seluruh scan
			}
			if user.Email == email {
				userCopy := user
				found = &userCopy
				return nil // ketemu, hentikan scan (Cursor tidak punya break eksplisit, jadi return)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, errors.New("user_not_found")
	}
	return found, nil
}
