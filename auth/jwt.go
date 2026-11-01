// auth/jwt.go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ============================================================
// TYPED CONTEXT KEY (Section 15) — bukan string literal, supaya
// tidak collision dengan package lain yang juga pakai context.
// ============================================================

type contextKey string

const claimsContextKey contextKey = "auth_claims"

// ============================================================
// CLAIMS
// ============================================================

type Claims struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	RoleID   string `json:"role_id"`
	jwt.RegisteredClaims
}

const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 7 * 24 * time.Hour
)

// ============================================================
// ACCESS TOKEN — JWT, HS256 ONLY
// ============================================================

// GenerateAccessToken membuat JWT access token. signingKey WAJIB
// datang dari config/secret abstraction milik caller — jangan
// hardcode di sini (Section 12).
func GenerateAccessToken(userID, tenantID, roleID string, signingKey []byte) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		TenantID: tenantID,
		RoleID:   roleID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        newRandomID(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(signingKey)
}

// ValidateAccessToken memvalidasi signature, expiry, DAN algoritma.
//
// PROTEKSI ALGORITHM CONFUSION (Section 14, Section 20):
// jwt.WithValidMethods memaksa parser HANYA menerima HS256 — kalau
// attacker mengirim token dengan header alg:"none" atau alg:"RS256"
// (mencoba trik memakai public key sebagai HMAC secret), parser akan
// MENOLAK token SEBELUM signature diverifikasi sama sekali.
// Tanpa opsi ini, library JWT populer HISTORIS rentan terhadap
// serangan ini kalau signing method tidak dipaksa eksplisit.
func ValidateAccessToken(tokenString string, signingKey []byte) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(t *jwt.Token) (interface{}, error) {
			return signingKey, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid_token: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("invalid_token: token marked invalid")
	}

	return claims, nil
}

// ============================================================
// REFRESH TOKEN — opaque random string, BUKAN JWT (Section 13).
// Hanya HASH yang disimpan, tidak pernah plaintext.
// ============================================================

type RefreshTokenRecord struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type RefreshTokenStore interface {
	Save(record *RefreshTokenRecord) error
	FindByHash(tokenHash string) (*RefreshTokenRecord, error)
	Revoke(id string) error

	// Rotate atomically validates and revokes the old token.
	// It must guarantee that only one concurrent request can consume
	// the same refresh token.
	Rotate(tokenHash string) (*RefreshTokenRecord, error)
	RevokeAllForUser(userID string) error
}

// InMemoryRefreshTokenStore — lihat catatan yang sama seperti
// RoleStore: Fase 1 pakai in-memory, gampang diganti nanti.
type InMemoryRefreshTokenStore struct {
	mu      sync.RWMutex
	records map[string]*RefreshTokenRecord // key: token hash
}

func NewInMemoryRefreshTokenStore() *InMemoryRefreshTokenStore {
	return &InMemoryRefreshTokenStore{records: make(map[string]*RefreshTokenRecord)}
}

func (s *InMemoryRefreshTokenStore) Save(record *RefreshTokenRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.TokenHash] = record
	return nil
}

func (s *InMemoryRefreshTokenStore) FindByHash(tokenHash string) (*RefreshTokenRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[tokenHash]
	if !ok {
		return nil, errors.New("refresh_token_not_found")
	}
	return rec, nil
}

func (s *InMemoryRefreshTokenStore) Rotate(tokenHash string) (*RefreshTokenRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.records[tokenHash]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}

	if rec.RevokedAt != nil {
		return nil, ErrRefreshTokenAlreadyUsed
	}

	if time.Now().After(rec.ExpiresAt) {
		return nil, ErrRefreshTokenExpired
	}

	now := time.Now()
	rec.RevokedAt = &now

	return rec, nil
}

func (s *InMemoryRefreshTokenStore) RevokeAllForUser(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, rec := range s.records {
		if rec.UserID == userID && rec.RevokedAt == nil {
			rec.RevokedAt = &now
		}
	}
	return nil
}

// GenerateRefreshToken membuat opaque token baru + record hash-nya.
// Mengembalikan PLAINTEXT token (dikirim ke client SEKALI SAJA) dan
// record yang disimpan (hash saja).
func GenerateRefreshToken(userID string, store RefreshTokenStore) (plaintext string, err error) {
	plaintext = newRandomID() + newRandomID() // 2x entropy
	hash := hashToken(plaintext)

	record := &RefreshTokenRecord{
		ID:        newRandomID(),
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(RefreshTokenTTL),
	}

	if err := store.Save(record); err != nil {
		return "", err
	}
	return plaintext, nil
}

// RotateRefreshToken memvalidasi token lama, WAJIB me-revoke-nya,
// lalu menerbitkan token baru. Token lama yang dipakai lagi setelah
// ini HARUS gagal (Section 19 Test 6).
func RotateRefreshToken(
	oldPlaintext string,
	userID string,
	store RefreshTokenStore,
) (newPlaintext string, err error) {
	hash := hashToken(oldPlaintext)

	record, err := store.FindByHash(hash)
	if err != nil {
		return "", fmt.Errorf("invalid_refresh_token: %w", err)
	}

	if record.RevokedAt != nil {
		return "", errors.New("refresh_token_already_used")
	}

	if time.Now().After(record.ExpiresAt) {
		return "", errors.New("refresh_token_expired")
	}

	if record.UserID != userID {
		return "", errors.New("refresh_token_user_mismatch")
	}

	// Konsumsi token lama lewat Rotate, bukan Revoke.
	//
	// Pemeriksaan RevokedAt/ExpiresAt di atas hanya jalur cepat untuk
	// pesan error; otoritasnya ada pada Rotate, yang memeriksa keduanya
	// DI DALAM satu transaksi tulis. Dengan Revoke terpisah, dua request
	// paralel sama-sama lolos pemeriksaan di atas lalu sama-sama menerima
	// token baru — satu refresh token mencetak dua sesi.
	if _, err := store.Rotate(hash); err != nil {
		return "", err
	}

	return GenerateRefreshToken(userID, store)
}

// RotateRefreshTokenForAnyUser memvalidasi refresh token tanpa
// membutuhkan user ID dari client.
//
// User ID diambil dari record refresh token yang tersimpan di server.
// Setelah itu token lama di-rotate melalui RotateRefreshToken(),
// sehingga token lama langsung menjadi tidak valid.
//
// Return:
//   - newPlaintext: refresh token baru
//   - userID:       pemilik refresh token
func RotateRefreshTokenForAnyUser(
	oldPlaintext string,
	store RefreshTokenStore,
) (newPlaintext string, userID string, err error) {
	hash := hashToken(oldPlaintext)

	record, err := store.Rotate(hash)
	if err != nil {
		return "", "", fmt.Errorf(
			"invalid_refresh_token: %w",
			err,
		)
	}

	if record.UserID == "" {
		return "", "", errors.New(
			"refresh_token_user_missing",
		)
	}

	newPlaintext, err = GenerateRefreshToken(
		record.UserID,
		store,
	)
	if err != nil {
		return "", "", err
	}

	return newPlaintext, record.UserID, nil
}

func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func newRandomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ============================================================
// CONTEXT HELPERS
// ============================================================

func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*Claims)
	return claims, ok
}
