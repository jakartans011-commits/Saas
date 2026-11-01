package auth

import (
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestVerifyPasswordForLogin_EmptyHash_ShortCircuitsWithoutBcrypt(t *testing.T) {
	err := VerifyPasswordForLogin("", "anything")
	if !errors.Is(err, ErrPasswordLoginDisabled) {
		t.Fatalf("expected ErrPasswordLoginDisabled, got %v", err)
	}
}

func TestVerifyPasswordForLogin_CorrectPassword_ReturnsNil(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-horse-battery-staple"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate test hash: %v", err)
	}

	if err := VerifyPasswordForLogin(string(hash), "correct-horse-battery-staple"); err != nil {
		t.Fatalf("expected nil error for correct password, got %v", err)
	}
}

func TestVerifyPasswordForLogin_WrongPassword_ReturnsBcryptMismatchError(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate test hash: %v", err)
	}

	err = VerifyPasswordForLogin(string(hash), "wrong-password")
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
	if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("expected bcrypt.ErrMismatchedHashAndPassword, got %v", err)
	}
	if errors.Is(err, ErrPasswordLoginDisabled) {
		t.Fatal("wrong password on a non-empty hash must NOT be reported as ErrPasswordLoginDisabled")
	}
}

func TestVerifyPasswordForLogin_MalformedHash_ReturnsError(t *testing.T) {
	err := VerifyPasswordForLogin("not-a-real-bcrypt-hash", "whatever")
	if err == nil {
		t.Fatal("expected error for malformed hash, got nil")
	}
	if errors.Is(err, ErrPasswordLoginDisabled) {
		t.Fatal("malformed non-empty hash must NOT be reported as ErrPasswordLoginDisabled")
	}
}
