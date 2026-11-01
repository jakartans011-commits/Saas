// cmd/saas-api/app_test.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	bolt "go.etcd.io/bbolt"

	"trailseal/auth"
	"trailseal/db"
	"trailseal/middleware"
	"trailseal/rbac"
	"trailseal/tenant"
)

const (
	e2eSigningKey = "e2e-test-signing-key-min-32-characters-long"
	e2eRootKey    = "e2e-test-root-key-min-32-characters-long"
)

// testEnv membangun app SaaS API LENGKAP lewat BuildApp — fungsi
// yang SAMA PERSIS dipakai main() di production — di atas bbolt db
// temporary. Juga menyimpan akses langsung ke store untuk fixture
// setup yang butuh bypass HTTP (lihat bootstrapTenantOwner) dan
// untuk verifikasi state DB setelah request ditolak.
type testEnv struct {
	app         *fiber.App
	userStore   *auth.BoltUserStore
	roleStore   *rbac.BoltRoleStore
	tenantStore *tenant.BoltTenantStore
	pgPool      *pgxpool.Pool // nil kalau TRAILSEAL_TEST_POSTGRES_URL tidak di-set
}

// requirePostgres di-panggil di AWAL test yang butuh Postgres
// sungguhan (project/apikey/events). Kalau env var tidak di-set,
// test di-SKIP (bukan gagal) — supaya `go test ./...` tetap bisa
// jalan di mesin mana pun tanpa Postgres nyala, tapi test ini tetap
// jalan penuh begitu env var di-set (mis. di CI).
func requirePostgres(t *testing.T, env *testEnv) {
	t.Helper()
	if env.pgPool == nil {
		t.Skip("TRAILSEAL_TEST_POSTGRES_URL not set, skipping test that requires Postgres")
	}
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "e2e-saas-api.db")
	// NoSync: database test hidup di t.TempDir() dan dibuang begitu test
	// selesai, jadi fsync per commit tidak melindungi apa pun di sini — ia
	// hanya memperlambat. Rate limiter menulis SATU transaksi bbolt untuk
	// SETIAP request (middleware/rate_limit.go, rateCounter.Allow), jadi di
	// disk laptop satu request berbiaya ~54 ms dan 1005 request memakan ~54
	// detik — hampir selebar window limiter itu sendiri.
	//
	// HANYA untuk test. Produksi (cmd/saas-api/main.go) tetap membuka bbolt
	// dengan sync penuh; mematikannya di sana berarti kehilangan tulisan
	// yang sudah di-commit kalau proses mati mendadak.
	boltDB, err := bolt.Open(dbPath, 0600, &bolt.Options{NoSync: true})
	if err != nil {
		t.Fatalf("failed to open test bolt db: %v", err)
	}
	t.Cleanup(func() { _ = boltDB.Close() })
	t.Cleanup(func() { middleware.StopAllRateLimiterCleanups() })

	var pgPool *pgxpool.Pool
	if pgURL := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL"); pgURL != "" {
		pool, err := db.Connect(context.Background(), pgURL)
		if err != nil {
			t.Fatalf("TRAILSEAL_TEST_POSTGRES_URL is set but connection failed: %v", err)
		}
		pgPool = pool
		t.Cleanup(func() { pool.Close() })
	}

	// Test membaca token dari log LoggingMailer; jangan pernah kirim email sungguhan.
	t.Setenv("RESEND_API_KEY", "")

	app, _, err := BuildApp(boltDB, pgPool, e2eSigningKey, e2eRootKey, nil)
	if err != nil {
		t.Fatalf("BuildApp failed: %v", err)
	}

	userStore, err := auth.NewBoltUserStore(boltDB)
	if err != nil {
		t.Fatalf("NewBoltUserStore failed: %v", err)
	}
	roleStore, err := rbac.NewBoltRoleStore(boltDB)
	if err != nil {
		t.Fatalf("NewBoltRoleStore failed: %v", err)
	}
	tenantStore, err := tenant.NewBoltTenantStore(boltDB)
	if err != nil {
		t.Fatalf("NewBoltTenantStore failed: %v", err)
	}

	return &testEnv{
		app:         app,
		userStore:   userStore,
		roleStore:   roleStore,
		tenantStore: tenantStore,
		pgPool:      pgPool,
	}
}

