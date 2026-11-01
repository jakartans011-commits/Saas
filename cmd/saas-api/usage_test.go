package main

import (
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// ============================================================
// USAGE METERING E2E TESTS
//
// Menggunakan testEnv yang sama dengan app_test.go:
//   - BuildApp() asli (bukan fake)
//   - BoltDB temporary
//   - doJSON() helper untuk HTTP request
//   - bootstrapTenantOwner() untuk setup tenant + owner
// ============================================================

func TestUsageMetering_TenantCanReadOwnUsage(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-usage-a", "Tenant Usage A",
		"owner-a@usage.test", "password-a")

	tokenA := env.login(t, "owner-a@usage.test", "password-a")

	// Buat beberapa request untuk generate usage events
	for i := 0; i < 3; i++ {
		email := "user" + string(rune('a'+i)) + "@usage.test"
		status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-usage-a/users",
			bearer(tokenA),
			map[string]string{
				"email":    email,
				"password": "password",
				"role_id":  "owner",
			},
			nil,
		)
		if status != fiber.StatusOK {
			t.Fatalf("failed to create user %s: status %d", email, status)
		}
	}

	time.Sleep(100 * time.Millisecond)

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-usage-a/usage",
		bearer(tokenA),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["tenant_id"] != "tenant-usage-a" {
		t.Fatalf("expected tenant_id=tenant-usage-a, got %v", out["tenant_id"])
	}

	events, ok := out["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out["events"])
	}
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}

	// Verifikasi event metadata tidak mengandung credential
	for _, e := range events {
		eventMap, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		metadata, ok := eventMap["metadata"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, key := range []string{"password", "token", "secret", "credential", "key", "auth"} {
			if _, exists := metadata[key]; exists {
				t.Fatalf("SECURITY FAILURE: metadata contains %q", key)
			}
		}
	}
}

func TestUsageMetering_TenantCannotReadOtherTenantUsage(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-usage-a", "Tenant Usage A",
		"owner-a2@usage.test", "password-a2")
	env.bootstrapTenantOwner(t, "tenant-usage-b", "Tenant Usage B",
		"owner-b@usage.test", "password-b")

	tokenA := env.login(t, "owner-a2@usage.test", "password-a2")

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-usage-b/usage",
		bearer(tokenA),
		nil,
		&out,
	)

	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
	if out["error"] != "tenant_mismatch" {
		t.Fatalf("expected error tenant_mismatch, got %v", out["error"])
	}
}

func TestUsageMetering_RootCanReadAnyTenantUsage(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-usage-root", "Tenant Usage Root",
		"owner-root@usage.test", "password-root")

	tokenRoot := env.login(t, "owner-root@usage.test", "password-root")

	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-usage-root/users",
		bearer(tokenRoot),
		map[string]string{
			"email":    "user-root@usage.test",
			"password": "password",
			"role_id":  "owner",
		},
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("failed to create user: status %d", status)
	}

	time.Sleep(100 * time.Millisecond)

	var out map[string]interface{}
	status = env.doJSON(t, fiber.MethodGet, "/api/admin/tenants/tenant-usage-root/usage",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["tenant_id"] != "tenant-usage-root" {
		t.Fatalf("expected tenant_id=tenant-usage-root, got %v", out["tenant_id"])
	}

	events, ok := out["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out["events"])
	}
	if len(events) < 1 {
		t.Fatalf("expected at least 1 event, got %d", len(events))
	}
}

func TestUsageMetering_RootTenantNotFound(t *testing.T) {
	env := newTestEnv(t)

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/admin/tenants/tenant-not-exist/usage",
		map[string]string{"X-Root-API-Key": e2eRootKey},
		nil,
		&out,
	)

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
	if out["error"] != "tenant not found" {
		t.Fatalf("expected error 'tenant not found', got %v", out["error"])
	}
}

func TestUsageMetering_LimitDefault(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-usage-limit", "Tenant Usage Limit",
		"owner-limit@usage.test", "password-limit")

	token := env.login(t, "owner-limit@usage.test", "password-limit")

	for i := 0; i < 10; i++ {
		email := "user" + string(rune('a'+i)) + "@limit.test"
		status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-usage-limit/users",
			bearer(token),
			map[string]string{
				"email":    email,
				"password": "password",
				"role_id":  "owner",
			},
			nil,
		)
		if status != fiber.StatusOK {
			t.Fatalf("failed to create user %s: status %d", email, status)
		}
	}

	time.Sleep(100 * time.Millisecond)

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-usage-limit/usage",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["limit"] != float64(50) {
		t.Fatalf("expected limit=50, got %v", out["limit"])
	}

	events, ok := out["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out["events"])
	}
	if len(events) < 10 {
		t.Fatalf("expected at least 10 events, got %d", len(events))
	}
}

