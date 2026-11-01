// auth/email_verification.go
package auth

import (
	"errors"
	"time"
)

const EmailVerificationTTL = 24 * time.Hour

type EmailVerificationRecord struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
}

type EmailVerificationStore interface {
	Save(record *EmailVerificationRecord) error
	FindByHash(tokenHash string) (*EmailVerificationRecord, error)
	MarkUsed(tokenHash string) (*EmailVerificationRecord, error)
}

func GenerateEmailVerificationToken(userID string, store EmailVerificationStore) (plaintext string, err error) {
	plaintext = newRandomID() + newRandomID()
	record := &EmailVerificationRecord{
		ID:        newRandomID(),
		UserID:    userID,
		TokenHash: hashToken(plaintext),
		ExpiresAt: time.Now().Add(EmailVerificationTTL),
	}
	if err := store.Save(record); err != nil {
		return "", err
	}
	return plaintext, nil
}

func VerifyEmailToken(plaintext string, store EmailVerificationStore) (userID string, err error) {
	record, err := store.MarkUsed(hashToken(plaintext))
	if err != nil {
		return "", err
	}
	return record.UserID, nil
}

var (
	ErrEmailTokenNotFound = errors.New("email_verification_token_not_found")
	ErrEmailTokenUsed     = errors.New("email_verification_token_already_used")
	ErrEmailTokenExpired  = errors.New("email_verification_token_expired")
)
