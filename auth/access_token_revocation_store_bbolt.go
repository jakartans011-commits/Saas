package auth

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var accessTokenRevocationsBucket = []byte("access_token_revocations")

type BoltAccessTokenRevocationStore struct {
	db *bolt.DB
}

func NewBoltAccessTokenRevocationStore(
	db *bolt.DB,
) (*BoltAccessTokenRevocationStore, error) {
	if db == nil {
		return nil, errors.New("nil_bolt_db")
	}

	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(accessTokenRevocationsBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf(
			"failed to init access_token_revocations bucket: %w",
			err,
		)
	}

	return &BoltAccessTokenRevocationStore{db: db}, nil
}

func (s *BoltAccessTokenRevocationStore) Revoke(
	jti string,
	expiresAt time.Time,
) error {
	if jti == "" {
		return errors.New("access_token_jti_required")
	}

	if expiresAt.IsZero() {
		return errors.New("access_token_expiry_required")
	}

	now := time.Now()

	// Jangan menyimpan JTI token yang sudah expired.
	if !expiresAt.After(now) {
		return nil
	}

	var value [8]byte
	binary.BigEndian.PutUint64(
		value[:],
		uint64(expiresAt.UnixNano()),
	)

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(accessTokenRevocationsBucket)
		if b == nil {
			return errors.New("access_token_revocations_bucket_missing")
		}

		return b.Put([]byte(jti), value[:])
	})
}

func (s *BoltAccessTokenRevocationStore) IsRevoked(
	jti string,
) (bool, error) {
	if jti == "" {
		return false, errors.New("access_token_jti_required")
	}

	var expiresAt time.Time
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(accessTokenRevocationsBucket)
		if b == nil {
			return errors.New("access_token_revocations_bucket_missing")
		}

		raw := b.Get([]byte(jti))
		if raw == nil {
			return nil
		}

		if len(raw) != 8 {
			return errors.New("invalid_access_token_revocation_record")
		}

		nanos := int64(binary.BigEndian.Uint64(raw))
		expiresAt = time.Unix(0, nanos)
		found = true

		return nil
	})

	if err != nil {
		return false, err
	}

	if !found {
		return false, nil
	}

	// Lazy cleanup expired JTI.
	if !expiresAt.After(time.Now()) {
		err := s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(accessTokenRevocationsBucket)
			if b == nil {
				return errors.New("access_token_revocations_bucket_missing")
			}
			return b.Delete([]byte(jti))
		})
		if err != nil {
			return false, err
		}

		return false, nil
	}

	return true, nil
}
