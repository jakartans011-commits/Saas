// auth/password_test.go
package auth

import "testing"

func TestPasswordHashing(t *testing.T) {
	t.Run("hash_and_verify_roundtrip", func(t *testing.T) {
		hash, err := HashPassword("correct-horse-battery-staple")
		if err != nil {
			t.Fatalf("HashPassword failed: %v", err)
		}
		if err := VerifyPassword(hash, "correct-horse-battery-staple"); err != nil {
			t.Fatalf("VerifyPassword failed for correct password: %v", err)
		}
		t.Log("OK: hash/verify roundtrip succeeds")
	})

	t.Run("wrong_password_rejected", func(t *testing.T) {
		hash, err := HashPassword("correct-horse-battery-staple")
		if err != nil {
			t.Fatalf("HashPassword failed: %v", err)
		}
		if err := VerifyPassword(hash, "wrong-password"); err == nil {
			t.Fatal("expected error for wrong password, got nil")
		}
		t.Log("OK: wrong password rejected")
	})

	t.Run("same_password_produces_different_hashes", func(t *testing.T) {
		hash1, err := HashPassword("same-password")
		if err != nil {
			t.Fatalf("HashPassword (1) failed: %v", err)
		}
		hash2, err := HashPassword("same-password")
		if err != nil {
			t.Fatalf("HashPassword (2) failed: %v", err)
		}
		if hash1 == hash2 {
			t.Fatal("SECURITY CONCERN: identical hashes for same password — salting may be broken")
		}
		// Tapi keduanya harus tetap valid untuk password yang sama.
		if err := VerifyPassword(hash1, "same-password"); err != nil {
			t.Fatalf("hash1 verify failed: %v", err)
		}
		if err := VerifyPassword(hash2, "same-password"); err != nil {
			t.Fatalf("hash2 verify failed: %v", err)
		}
		t.Log("OK: different hashes per call (salted), both verify correctly")
	})

	t.Run("malformed_hash_returns_error_not_panic", func(t *testing.T) {
		err := VerifyPassword("not-a-real-bcrypt-hash", "anything")
		if err == nil {
			t.Fatal("expected error for malformed hash, got nil")
		}
		t.Logf("OK: malformed hash handled gracefully: %v", err)
	})
}
