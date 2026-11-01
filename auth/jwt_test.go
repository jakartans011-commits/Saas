package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ============================================================
// JWT TESTS
// ============================================================

func TestGenerateAccessToken_Success(t *testing.T) {
	signingKey := []byte("test-key-32-bytes-long-1234567890")

	token, err := GenerateAccessToken("user-1", "tenant-1", "owner", signingKey)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestValidateAccessToken_Success(t *testing.T) {
	signingKey := []byte("test-key-32-bytes-long-1234567890")

	token, err := GenerateAccessToken("user-1", "tenant-1", "owner", signingKey)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	claims, err := ValidateAccessToken(token, signingKey)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}

	if claims.UserID != "user-1" {
		t.Errorf("expected UserID user-1, got %s", claims.UserID)
	}
	if claims.TenantID != "tenant-1" {
		t.Errorf("expected TenantID tenant-1, got %s", claims.TenantID)
	}
	if claims.RoleID != "owner" {
		t.Errorf("expected RoleID owner, got %s", claims.RoleID)
	}
}

func TestValidateAccessToken_InvalidSignature(t *testing.T) {
	signingKey := []byte("test-key-32-bytes-long-1234567890")
	wrongKey := []byte("wrong-key-32-bytes-long-1234567890")

	token, err := GenerateAccessToken("user-1", "tenant-1", "owner", signingKey)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	_, err = ValidateAccessToken(token, wrongKey)
	if err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestValidateAccessToken_EmptyToken(t *testing.T) {
	signingKey := []byte("test-key-32-bytes-long-1234567890")

	_, err := ValidateAccessToken("", signingKey)
	if err == nil {
		t.Error("expected error for empty token")
	}
}

func TestValidateAccessToken_MalformedToken(t *testing.T) {
	signingKey := []byte("test-key-32-bytes-long-1234567890")

	_, err := ValidateAccessToken("not-a-valid-jwt", signingKey)
	if err == nil {
		t.Error("expected error for malformed token")
	}
}

func TestGenerateAccessToken_EmptyUserID(t *testing.T) {
	signingKey := []byte("test-key-32-bytes-long-1234567890")

	token, err := GenerateAccessToken("", "tenant-1", "owner", signingKey)
	if err != nil {
		t.Fatalf("GenerateAccessToken with empty userID failed: %v", err)
	}

	if token == "" {
		t.Error("expected non-empty token even with empty userID")
	}
}

func TestGenerateAccessToken_EmptyTenantID(t *testing.T) {
	signingKey := []byte("test-key-32-bytes-long-1234567890")

	token, err := GenerateAccessToken("user-1", "", "owner", signingKey)
	if err != nil {
		t.Fatalf("GenerateAccessToken with empty tenantID failed: %v", err)
	}

	if token == "" {
		t.Error("expected non-empty token even with empty tenantID")
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	signingKey := []byte("test-key-32-bytes-long-1234567890")

	claims := &Claims{
		UserID:   "user-1",
		TenantID: "tenant-1",
		RoleID:   "owner",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ID:        newRandomID(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	_, err = ValidateAccessToken(tokenString, signingKey)
	if err == nil {
		t.Error("expected error for expired token")
	}
}