// doJSON mengirim request lewat app.Test (Fiber in-memory — TIDAK
// membuka port TCP sungguhan, tapi tetap melewati seluruh Fiber
// router + middleware chain + handler yang sesungguhnya). Timeout
// dinaikkan ke 5s karena handler login/create-user memanggil bcrypt
// (cost 12, ~200-300ms per operasi — atau lebih cepat kalau
// SHADOWHARVEST_TEST_BCRYPT_COST di-set).
func (e *testEnv) doJSON(t *testing.T, method, path string, headers map[string]string, body any, out any) int {
	t.Helper()

	var reqBody *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(data)
	} else {
		reqBody = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := e.app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test failed for %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("failed to decode response body for %s %s: %v", method, path, err)
		}
	}

	return resp.StatusCode
}

// bootstrapTenantOwner membuat tenant lewat HTTP root endpoint
// SUNGGUHAN (jalur produksi), lalu insert user owner PERTAMA-nya
// LANGSUNG lewat store.
func (e *testEnv) bootstrapTenantOwner(t *testing.T, tenantID, name, email, password string) {
	t.Helper()

	status := e.doJSON(t, fiber.MethodPost, "/api/admin/tenants",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		map[string]string{"tenant_id": tenantID, "name": name},
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("failed to provision tenant %s via HTTP: status %d", tenantID, status)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	now := time.Now()
	owner := &auth.User{
		ID:              "user-" + tenantID + "-owner",
		TenantID:        tenantID,
		Email:           email,
		PasswordHash:    hash,
		RoleID:          "owner", // dibuat otomatis oleh tenant.ProvisionTenant
		Status:          auth.UserStatusActive,
		EmailVerifiedAt: &now, // test fixture bypass HTTP — konsisten dengan pola app.go:612/794 (root/admin-provisioned)
	}
	if err := e.userStore.Put(owner); err != nil {
		t.Fatalf("failed to insert bootstrap owner user: %v", err)
	}
}

func (e *testEnv) login(t *testing.T, email, password string) string {
	t.Helper()

	var out struct {
		AccessToken string `json:"access_token"`
	}
	status := e.doJSON(t, fiber.MethodPost, "/api/auth/login", nil,
		map[string]string{"email": email, "password": password}, &out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("login failed for %s: status %d", email, status)
	}
	if out.AccessToken == "" {
		t.Fatalf("login for %s succeeded but returned empty access_token", email)
	}
	return out.AccessToken
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// ============================================================
// E2E TEST 1 & 2 — tenant boundary lewat HTTP request sungguhan.
// ============================================================

func TestE2E_TenantA_CreatesUser_InOwnTenant_Allowed(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-a", "Tenant A", "owner-a@example.test", "correct-horse-battery-staple")

	tokenA := env.login(t, "owner-a@example.test", "correct-horse-battery-staple")

	before := env.userStore.CountForTenant("tenant-a")

	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-a/users", bearer(tokenA),
		map[string]string{"email": "newuser-a@example.test", "password": "another-password", "role_id": "owner"},
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for same-tenant user creation, got %d", status)
	}

	after := env.userStore.CountForTenant("tenant-a")
	if after != before+1 {
		t.Fatalf("expected user count in tenant-a to increase by 1, got before=%d after=%d", before, after)
	}
}

func TestE2E_TenantA_CreatesUser_InTenantB_Denied(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-a", "Tenant A", "owner-a2@example.test", "correct-horse-battery-staple")
	env.bootstrapTenantOwner(t, "tenant-b", "Tenant B", "owner-b@example.test", "correct-horse-battery-staple")

	tokenA := env.login(t, "owner-a2@example.test", "correct-horse-battery-staple")

	beforeB := env.userStore.CountForTenant("tenant-b")

	var out map[string]any
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-b/users", bearer(tokenA),
		map[string]string{"email": "intruder@example.test", "password": "whatever-password", "role_id": "owner"},
		&out,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403 for cross-tenant user creation, got %d", status)
	}
	if out["error"] != "tenant_mismatch" {
		t.Fatalf("expected error tenant_mismatch, got: %v", out)
	}

	afterB := env.userStore.CountForTenant("tenant-b")
	if afterB != beforeB {
		t.Fatalf("SECURITY FAILURE: handler ran despite tenant mismatch — user count in tenant-b changed from %d to %d", beforeB, afterB)
	}
	if _, err := env.userStore.FindByEmail("intruder@example.test"); err == nil {
		t.Fatal("SECURITY FAILURE: intruder user was created in tenant-b despite tenant mismatch")
	}
}

// ============================================================
// E2E TEST 3 — capability denied meski tenant cocok.
// ============================================================

