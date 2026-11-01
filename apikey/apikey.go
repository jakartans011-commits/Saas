package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ============================================================
// API KEY
//
// PERUBAHAN DARI VERSI SEBELUMNYA:
//   - Plaintext key TIDAK LAGI disimpan. Hanya key_hash (SHA-256)
//     yang tersimpan di database. Plaintext hanya dikembalikan ke
//     client SATU KALI saat GenerateAPIKey dipanggil, sama seperti
//     pola refresh token di auth/jwt.go.
//   - Scoped ke Project (bukan cuma Tenant) — satu Project bisa
//     punya banyak key dengan Scope berbeda (ingest/read/admin).
// ============================================================

type Scope string

const (
	ScopeIngest Scope = "ingest"
	ScopeRead   Scope = "read"
	ScopeAdmin  Scope = "admin"
)

func (s Scope) Valid() bool {
	switch s {
	case ScopeIngest, ScopeRead, ScopeAdmin:
		return true
	default:
		return false
	}
}

// CanIngest / CanRead menentukan apakah scope ini boleh memanggil
// endpoint ingest / read. Admin boleh keduanya.
func (s Scope) CanIngest() bool { return s == ScopeIngest || s == ScopeAdmin }
func (s Scope) CanRead() bool   { return s == ScopeRead || s == ScopeAdmin }

type APIKey struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	TenantID   string     `json:"tenant_id"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"` // tidak pernah di-JSON-kan
	KeyPrefix  string     `json:"key_prefix"`
	Scope      Scope      `json:"scope"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

var (
	ErrNotFound     = errors.New("api_key_not_found")
	ErrRevoked      = errors.New("api_key_revoked")
	ErrInvalidScope = errors.New("invalid_scope")
)

// GeneratedKey adalah hasil pembuatan key baru — Plaintext HANYA
// ada di struct ini, tidak pernah disimpan ke storage.
type GeneratedKey struct {
	Record    *APIKey
	Plaintext string
}

// GenerateAPIKey membuat key baru. Plaintext dikembalikan sekali
// ke pemanggil (untuk dikirim ke client) — pemanggil WAJIB
// menyimpan Record (dengan KeyHash) ke store, BUKAN plaintext-nya.
func GenerateAPIKey(tenantID, projectID, name string, scope Scope) (*GeneratedKey, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if !scope.Valid() {
		return nil, ErrInvalidScope
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	plaintext := "tsk_" + hex.EncodeToString(b) // prefix "tsk_" = TrailSeal Key, memudahkan identifikasi di log/scanner secret

	record := &APIKey{
		ProjectID: projectID,
		TenantID:  tenantID,
		Name:      name,
		KeyHash:   HashKey(plaintext),
		KeyPrefix: plaintext[:12], // "tsk_" + 8 hex char, cukup untuk ditampilkan di UI tanpa membocorkan key
		Scope:     scope,
		CreatedAt: time.Now(),
	}

	return &GeneratedKey{Record: record, Plaintext: plaintext}, nil
}

// HashKey — SHA-256 sudah cukup di sini (BUKAN untuk password).
// Key API punya entropi tinggi (32 byte random) dan tidak
// diketik manusia, jadi tidak butuh cost function lambat seperti
// bcrypt — beda kasus dengan password login.
func HashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}