// cmd/saas-api/app_bootstrap_owner_test.go
package main

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// bootstrapOwnerViaHTTP membantu memanggil endpoint bootstrap-owner
// lewat HTTP sungguhan (bukan bypass store langsung seperti
// bootstrapTenantOwner di app_test.go — test ini SPESIFIK menguji
// endpoint-nya sendiri).
func (e *testEnv) bootstrapOwnerViaHTTP(
	t *testing.T, tenantID, email, password string, rootHeader map[string]string,
) (int, map[string]any) {
	t.Helper()
	var out map[string]any
	status := e.doJSON(t, fiber.MethodPost,
		"/api/admin/tenants/"+tenantID+"/bootstrap-owner",
		rootHeader,
		map[string]string{"email": email, "password": password},
		&out,
	)
	return status, out
}

func provisionTenantOnly(t *testing.T, e *testEnv, tenantID, name string) {
	t.Helper()
	status := e.doJSON(t, fiber.MethodPost, "/api/admin/tenants",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		map[string]string{"tenant_id": tenantID, "name": name}, nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("failed to provision tenant %s: status %d", tenantID, status)
	}
}

// TEST 1: root valid -> bootstrap owner berhasil.
func TestBootstrapOwner_RootValid_Success(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-1", "Tenant BO 1")

	status, out := env.bootstrapOwnerViaHTTP(t, "tenant-bo-1", "owner1@bo.test", "bootstrap-password-1",
		map[string]string{"X-Root-API-Key": e2eRootKey})

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d, body: %v", status, out)
	}
	userObj, ok := out["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user object in response, got: %v", out)
	}
	if userObj["role_id"] != "owner" {
		t.Fatalf("expected role_id=owner, got: %v", userObj["role_id"])
	}
}

// TEST 2: root invalid -> denied, tidak ada user dibuat.
func TestBootstrapOwner_RootInvalid_Denied(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-2", "Tenant BO 2")

	before := env.userStore.CountForTenant("tenant-bo-2")

	status, _ := env.bootstrapOwnerViaHTTP(t, "tenant-bo-2", "owner2@bo.test", "bootstrap-password-2",
		map[string]string{"X-Root-API-Key": "wrong-key"})

	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
	if after := env.userStore.CountForTenant("tenant-bo-2"); after != before {
		t.Fatalf("SECURITY FAILURE: user created despite invalid root credential")
	}
}

// TEST 3: root credential hilang -> denied.
func TestBootstrapOwner_RootMissing_Denied(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-3", "Tenant BO 3")

	status, _ := env.bootstrapOwnerViaHTTP(t, "tenant-bo-3", "owner3@bo.test", "bootstrap-password-3", nil)

	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

// TEST 4: tenant tidak ada -> denied 404.
func TestBootstrapOwner_TenantNotFound_Denied(t *testing.T) {
	env := newTestEnv(t)

	status, out := env.bootstrapOwnerViaHTTP(t, "tenant-does-not-exist", "x@x.test", "password-x",
		map[string]string{"X-Root-API-Key": e2eRootKey})

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d, body: %v", status, out)
	}
	if out["error"] != "tenant_not_found" {
		t.Fatalf("expected error tenant_not_found, got: %v", out)
	}
}

// TEST 5: tenant sudah punya owner -> conflict, TIDAK overwrite.
func TestBootstrapOwner_AlreadyHasOwner_Conflict(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-5", "Tenant BO 5")

	status1, _ := env.bootstrapOwnerViaHTTP(t, "tenant-bo-5", "first@bo5.test", "password-first",
		map[string]string{"X-Root-API-Key": e2eRootKey})
	if status1 != fiber.StatusOK {
		t.Fatalf("first bootstrap should succeed, got %d", status1)
	}

	before := env.userStore.CountForTenant("tenant-bo-5")

	status2, out2 := env.bootstrapOwnerViaHTTP(t, "tenant-bo-5", "second@bo5.test", "password-second",
		map[string]string{"X-Root-API-Key": e2eRootKey})

	if status2 != fiber.StatusConflict {
		t.Fatalf("expected 409 on second bootstrap attempt, got %d, body: %v", status2, out2)
	}
	if out2["error"] != "tenant_owner_already_exists" {
		t.Fatalf("expected error tenant_owner_already_exists, got: %v", out2)
	}

	// IDEMPOTENCY: pastikan TIDAK ada owner kedua diam-diam dibuat.
	after := env.userStore.CountForTenant("tenant-bo-5")
	if after != before {
		t.Fatalf("SECURITY FAILURE: second owner was silently created — count changed %d -> %d", before, after)
	}
	if _, err := env.userStore.FindByEmail("second@bo5.test"); err == nil {
		t.Fatal("SECURITY FAILURE: second owner email was registered despite conflict")
	}
}

