package auth

import (
	"errors"
	"sync"
	"time"
)

// AccessTokenRevocationStore menyimpan JTI access token yang sudah dicabut.
//
// Entry menyimpan expiry token supaya revocation store tidak tumbuh selamanya.
// Setelah token expired, JTI tidak perlu dipertahankan lagi.
type AccessTokenRevocationStore interface {
	Revoke(jti string, expiresAt time.Time) error
	IsRevoked(jti string) (bool, error)
}

// InMemoryAccessTokenRevocationStore dipakai oleh unit/integration test.
type InMemoryAccessTokenRevocationStore struct {
	mu      sync.RWMutex
	entries map[string]time.Time
}

func NewInMemoryAccessTokenRevocationStore() *InMemoryAccessTokenRevocationStore {
	return &InMemoryAccessTokenRevocationStore{
		entries: make(map[string]time.Time),
	}
}

func (s *InMemoryAccessTokenRevocationStore) Revoke(
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

	// Token sudah expired -> tidak ada gunanya memasukkannya ke denylist.
	if !expiresAt.After(now) {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[jti] = expiresAt
	return nil
}

func (s *InMemoryAccessTokenRevocationStore) IsRevoked(
	jti string,
) (bool, error) {
	if jti == "" {
		return false, errors.New("access_token_jti_required")
	}

	now := time.Now()

	s.mu.RLock()
	expiresAt, ok := s.entries[jti]
	s.mu.RUnlock()

	if !ok {
		return false, nil
	}

	// Lazy cleanup.
	if !expiresAt.After(now) {
		s.mu.Lock()
		delete(s.entries, jti)
		s.mu.Unlock()
		return false, nil
	}

	return true, nil
}
