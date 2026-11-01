package auth

import (
	"errors"
	"time"
)

// Keputusan produk (2a/2c):
//  2a: PasswordHash tetap string, "" untuk user OAuth-only. Hard-check
//      ada di password_login.go (VerifyPasswordForLogin), bukan di sini.
//  2c: auto-link ke user existing berdasarkan email match — tanpa
//      konfirmasi tambahan (standar industri: Vercel, Linear, dll).

var ErrOAuthEmailNotVerified = errors.New("oauth_email_not_verified")

// UserPutStore — subset kecil dari UserStore, biar fungsi ini jalan
// baik untuk InMemoryUserStore maupun BoltUserStore tanpa perlu tahu
// tipe konkretnya.
type UserPutStore interface {
	Put(user *User) error
}

// NewTenantFunc diinject dari cmd/saas-api supaya package auth tidak
// perlu import package tenant (hindari cyclic dependency + tidak
// bergantung pada signature ProvisionTenant yang belum saya lihat).
type NewTenantFunc func(ownerEmail string) (tenantID string, ownerRoleID string, err error)

// ResolveOAuthUser: cari user existing via email (auto-link), atau
// buat tenant+user baru kalau belum pernah terdaftar.
func ResolveOAuthUser(
	lookup UserEmailLookupStore,
	store UserPutStore,
	newTenant NewTenantFunc,
	email string,
) (*User, error) {
	if email == "" {
		return nil, ErrOAuthEmailNotVerified
	}

	if existing, err := lookup.FindByEmail(email); err == nil && existing != nil {
		if existing.Status != UserStatusActive {
			return nil, ErrUserSuspended
		}
		return existing, nil
	}

	tenantID, ownerRoleID, err := newTenant(email)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user := &User{
		ID:              newRandomID(),
		TenantID:        tenantID,
		Email:           email,
		PasswordHash:    "", // OAuth-only — lihat VerifyPasswordForLogin
		RoleID:          ownerRoleID,
		Status:          UserStatusActive,
		EmailVerifiedAt: &now,
	}

	if err := store.Put(user); err != nil {
		return nil, err
	}
	return user, nil
}
