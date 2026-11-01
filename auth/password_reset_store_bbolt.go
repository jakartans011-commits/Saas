// auth/password_reset_store_bbolt.go
package auth

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var passwordResetBucket = []byte("password_reset_tokens")

type BoltPasswordResetStore struct {
	db *bolt.DB
}

func NewBoltPasswordResetStore(db *bolt.DB) (*BoltPasswordResetStore, error) {
	if db == nil {
		return nil, fmt.Errorf("password reset store: nil database")
	}
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(passwordResetBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init password_reset_tokens bucket: %w", err)
	}
	return &BoltPasswordResetStore{db: db}, nil
}

func (s *BoltPasswordResetStore) Save(record *PasswordResetRecord) error {
	if record == nil {
		return fmt.Errorf("password reset record is nil")
	}
	if record.ID == "" || record.UserID == "" || record.TokenHash == "" {
		return fmt.Errorf("password reset record missing required field")
	}
	if record.ExpiresAt.IsZero() {
		return fmt.Errorf("password reset record expiry is empty")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal password reset record: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(passwordResetBucket)
		if b == nil {
			return fmt.Errorf("password_reset_tokens bucket not found")
		}
		return b.Put([]byte(record.TokenHash), data)
	})
}

func (s *BoltPasswordResetStore) FindByHash(tokenHash string) (*PasswordResetRecord, error) {
	if tokenHash == "" {
		return nil, ErrResetTokenNotFound
	}
	var record PasswordResetRecord
	found := false
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(passwordResetBucket)
		if b == nil {
			return fmt.Errorf("password_reset_tokens bucket not found")
		}
		data := b.Get([]byte(tokenHash))
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &record)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrResetTokenNotFound
	}
	return &record, nil
}

func (s *BoltPasswordResetStore) MarkUsed(tokenHash string) (*PasswordResetRecord, error) {
	if tokenHash == "" {
		return nil, ErrResetTokenNotFound
	}
	var record PasswordResetRecord
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(passwordResetBucket)
		if b == nil {
			return fmt.Errorf("password_reset_tokens bucket not found")
		}
		data := b.Get([]byte(tokenHash))
		if data == nil {
			return ErrResetTokenNotFound
		}
		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("failed to decode password reset record: %w", err)
		}
		if record.UsedAt != nil {
			return ErrResetTokenUsed
		}
		if time.Now().After(record.ExpiresAt) {
			return ErrResetTokenExpired
		}
		now := time.Now()
		record.UsedAt = &now
		updated, err := json.Marshal(&record)
		if err != nil {
			return fmt.Errorf("failed to encode used password reset record: %w", err)
		}
		return b.Put([]byte(tokenHash), updated)
	})
	if err != nil {
		return nil, err
	}
	return &record, nil
}
