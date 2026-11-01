package auth

import "errors"

var ErrPasswordLoginDisabled = errors.New("password_login_disabled")

// VerifyPasswordForLogin: hard-check 2a. Ganti pemanggilan
// VerifyPassword(hash, plaintext) di login handler Anda jadi
// VerifyPasswordForLogin(hash, plaintext) — satu baris.
func VerifyPasswordForLogin(hash, plaintext string) error {
	if hash == "" {
		// User OAuth-only. Sengaja tidak panggil bcrypt sama sekali.
		return ErrPasswordLoginDisabled
	}
	return VerifyPassword(hash, plaintext)
}
