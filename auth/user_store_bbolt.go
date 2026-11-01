package auth

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// BoltUserStore — persistent, menggantikan InMemoryUserStore.
// Memenuhi UserAdminStore interface yang sama, jadi bisa dipasang
// di provisioning.go / middleware tanpa mengubah kode di sana.
// ============================================================

var usersBucket = []byte("users")

type BoltUserStore struct {
	db *bolt.DB
}

// NewBoltUserStore membuka/membuat bucket "users" di db yang sudah
// ada. db WAJIB sudah dibuka oleh caller (mis. di cmd/server/main.go)
// — package ini tidak bertanggung jawab atas lifecycle koneksi DB.
func NewBoltUserStore(db *bolt.DB) (*BoltUserStore, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(usersBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init users bucket: %w", err)
	}
	return &BoltUserStore{db: db}, nil
}

func (s *BoltUserStore) Put(user *User) error {
	if user.TenantID == "" {
		return fmt.Errorf("user %s: tenant_id is required", user.ID)
	}
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		return b.Put([]byte(user.ID), data)
	})
}

func (s *BoltUserStore) GetByID(userID string) (*User, error) {
	var user User
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		data := b.Get([]byte(userID))
		if data == nil {
			return nil // found tetap false
		}
		found = true
		return json.Unmarshal(data, &user)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrUserNotFound
	}
	// Soft-deleted users are not accessible
	if user.DeletedAt != nil {
		return nil, ErrUserNotFound
	}
	return &user, nil
}

// GetByIDIncludingDeleted returns user even if soft-deleted
func (s *BoltUserStore) GetByIDIncludingDeleted(userID string) (*User, error) {
	var user User
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		data := b.Get([]byte(userID))
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &user)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrUserNotFound
	}
	return &user, nil
}

// ListAllForTenant returns all users for a tenant (including soft-deleted)
func (s *BoltUserStore) ListAllForTenant(tenantID string) ([]*User, error) {
	users := make([]*User, 0)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var user User
			if err := json.Unmarshal(v, &user); err != nil {
				continue
			}
			if user.TenantID == tenantID {
				users = append(users, &user)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return users, nil
}

// ListDeletedForTenant returns only soft-deleted users for a tenant
func (s *BoltUserStore) ListDeletedForTenant(tenantID string) ([]*User, error) {
	users := make([]*User, 0)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var user User
			if err := json.Unmarshal(v, &user); err != nil {
				continue
			}
			if user.TenantID == tenantID && user.DeletedAt != nil {
				users = append(users, &user)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (s *BoltUserStore) DeleteAllForTenant(tenantID string) int {
	deleted := 0

	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		c := b.Cursor()

		var keysToDelete [][]byte
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var user User
			if err := json.Unmarshal(v, &user); err != nil {
				continue
			}
			if user.TenantID == tenantID {
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

func (s *BoltUserStore) CountForTenant(tenantID string) int {
	count := 0

	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var user User
			if err := json.Unmarshal(v, &user); err != nil {
				continue
			}
			if user.TenantID == tenantID && user.DeletedAt == nil {
				count++
			}
		}
		return nil
	})

	return count
}
