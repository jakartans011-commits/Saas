package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	// SessionHeader adalah nama header sesi Streamable HTTP (MCP spec).
	SessionHeader = "Mcp-Session-Id"

	defaultSessionTTL    = 12 * time.Hour
	sessionSweepInterval = time.Minute
)

type sessionInfo struct {
	tenantID  string
	userID    string
	expiresAt time.Time
}

// SessionStore menyimpan MCP session in-memory. Sesi diikat ke
// (tenantID, userID) sehingga ID yang bocor/tertebak tidak bisa
// dipakai lintas tenant. Expiry bersifat sliding (diperpanjang setiap
// dipakai). Cleanup dilakukan lazy saat Create, tanpa goroutine,
// jadi tidak ada resource yang bocor di test.
type SessionStore struct {
	mu        sync.Mutex
	sessions  map[string]sessionInfo
	ttl       time.Duration
	lastSweep time.Time
	now       func() time.Time
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	return &SessionStore{
		sessions: make(map[string]sessionInfo),
		ttl:      ttl,
		now:      time.Now,
	}
}

// Create membuat sesi baru dan mengembalikan ID-nya.
// Mengembalikan "" hanya jika crypto/rand gagal (praktis tidak terjadi).
func (st *SessionStore) Create(tenantID, userID string) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	id := hex.EncodeToString(buf)

	st.mu.Lock()
	defer st.mu.Unlock()

	now := st.now()
	if now.Sub(st.lastSweep) >= sessionSweepInterval {
		for k, v := range st.sessions {
			if now.After(v.expiresAt) {
				delete(st.sessions, k)
			}
		}
		st.lastSweep = now
	}

	st.sessions[id] = sessionInfo{
		tenantID:  tenantID,
		userID:    userID,
		expiresAt: now.Add(st.ttl),
	}
	return id
}

// Validate true jika sesi ada, belum expired, dan milik tenant+user ini.
func (st *SessionStore) Validate(id, tenantID, userID string) bool {
	if id == "" {
		return false
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	info, ok := st.sessions[id]
	if !ok {
		return false
	}

	now := st.now()
	if now.After(info.expiresAt) {
		delete(st.sessions, id)
		return false
	}
	if info.tenantID != tenantID || info.userID != userID {
		return false
	}

	info.expiresAt = now.Add(st.ttl)
	st.sessions[id] = info
	return true
}

// Delete menghapus sesi (dipakai nanti untuk DELETE terminate).
func (st *SessionStore) Delete(id string) {
	st.mu.Lock()
	delete(st.sessions, id)
	st.mu.Unlock()
}

// SetStrictSessions: false (default, lenient) = header hanya divalidasi
// kalau dikirim. true = semua request non-initialize wajib punya sesi valid.
func (s *Server) SetStrictSessions(strict bool) {
	if s != nil {
		s.strictSessions = strict
	}
}