// TEST 6: duplicate email (dipakai tenant lain) -> conflict.
func TestBootstrapOwner_DuplicateEmail_Conflict(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-6a", "Tenant BO 6A")
	provisionTenantOnly(t, env, "tenant-bo-6b", "Tenant BO 6B")

	status1, _ := env.bootstrapOwnerViaHTTP(t, "tenant-bo-6a", "shared@bo6.test", "password-a",
		map[string]string{"X-Root-API-Key": e2eRootKey})
	if status1 != fiber.StatusOK {
		t.Fatalf("first bootstrap should succeed, got %d", status1)
	}

	status2, out2 := env.bootstrapOwnerViaHTTP(t, "tenant-bo-6b", "shared@bo6.test", "password-b",
		map[string]string{"X-Root-API-Key": e2eRootKey})

	if status2 != fiber.StatusConflict {
		t.Fatalf("expected 409 for duplicate email, got %d, body: %v", status2, out2)
	}
	if out2["error"] != "email_already_registered" {
		t.Fatalf("expected error email_already_registered, got: %v", out2)
	}
}

// TEST 7, 8, 9: password disimpan bcrypt, tidak bocor di response
// (baik plaintext maupun hash).
func TestBootstrapOwner_PasswordNeverLeaksAndIsHashed(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-7", "Tenant BO 7")

	plaintextPassword := "super-secret-owner-password"
	status, out := env.bootstrapOwnerViaHTTP(t, "tenant-bo-7", "owner7@bo.test", plaintextPassword,
		map[string]string{"X-Root-API-Key": e2eRootKey})
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	userObj := out["user"].(map[string]any)
	if _, hasPassword := userObj["password"]; hasPassword {
		t.Fatal("SECURITY FAILURE: response contains 'password' field")
	}
	if _, hasHash := userObj["password_hash"]; hasHash {
		t.Fatal("SECURITY FAILURE: response contains 'password_hash' field")
	}

	// Storage-level check: password TIDAK plaintext, TIDAK muncul
	// mentah, dan bcrypt hash format ("$2a$"/"$2b$" prefix).
	stored, err := env.userStore.FindByEmail("owner7@bo.test")
	if err != nil {
		t.Fatalf("FindByEmail failed: %v", err)
	}
	if stored.PasswordHash == plaintextPassword {
		t.Fatal("SECURITY FAILURE: password stored as plaintext")
	}
	if !strings.HasPrefix(stored.PasswordHash, "$2a$") && !strings.HasPrefix(stored.PasswordHash, "$2b$") {
		t.Fatalf("expected bcrypt hash format, got: %s", stored.PasswordHash)
	}
}

// TEST 10: owner hasil bootstrap dapat login lewat mekanisme
// LOGIN EXISTING (bukan mekanisme baru).
func TestBootstrapOwner_CanLoginViaExistingFlow(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-10", "Tenant BO 10")

	status, _ := env.bootstrapOwnerViaHTTP(t, "tenant-bo-10", "owner10@bo.test", "login-test-password",
		map[string]string{"X-Root-API-Key": e2eRootKey})
	if status != fiber.StatusOK {
		t.Fatalf("bootstrap should succeed, got %d", status)
	}

	// env.login() memanggil POST /api/auth/login yang SUDAH ADA,
	// tidak dimodifikasi sama sekali untuk fitur ini.
	token := env.login(t, "owner10@bo.test", "login-test-password")
	if token == "" {
		t.Fatal("expected non-empty access token after login")
	}
}

// TEST 11, 12, 13: owner mendapat role owner yang benar, capability
// tenant yang benar, dan TIDAK bisa akses tenant lain (cross-tenant).
func TestBootstrapOwner_HasCorrectRoleAndTenantBoundary(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-11a", "Tenant BO 11A")
	provisionTenantOnly(t, env, "tenant-bo-11b", "Tenant BO 11B")

	statusA, _ := env.bootstrapOwnerViaHTTP(t, "tenant-bo-11a", "ownerA@bo11.test", "password-a11",
		map[string]string{"X-Root-API-Key": e2eRootKey})
	if statusA != fiber.StatusOK {
		t.Fatalf("bootstrap tenant A should succeed, got %d", statusA)
	}

	// Tenant A harus plan Pro agar boleh punya lebih dari satu user
	// (owner hasil bootstrap + member-a di bawah).
	tnA, err := env.tenantStore.Get("tenant-bo-11a")
	if err != nil {
		t.Fatalf("get tenant tenant-bo-11a: %v", err)
	}
	tnA.PlanTier = "pro"
	if err := env.tenantStore.Put(tnA); err != nil {
		t.Fatalf("set plan pro for tenant tenant-bo-11a: %v", err)
	}

	tokenA := env.login(t, "ownerA@bo11.test", "password-a11")

	// Owner Tenant A BOLEH create user di tenant sendiri (role owner
	// = capability tenant.<A>.* mencakup users.create).
	statusOwn := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-bo-11a/users", bearer(tokenA),
		map[string]string{"email": "member-a@bo11.test", "password": "password-member", "role_id": "owner"}, nil)
	if statusOwn != fiber.StatusOK {
		t.Fatalf("owner should be able to create user in own tenant, got %d", statusOwn)
	}

	// Owner Tenant A TIDAK BOLEH create user di tenant B - cross-tenant
	// boundary (Section: security test paling kritis di spesifikasi).
	var outCross map[string]any
	statusCross := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-bo-11b/users", bearer(tokenA),
		map[string]string{"email": "intruder@bo11.test", "password": "whatever", "role_id": "owner"}, &outCross)
	if statusCross != fiber.StatusForbidden {
		t.Fatalf("expected 403 for cross-tenant access, got %d", statusCross)
	}
	if outCross["error"] != "tenant_mismatch" {
		t.Fatalf("expected tenant_mismatch, got: %v", outCross)
	}
}