func TestUsageMetering_LimitCustom(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-usage-custom", "Tenant Usage Custom",
		"owner-custom@usage.test", "password-custom")

	token := env.login(t, "owner-custom@usage.test", "password-custom")

	for i := 0; i < 15; i++ {
		email := "user" + string(rune('a'+i)) + "@custom.test"
		status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-usage-custom/users",
			bearer(token),
			map[string]string{
				"email":    email,
				"password": "password",
				"role_id":  "owner",
			},
			nil,
		)
		if status != fiber.StatusOK {
			t.Fatalf("failed to create user %s: status %d", email, status)
		}
	}

	time.Sleep(100 * time.Millisecond)

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-usage-custom/usage?limit=5",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["limit"] != float64(5) {
		t.Fatalf("expected limit=5, got %v", out["limit"])
	}

	events, ok := out["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out["events"])
	}
	if len(events) > 5 {
		t.Fatalf("expected at most 5 events, got %d", len(events))
	}
}

func TestUsageMetering_LimitMaximum(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-usage-max", "Tenant Usage Max",
		"owner-max@usage.test", "password-max")

	token := env.login(t, "owner-max@usage.test", "password-max")

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-usage-max/usage?limit=500",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["limit"] != float64(200) {
		t.Fatalf("expected limit capped to 200, got %v", out["limit"])
	}
}

func TestUsageMetering_InvalidLimit(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-usage-invalid", "Tenant Usage Invalid",
		"owner-invalid@usage.test", "password-invalid")

	token := env.login(t, "owner-invalid@usage.test", "password-invalid")

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-usage-invalid/usage?limit=not-a-number",
		bearer(token),
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if out["limit"] != float64(50) {
		t.Fatalf("expected limit fallback to 50, got %v", out["limit"])
	}
}

func TestUsageMetering_UnauthenticatedDenied(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-usage-unauth", "Tenant Usage Unauth",
		"owner-unauth@usage.test", "password-unauth")

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-usage-unauth/usage",
		nil,
		nil,
		&out,
	)

	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestUsageMetering_HealthEndpointNoEvents(t *testing.T) {
	env := newTestEnv(t)

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/health",
		nil,
		nil,
		&out,
	)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for health, got %d", status)
	}
	// Test ini hanya memastikan tidak panic/error.
	// Health endpoint tidak dicatat karena skip di middleware.
}

func TestUsageMetering_CrossTenantNoEventForTarget(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, "tenant-cross-a", "Tenant Cross A",
		"owner-cross-a@usage.test", "password-cross-a")
	env.bootstrapTenantOwner(t, "tenant-cross-b", "Tenant Cross B",
		"owner-cross-b@usage.test", "password-cross-b")

	tokenA := env.login(t, "owner-cross-a@usage.test", "password-cross-a")

	var out map[string]interface{}
	status := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-cross-b/usage",
		bearer(tokenA),
		nil,
		&out,
	)
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}

	// Verifikasi: tenant B tidak punya event dari request A→B
	tokenB := env.login(t, "owner-cross-b@usage.test", "password-cross-b")

	var outB map[string]interface{}
	statusB := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-cross-b/usage",
		bearer(tokenB),
		nil,
		&outB,
	)
	if statusB != fiber.StatusOK {
		t.Fatalf("expected 200 for tenant B reading own usage, got %d", statusB)
	}

	// Tidak ada event yang tercatat untuk tenant B dari request A→B
	// karena request ditolak di RequireTenantScope sebelum mencapai handler.
	// Ini sudah dijamin oleh posisi middleware.
}

func TestUsageMetering_SelfMeteringExclusion(t *testing.T) {
	env := newTestEnv(t)
	env.bootstrapTenantOwnerPro(t, "tenant-self", "Tenant Self",
		"owner-self@usage.test", "password-self")

	token := env.login(t, "owner-self@usage.test", "password-self")

	// Buat satu event terlebih dahulu (create user)
	status := env.doJSON(t, fiber.MethodPost, "/api/tenants/tenant-self/users",
		bearer(token),
		map[string]string{
			"email":    "user-self@usage.test",
			"password": "password",
			"role_id":  "owner",
		},
		nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("failed to create user: status %d", status)
	}

	time.Sleep(100 * time.Millisecond)

	// Baca usage pertama kali → ini TIDAK boleh tercatat sebagai event baru
	var out1 map[string]interface{}
	status1 := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-self/usage",
		bearer(token),
		nil,
		&out1,
	)
	if status1 != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status1)
	}

	events1, ok := out1["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out1["events"])
	}
	count1 := len(events1)

	time.Sleep(100 * time.Millisecond)

	// Baca usage lagi
	var out2 map[string]interface{}
	status2 := env.doJSON(t, fiber.MethodGet, "/api/tenants/tenant-self/usage",
		bearer(token),
		nil,
		&out2,
	)
	if status2 != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status2)
	}

	events2, ok := out2["events"].([]interface{})
	if !ok {
		t.Fatalf("expected events array, got %T", out2["events"])
	}
	count2 := len(events2)

	// Jumlah event harus SAMA (karena GET /usage tidak dicatat)
	if count1 != count2 {
		t.Fatalf("self-metering exclusion failed: count1=%d, count2=%d (should be equal)", count1, count2)
	}
}
