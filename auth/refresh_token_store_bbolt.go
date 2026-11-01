// auth/refresh_token_store_bbolt.go
package auth

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// BoltRefreshTokenStore
//
// Persistent implementation of RefreshTokenStore.
//
// Key = token HASH.
// Plaintext refresh tokens are NEVER persisted.
//
// Rotation is performed inside a single Bolt write transaction so
// checking expiry/revocation and revoking the token happen atomically.
// ============================================================

var refreshTokensBucket = []byte("refresh_tokens")

type BoltRefreshTokenStore struct {
	db *bolt.DB
}

// NewBoltRefreshTokenStore initializes the refresh-token bucket.
func NewBoltRefreshTokenStore(db *bolt.DB) (*BoltRefreshTokenStore, error) {
	if db == nil {
		return nil, fmt.Errorf("refresh token store: nil database")
	}

	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(refreshTokensBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf(
			"failed to init refresh_tokens bucket: %w",
			err,
		)
	}

	return &BoltRefreshTokenStore{
		db: db,
	}, nil
}

// Save persists a refresh-token record.
//
// The record is indexed by TokenHash. The plaintext token itself
// is never passed to this store and therefore never persisted.
func (s *BoltRefreshTokenStore) Save(
	record *RefreshTokenRecord,
) error {
	if record == nil {
		return fmt.Errorf("refresh token record is nil")
	}

	if record.ID == "" {
		return fmt.Errorf("refresh token record id is empty")
	}

	if record.UserID == "" {
		return fmt.Errorf("refresh token record user id is empty")
	}

	if record.TokenHash == "" {
		return fmt.Errorf("refresh token record hash is empty")
	}

	if record.ExpiresAt.IsZero() {
		return fmt.Errorf("refresh token record expiry is empty")
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf(
			"failed to marshal refresh token record: %w",
			err,
		)
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(refreshTokensBucket)
		if b == nil {
			return fmt.Errorf("refresh_tokens bucket not found")
		}

		return b.Put([]byte(record.TokenHash), data)
	})
}

// FindByHash retrieves a refresh-token record by its SHA-256 hash.
func (s *BoltRefreshTokenStore) FindByHash(
	tokenHash string,
) (*RefreshTokenRecord, error) {
	if tokenHash == "" {
		return nil, fmt.Errorf("refresh_token_not_found")
	}

	var record RefreshTokenRecord
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(refreshTokensBucket)
		if b == nil {
			return fmt.Errorf("refresh_tokens bucket not found")
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
		return nil, fmt.Errorf("refresh_token_not_found")
	}

	return &record, nil
}

// Rotate atomically validates and revokes a refresh token.
//
// IMPORTANT:
//
// The entire operation happens inside ONE Bolt write transaction:
//
//  1. Find token by hash.
//  2. Check revoked state.
//  3. Check expiration.
//  4. Mark token revoked.
//  5. Persist the updated record.
//
// Because Bolt serializes write transactions, two concurrent requests
// attempting to consume the same token cannot both successfully
// rotate it.
//
// This method intentionally does NOT generate the replacement token.
// The caller generates the new token after the old token has been
// atomically consumed.
func (s *BoltRefreshTokenStore) Rotate(
	tokenHash string,
) (*RefreshTokenRecord, error) {
	if tokenHash == "" {
		return nil, errorsRefreshTokenNotFound()
	}

	var record RefreshTokenRecord

	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(refreshTokensBucket)
		if b == nil {
			return fmt.Errorf("refresh_tokens bucket not found")
		}

		data := b.Get([]byte(tokenHash))
		if data == nil {
			return errorsRefreshTokenNotFound()
		}

		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf(
				"failed to decode refresh token record: %w",
				err,
			)
		}

		if record.RevokedAt != nil {
			return errorsRefreshTokenAlreadyUsed()
		}

		if time.Now().After(record.ExpiresAt) {
			return errorsRefreshTokenExpired()
		}

		now := time.Now()
		record.RevokedAt = &now

		updated, err := json.Marshal(&record)
		if err != nil {
			return fmt.Errorf(
				"failed to encode rotated refresh token record: %w",
				err,
			)
		}

		return b.Put([]byte(tokenHash), updated)
	})

	if err != nil {
		return nil, err
	}

	return &record, nil
}

// Revoke marks a refresh-token record as revoked.
//
// Revoke receives the record ID, not the token hash, because the
// RefreshTokenStore interface defines this method in terms of ID.
//
// Since the primary Bolt key is TokenHash, this requires scanning
// the bucket. This is acceptable for the current scale. If token
// volume becomes large, a secondary ID index can be introduced.
func (s *BoltRefreshTokenStore) Revoke(id string) error {
	if id == "" {
		return errorsRefreshTokenNotFound()
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(refreshTokensBucket)
		if b == nil {
			return fmt.Errorf("refresh_tokens bucket not found")
		}

		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var record RefreshTokenRecord

			if err := json.Unmarshal(v, &record); err != nil {
				// Ignore malformed unrelated records and continue
				// searching for the requested ID.
				continue
			}

			if record.ID != id {
				continue
			}

			if record.RevokedAt != nil {
				// Revoke is idempotent for an already-revoked record.
				return nil
			}

			now := time.Now()
			record.RevokedAt = &now

			updated, err := json.Marshal(&record)
			if err != nil {
				return fmt.Errorf(
					"failed to encode revoked refresh token: %w",
					err,
				)
			}

			return b.Put(k, updated)
		}

		return errorsRefreshTokenNotFound()
	})
}

// RevokeAllForUser revokes every non-revoked refresh token belonging
// to userID. Same full-bucket-scan tradeoff as Revoke — acceptable
// at current scale.
func (s *BoltRefreshTokenStore) RevokeAllForUser(userID string) error {
	if userID == "" {
		return fmt.Errorf("user id is empty")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(refreshTokensBucket)
		if b == nil {
			return fmt.Errorf("refresh_tokens bucket not found")
		}
		c := b.Cursor()
		now := time.Now()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var record RefreshTokenRecord
			if err := json.Unmarshal(v, &record); err != nil {
				continue
			}
			if record.UserID != userID || record.RevokedAt != nil {
				continue
			}
			record.RevokedAt = &now
			updated, err := json.Marshal(&record)
			if err != nil {
				return fmt.Errorf("failed to encode revoked refresh token: %w", err)
			}
			if err := b.Put(k, updated); err != nil {
				return err
			}
		}
		return nil
	})
}

// ============================================================
// Internal error helpers
//
// Keep error strings aligned with the in-memory implementation
// and the existing refresh-token tests.
// ============================================================

func errorsRefreshTokenNotFound() error {
	return ErrRefreshTokenNotFound
}

func errorsRefreshTokenAlreadyUsed() error {
	return ErrRefreshTokenAlreadyUsed
}

func errorsRefreshTokenExpired() error {
	return ErrRefreshTokenExpired
}
