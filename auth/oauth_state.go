package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// OAuthStateStore: pola sama seperti LoginAttemptLimiter (in-memory + mutex).
type OAuthStateStore struct {
	mu     sync.Mutex
	states map[string]oauthStateEntry
	ttl    time.Duration
}

type oauthStateEntry struct {
	provider  string
	expiresAt time.Time
}

func NewOAuthStateStore() *OAuthStateStore {
	return &OAuthStateStore{
		states: make(map[string]oauthStateEntry),
		ttl:    10 * time.Minute,
	}
}

func (s *OAuthStateStore) Generate(provider string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	state := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	s.states[state] = oauthStateEntry{provider: provider, expiresAt: time.Now().Add(s.ttl)}
	return state, nil
}

// Consume: single-use — state dihapus begitu dicek, valid atau tidak (anti-replay).
func (s *OAuthStateStore) Consume(provider, state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.states[state]
	delete(s.states, state)
	if !ok || time.Now().After(entry.expiresAt) {
		return false
	}
	return entry.provider == provider
}

func (s *OAuthStateStore) cleanupLocked() {
	now := time.Now()
	for k, v := range s.states {
		if now.After(v.expiresAt) {
			delete(s.states, k)
		}
	}
}
