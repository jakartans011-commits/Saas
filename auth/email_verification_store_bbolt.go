// auth/email_verification_store_bbolt.go
package auth

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var emailVerificationBucket = []byte("email_verification_tokens")

type BoltEmailVerificationStore struct {
	db *bolt.DB
}

func NewBoltEmailVerificationStore(db *bolt.DB) (*BoltEmailVerificationStore, error) {
	if db == nil {
		return nil, fmt.Errorf("email verification store: nil database")
	}
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(emailVerificationBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init email_verification_tokens bucket: %w", err)
	}
	return &BoltEmailVerificationStore{db: db}, nil
}

func (s *BoltEmailVerificationStore) Save(record *EmailVerificationRecord) error {
	if record == nil {
		return fmt.Errorf("email verification record is nil")
	}
	if record.ID == "" || record.UserID == "" || record.TokenHash == "" {
		return fmt.Errorf("email verification record missing required field")
	}
	if record.ExpiresAt.IsZero() {
		return fmt.Errorf("email verification record expiry is empty")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal email verification record: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(emailVerificationBucket)
		if b == nil {
			return fmt.Errorf("email_verification_tokens bucket not found")
		}
		return b.Put([]byte(record.TokenHash), data)
	})
}

func (s *BoltEmailVerificationStore) FindByHash(tokenHash string) (*EmailVerificationRecord, error) {
	if tokenHash == "" {
		return nil, ErrEmailTokenNotFound
	}
	var record EmailVerificationRecord
	found := false
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(emailVerificationBucket)
		if b == nil {
			return fmt.Errorf("email_verification_tokens bucket not found")
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
		return nil, ErrEmailTokenNotFound
	}
	return &record, nil
}

// MarkUsed atomically validates and marks the token used — same
// single-write-transaction guarantee as BoltRefreshTokenStore.Rotate.
func (s *BoltEmailVerificationStore) MarkUsed(tokenHash string) (*EmailVerificationRecord, error) {
	if tokenHash == "" {
		return nil, ErrEmailTokenNotFound
	}
	var record EmailVerificationRecord
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(emailVerificationBucket)
		if b == nil {
			return fmt.Errorf("email_verification_tokens bucket not found")
		}
		data := b.Get([]byte(tokenHash))
		if data == nil {
			return ErrEmailTokenNotFound
		}
		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("failed to decode email verification record: %w", err)
		}
		if record.UsedAt != nil {
			return ErrEmailTokenUsed
		}
		if time.Now().After(record.ExpiresAt) {
			return ErrEmailTokenExpired
		}
		now := time.Now()
		record.UsedAt = &now
		updated, err := json.Marshal(&record)
		if err != nil {
			return fmt.Errorf("failed to encode used email verification record: %w", err)
		}
		return b.Put([]byte(tokenHash), updated)
	})
	if err != nil {
		return nil, err
	}
	return &record, nil
}