// TEST 14a: owner Tenant A TIDAK BOLEH memanggil bootstrap-owner
// untuk TENANT LAIN (Tenant B) — ini root-only operation.
func TestBootstrapOwner_TenantOwnerCannotBootstrapOtherTenant(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-14a", "Tenant BO 14A")
	provisionTenantOnly(t, env, "tenant-bo-14b", "Tenant BO 14B")

	statusA, _ := env.bootstrapOwnerViaHTTP(t, "tenant-bo-14a", "ownerA14@bo.test", "password-a14",
		map[string]string{"X-Root-API-Key": e2eRootKey})
	if statusA != fiber.StatusOK {
		t.Fatalf("bootstrap tenant A should succeed, got %d", statusA)
	}
	tokenA := env.login(t, "ownerA14@bo.test", "password-a14")

	before := env.userStore.CountForTenant("tenant-bo-14b")

	// Owner Tenant A mencoba panggil bootstrap-owner untuk Tenant B,
	// pakai JWT tenant biasa (BUKAN X-Root-API-Key) — harus DITOLAK
	// oleh requireRoot (yang hanya cek header X-Root-API-Key, sama
	// sekali tidak peduli Authorization/JWT).
	status, _ := env.doJSONStatusOnly(t, fiber.MethodPost,
		"/api/admin/tenants/tenant-bo-14b/bootstrap-owner",
		bearer(tokenA), // JWT valid, TAPI bukan root credential
		map[string]string{"email": "sneaky@bo14.test", "password": "sneaky-password"})

	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 (JWT is not root credential), got %d", status)
	}
	if after := env.userStore.CountForTenant("tenant-bo-14b"); after != before {
		t.Fatal("SECURITY FAILURE: tenant-user JWT was able to bootstrap owner for another tenant")
	}
}

// TEST 14b: owner Tenant A TIDAK BOLEH memanggil bootstrap-owner
// untuk TENANT SENDIRI juga — endpoint ini root-only sepenuhnya,
// bukan operasi tenant-owner biasa (Section: khusus diminta di spec).
func TestBootstrapOwner_TenantOwnerCannotBootstrapOwnTenant(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-14c", "Tenant BO 14C")

	statusFirst, _ := env.bootstrapOwnerViaHTTP(t, "tenant-bo-14c", "first14c@bo.test", "password-first14c",
		map[string]string{"X-Root-API-Key": e2eRootKey})
	if statusFirst != fiber.StatusOK {
		t.Fatalf("initial bootstrap should succeed, got %d", statusFirst)
	}
	tokenOwn := env.login(t, "first14c@bo.test", "password-first14c")

	status, _ := env.doJSONStatusOnly(t, fiber.MethodPost,
		"/api/admin/tenants/tenant-bo-14c/bootstrap-owner",
		bearer(tokenOwn), // JWT tenant sendiri, BUKAN root
		map[string]string{"email": "second14c@bo.test", "password": "password-second14c"})

	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 (own-tenant JWT is not root credential), got %d", status)
	}
}

// TEST 15: malformed request -> 400.
func TestBootstrapOwner_MalformedRequest_BadRequest(t *testing.T) {
	env := newTestEnv(t)
	provisionTenantOnly(t, env, "tenant-bo-15", "Tenant BO 15")

	// Body kosong (tanpa email/password).
	status, out := env.doJSONStatusOnly(t, fiber.MethodPost,
		"/api/admin/tenants/tenant-bo-15/bootstrap-owner",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		map[string]string{},
	)
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for missing email/password, got %d, body: %v", status, out)
	}
}

// doJSONStatusOnly — varian doJSON yang mengembalikan status DAN
// body ter-decode sebagai map, untuk test yang butuh keduanya tanpa
// perlu struct out terpisah tiap kali.
func (e *testEnv) doJSONStatusOnly(
	t *testing.T, method, path string, headers map[string]string, body any,
) (int, map[string]any) {
	t.Helper()
	var out map[string]any
	status := e.doJSON(t, method, path, headers, body, &out)
	return status, out
}
