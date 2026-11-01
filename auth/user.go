package auth

import (
	"errors"
	"sync"
	"time"
)

// ============================================================
// USER — WAJIB tenant_id (Section 7). Satu user = satu tenant,
// sesuai keputusan Anda sebelumnya (bukan multi-tenant per user).
// ============================================================

type UserStatus string

const (
	UserStatusActive    UserStatus = "active"
	UserStatusSuspended UserStatus = "suspended"
)

// ============================================================
// SENTINEL ERRORS
//
// Dipakai supaya caller (middleware) bisa membedakan JENIS
// kegagalan dengan errors.Is(), bukan string-matching yang
// rapuh. Pesan .Error() SENGAJA dipertahankan sama seperti
// sebelumnya ("user_not_found" / "user_suspended") supaya
// tidak ada kode lain yang bergantung pada teks lama menjadi
// rusak.
// ============================================================

var (
	ErrUserNotFound  = errors.New("user_not_found")
	ErrUserSuspended = errors.New("user_suspended")
)

type User struct {
	ID              string
	TenantID        string
	Email           string
	PasswordHash    string
	RoleID          string
	Status          UserStatus
	DeletedAt       *time.Time `json:"deleted_at,omitempty"` // GDPR soft delete
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
}

type UserStore interface {
	GetByID(userID string) (*User, error)
}

// InMemoryUserStore — pola sama seperti RoleStore/RefreshTokenStore,
// gampang diganti ke bbolt/SQL nanti tanpa mengubah middleware.
type InMemoryUserStore struct {
	mu    sync.RWMutex
	users map[string]*User
}

func NewInMemoryUserStore() *InMemoryUserStore {
	return &InMemoryUserStore{users: make(map[string]*User)}
}

func (s *InMemoryUserStore) Put(user *User) error {
	if user.TenantID == "" {
		return errors.New("user " + user.ID + ": tenant_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[user.ID] = user
	return nil
}

func (s *InMemoryUserStore) GetByID(userID string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[userID]
	if !ok {
		return nil, ErrUserNotFound
	}
	// Soft-deleted users are not accessible via GetByID()
	if user.DeletedAt != nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// CheckUserActive dipanggil middleware SETELAH JWT tervalidasi tapi
// SEBELUM request diteruskan. Kalau user suspended, akses ditolak
// walau JWT signature dan expiry-nya valid (Section 7).
//
// Error yang dikembalikan SELALU salah satu dari:
//
//	ErrUserNotFound  — user tidak ada ATAU sudah soft-deleted
//	ErrUserSuspended — user ada tapi status != active
//
// Caller (middleware) menggunakan errors.Is() untuk membedakan,
// supaya bisa memilih HTTP status code yang tepat (404 vs 403)
// tanpa membocorkan detail internal ke response body.
func CheckUserActive(store UserStore, userID string) error {
	user, err := store.GetByID(userID)
	if err != nil {
		return ErrUserNotFound
	}
	if user.Status != UserStatusActive {
		return ErrUserSuspended
	}
	return nil
}
