package middleware

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"trailseal/auth"
)

// ============================================================
// TEST INFRASTRUCTURE
// ============================================================

var testSigningKey = []byte("test-signing-key-do-not-use-in-prod")

// buildToken signs a JWT with full control over claims and algorithm,
// so tests can construct edge cases (missing JTI, wrong alg, expired)
// that auth.GenerateAccessToken's happy-path API can't produce.
func buildToken(t *testing.T, claims auth.Claims, method jwt.SigningMethod, key []byte) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return signed
}

func validClaims(userID, tenantID, roleID string) auth.Claims {
	now := time.Now()
	return auth.Claims{
		UserID:   userID,
		TenantID: tenantID,
		RoleID:   roleID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        "jti-" + userID,
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
}

// mockRevocationStore lets tests force IsRevoked to fail (simulating
// a store outage), which the InMemory store can never do.
type mockRevocationStore struct {
	inner       *auth.InMemoryAccessTokenRevocationStore
	isRevokedErr error
}

func newMockRevocationStore() *mockRevocationStore {
	return &mockRevocationStore{inner: auth.NewInMemoryAccessTokenRevocationStore()}
}

func (m *mockRevocationStore) Revoke(jti string, expiresAt time.Time) error {
	return m.inner.Revoke(jti, expiresAt)
}

func (m *mockRevocationStore) IsRevoked(jti string) (bool, error) {
	if m.isRevokedErr != nil {
		return false, m.isRevokedErr
	}
	return m.inner.IsRevoked(jti)
}

func newActiveUserStore(userID, tenantID string) *auth.InMemoryUserStore {
	store := auth.NewInMemoryUserStore()
	_ = store.Put(&auth.User{
		ID:       userID,
		TenantID: tenantID,
		Email:    userID + "@example.com",
		RoleID:   "role-member",
		Status:   auth.UserStatusActive,
	})
	return store
}

// ============================================================
// RequireAuth
// ============================================================

func TestRequireAuth_Success(t *testing.T) {
	userStore := newActiveUserStore("user-1", "tenant-1")
	revStore := newMockRevocationStore()

	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, userStore, revStore))
	app.Get("/test", func(c *fiber.Ctx) error {
		claims, ok := ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(500).JSON(fiber.Map{"error": "no claims"})
		}
		return c.JSON(fiber.Map{"user_id": claims.UserID, "tenant_id": claims.TenantID})
	})

	token := buildToken(t, validClaims("user-1", "tenant-1", "role-member"), jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("u", "t"), newMockRevocationStore()))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRequireAuth_MalformedHeader_NoBearerPrefix(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("u", "t"), newMockRevocationStore()))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "sometoken")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRequireAuth_InvalidSignature(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("u", "t"), newMockRevocationStore()))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	// Signed with a DIFFERENT key than the middleware validates against.
	token := buildToken(t, validClaims("u", "t", "role"), jwt.SigningMethodHS256, []byte("wrong-key"))
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// TestRequireAuth_AlgorithmConfusionRejected proves the
// jwt.WithValidMethods([]string{"HS256"}) protection actually works:
// a token signed with a different HMAC algorithm (HS384) must be
// rejected even though it's a valid signature under that algorithm.
func TestRequireAuth_AlgorithmConfusionRejected(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("u", "t"), newMockRevocationStore()))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	token := buildToken(t, validClaims("u", "t", "role"), jwt.SigningMethodHS384, testSigningKey)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for HS384 token, got %d", resp.StatusCode)
	}
}

func TestRequireAuth_ExpiredToken(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("u", "t"), newMockRevocationStore()))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	claims := validClaims("u", "t", "role")
	past := time.Now().Add(-1 * time.Hour)
	claims.ExpiresAt = jwt.NewNumericDate(past)

	token := buildToken(t, claims, jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for expired token, got %d", resp.StatusCode)
	}
}

// TestRequireAuth_MissingJTI_Rejected covers the explicit `claims.ID == ""`
// guard: a token without a JTI can never be individually revoked, so it
// must never be accepted, even if signature and expiry are otherwise valid.
func TestRequireAuth_MissingJTI_Rejected(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("u", "t"), newMockRevocationStore()))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	claims := validClaims("u", "t", "role")
	claims.ID = "" // no JTI

	token := buildToken(t, claims, jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for token without JTI, got %d", resp.StatusCode)
	}
}

func TestRequireAuth_RevokedToken_Rejected(t *testing.T) {
	revStore := newMockRevocationStore()
	claims := validClaims("u", "t", "role")

	// Revoke the JTI up front, simulating a previously logged-out session.
	if err := revStore.Revoke(claims.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to seed revocation: %v", err)
	}

	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("u", "t"), revStore))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	token := buildToken(t, claims, jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for revoked token, got %d", resp.StatusCode)
	}
}

// TestRequireAuth_RevocationStoreError_FailsClosed proves that if the
// revocation store itself is unreachable, the token is REJECTED, not
// silently accepted â€” "fail closed" per the comment in auth_middleware.go.
func TestRequireAuth_RevocationStoreError_FailsClosed(t *testing.T) {
	revStore := newMockRevocationStore()
	revStore.isRevokedErr = errors.New("revocation store unreachable")

	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, newActiveUserStore("u", "t"), revStore))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	token := buildToken(t, validClaims("u", "t", "role"), jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 (fail closed) when revocation store errors, got %d", resp.StatusCode)
	}
}