func TestE2E_TenantMatches_ButCapabilityDenied(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-a", "Tenant A", "owner-a3@example.test", "correct-horse-battery-staple")

	if err := env.roleStore.PutRole(&rbac.Role{
		ID:               "limited",
		TenantID:         "tenant-a",
		Name:             "limited",
		CapabilityGrants: []string{"tenant.tenant-a.reports.read"},
	}); err != nil {
		t.Fatalf("PutRole failed: %v", err)
	}

	hash, err := auth.HashPassword("limited-password")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	now := time.Now()
	limitedUser := &auth.User{
		ID: "user-limited-1", TenantID: "tenant-a", Email: "limited@example.test",
		PasswordHash: hash, RoleID: "limited", Status: auth.UserStatusActive,
		EmailVerifiedAt: &now,
	}
	if err := env.userStore.Put(limitedUser); err != nil {
		t.Fatalf("failed to insert limited user: %v", err)
	}

	tokenLimited := env.login(t, "limited@example.test", "limited-password")

	before := env.userStore.CountForTenant("tenant-a")

	var out map[string]any
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-a/users", bearer(tokenLimited),
		map[string]string{"email": "shouldnotexist@example.test", "password": "whatever-password", "role_id": "owner"},
		&out,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403 for capability-denied user creation, got %d", status)
	}
	if out["error"] != "capability_denied" {
		t.Fatalf("expected error capability_denied, got: %v", out)
	}

	after := env.userStore.CountForTenant("tenant-a")
	if after != before {
		t.Fatalf("SECURITY FAILURE: handler ran despite missing capability — user count changed from %d to %d", before, after)
	}
	if _, err := env.userStore.FindByEmail("shouldnotexist@example.test"); err == nil {
		t.Fatal("SECURITY FAILURE: user was created despite missing capability")
	}
}

// ============================================================
// E2E TEST 4 & 5 — root credential valid/invalid.
// ============================================================

func TestE2E_RootCredential_Valid_ProvisionsTenant(t *testing.T) {
	env := newTestEnv(t)

	var out map[string]any
	status := env.doJSON(t, fiber.MethodPost, "/api/admin/tenants",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		map[string]string{"tenant_id": "tenant-root-valid", "name": "Root Valid Test"},
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for valid root credential, got %d, body: %v", status, out)
	}

	if _, err := env.tenantStore.Get("tenant-root-valid"); err != nil {
		t.Fatalf("CORRECTNESS FAILURE: tenant not actually provisioned despite 200 response: %v", err)
	}
}

func TestE2E_RootCredential_Invalid_TenantNotProvisioned(t *testing.T) {
	env := newTestEnv(t)

	status := env.doJSON(t, fiber.MethodPost, "/api/admin/tenants",
		map[string]string{"X-Root-API-Key": "wrong-root-key"},
		map[string]string{"tenant_id": "tenant-root-invalid", "name": "Root Invalid Test"},
		nil,
	)
	if status == fiber.StatusOK {
		t.Fatalf("expected non-200 for invalid root credential, got %d", status)
	}

	if _, err := env.tenantStore.Get("tenant-root-invalid"); err == nil {
		t.Fatal("SECURITY FAILURE: tenant was provisioned despite invalid root credential")
	}
}

func TestE2E_RootCredential_Missing_TenantNotProvisioned(t *testing.T) {
	env := newTestEnv(t)

	status := env.doJSON(t, fiber.MethodPost, "/api/admin/tenants", nil,
		map[string]string{"tenant_id": "tenant-root-missing", "name": "Root Missing Test"}, nil,
	)
	if status == fiber.StatusOK {
		t.Fatalf("expected non-200 for missing root credential, got %d", status)
	}
	if _, err := env.tenantStore.Get("tenant-root-missing"); err == nil {
		t.Fatal("SECURITY FAILURE: tenant was provisioned despite missing root credential")
	}
}

// ============================================================
// E2E TEST 6 — unauthenticated request tidak pernah sampai DB.
// ============================================================

func TestE2E_FullChain_UnauthenticatedRequest_NeverReachesDatabase(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-a", "Tenant A", "owner-a4@example.test", "correct-horse-battery-staple")

	before := env.userStore.CountForTenant("tenant-a")

	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-a/users", nil,
		map[string]string{"email": "noauth@example.test", "password": "whatever-password", "role_id": "owner"},
		nil,
	)
	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", status)
	}

	after := env.userStore.CountForTenant("tenant-a")
	if after != before {
		t.Fatalf("SECURITY FAILURE: handler ran despite missing authentication — user count changed from %d to %d", before, after)
	}
}
