package auth

import (
	"errors"
	"time"
)

const PasswordResetTTL = 1 * time.Hour

type PasswordResetRecord struct {
	ID          string
	UserID      string
	TokenHash   string
	ExpiresAt   time.Time
	UsedAt      *time.Time
	RequestedIP string
}

type PasswordResetStore interface {
	Save(record *PasswordResetRecord) error
	FindByHash(tokenHash string) (*PasswordResetRecord, error)
	MarkUsed(tokenHash string) (*PasswordResetRecord, error)
}

var (
	ErrResetTokenNotFound = errors.New("password_reset_token_not_found")
	ErrResetTokenUsed     = errors.New("password_reset_token_already_used")
	ErrResetTokenExpired  = errors.New("password_reset_token_expired")
)

func GeneratePasswordResetToken(userID, requestedIP string, store PasswordResetStore) (plaintext string, err error) {
	plaintext = newRandomID() + newRandomID()
	record := &PasswordResetRecord{
		ID:          newRandomID(),
		UserID:      userID,
		TokenHash:   hashToken(plaintext),
		ExpiresAt:   time.Now().Add(PasswordResetTTL),
		RequestedIP: requestedIP,
	}
	if err := store.Save(record); err != nil {
		return "", err
	}
	return plaintext, nil
}

// ConsumePasswordResetToken marks the token used and returns the userID.
// Caller is responsible for updating the password hash and calling
// RefreshTokenStore.RevokeAllForUser afterward.
func ConsumePasswordResetToken(plaintext string, store PasswordResetStore) (userID string, err error) {
	record, err := store.MarkUsed(hashToken(plaintext))
	if err != nil {
		return "", err
	}
	return record.UserID, nil
}