func TestRequireAuth_UserNotFound_Returns404(t *testing.T) {
	userStore := auth.NewInMemoryUserStore() // empty, no user seeded
	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, userStore, newMockRevocationStore()))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	token := buildToken(t, validClaims("ghost-user", "t", "role"), jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("expected 404 for nonexistent user, got %d", resp.StatusCode)
	}
}

func TestRequireAuth_SoftDeletedUser_Returns404(t *testing.T) {
	userStore := auth.NewInMemoryUserStore()
	deletedAt := time.Now()
	_ = userStore.Put(&auth.User{
		ID:        "user-1",
		TenantID:  "t",
		Status:    auth.UserStatusActive,
		DeletedAt: &deletedAt,
	})

	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, userStore, newMockRevocationStore()))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	token := buildToken(t, validClaims("user-1", "t", "role"), jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	// GetByID treats soft-deleted as not-found -> ErrUserNotFound -> 404.
	if resp.StatusCode != 404 {
		t.Errorf("expected 404 for soft-deleted user on normal RequireAuth, got %d", resp.StatusCode)
	}
}

func TestRequireAuth_SuspendedUser_Rejected(t *testing.T) {
	userStore := auth.NewInMemoryUserStore()
	_ = userStore.Put(&auth.User{
		ID:       "user-1",
		TenantID: "t",
		Status:   auth.UserStatusSuspended,
	})

	app := fiber.New()
	app.Use(RequireAuth(testSigningKey, userStore, newMockRevocationStore()))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendString("ok") })

	token := buildToken(t, validClaims("user-1", "t", "role"), jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for suspended user, got %d", resp.StatusCode)
	}
}

// ============================================================
// RequireAuthAllowDeletedUser
// ============================================================

func TestRequireAuthAllowDeletedUser_AllowsSoftDeletedUser(t *testing.T) {
	// No user store lookup happens at all in this path (userStore is
	// intentionally unused per the comment in auth_middleware.go), so
	// there is no need to seed anything â€” a nil-safe empty store works.
	userStore := auth.NewInMemoryUserStore()

	app := fiber.New()
	app.Use(RequireAuthAllowDeletedUser(testSigningKey, userStore, newMockRevocationStore()))
	app.Post("/restore", func(c *fiber.Ctx) error {
		claims, ok := ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(500).JSON(fiber.Map{"error": "no claims"})
		}
		return c.JSON(fiber.Map{"tenant_id": claims.TenantID})
	})

	// User "user-1" does not even exist in userStore â€” proving this
	// path truly bypasses the active-user check.
	token := buildToken(t, validClaims("user-1", "tenant-1", "admin"), jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("POST", "/restore", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 (soft-deleted/nonexistent user allowed), got %d", resp.StatusCode)
	}
}

func TestRequireAuthAllowDeletedUser_MissingTenantID_Rejected(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuthAllowDeletedUser(testSigningKey, auth.NewInMemoryUserStore(), newMockRevocationStore()))
	app.Post("/restore", func(c *fiber.Ctx) error { return c.SendString("ok") })

	claims := validClaims("user-1", "" /* no tenant */, "admin")
	token := buildToken(t, claims, jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("POST", "/restore", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for missing tenant_id, got %d", resp.StatusCode)
	}
}

func TestRequireAuthAllowDeletedUser_MissingUserID_Rejected(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuthAllowDeletedUser(testSigningKey, auth.NewInMemoryUserStore(), newMockRevocationStore()))
	app.Post("/restore", func(c *fiber.Ctx) error { return c.SendString("ok") })

	claims := validClaims("" /* no user id */, "tenant-1", "admin")
	token := buildToken(t, claims, jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("POST", "/restore", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for missing user_id, got %d", resp.StatusCode)
	}
}

func TestRequireAuthAllowDeletedUser_RevokedToken_Rejected(t *testing.T) {
	revStore := newMockRevocationStore()
	claims := validClaims("user-1", "tenant-1", "admin")
	if err := revStore.Revoke(claims.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("failed to seed revocation: %v", err)
	}

	app := fiber.New()
	app.Use(RequireAuthAllowDeletedUser(testSigningKey, auth.NewInMemoryUserStore(), revStore))
	app.Post("/restore", func(c *fiber.Ctx) error { return c.SendString("ok") })

	token := buildToken(t, claims, jwt.SigningMethodHS256, testSigningKey)
	req := httptest.NewRequest("POST", "/restore", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for revoked token even on the allow-deleted path, got %d", resp.StatusCode)
	}
}

func TestRequireAuthAllowDeletedUser_MissingHeader_Rejected(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuthAllowDeletedUser(testSigningKey, auth.NewInMemoryUserStore(), newMockRevocationStore()))
	app.Post("/restore", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("POST", "/restore", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 for missing header, got %d", resp.StatusCode)
	}
}