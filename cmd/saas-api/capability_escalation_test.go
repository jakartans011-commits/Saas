package main

import (
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/auth"
	"trailseal/rbac"
)

// seedLimitedRoleUser membuat role dengan capability terbatas beserta satu
// user yang memakainya, lewat store langsung — pola yang sama dengan
// bootstrapTenantOwner, sehingga test ini tidak bergantung pada endpoint
// manajemen role yang bisa berubah.
func (e *testEnv) seedLimitedRoleUser(
	t *testing.T,
	tenantID, roleID, email, password string,
	grants []string,
) {
	t.Helper()

	if err := e.roleStore.PutRole(&rbac.Role{
		ID:               roleID,
		TenantID:         tenantID,
		Name:             roleID,
		CapabilityGrants: grants,
	}); err != nil {
		t.Fatalf("PutRole failed: %v", err)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	now := time.Now()
	if err := e.userStore.Put(&auth.User{
		ID:              "user-" + tenantID + "-" + roleID,
		TenantID:        tenantID,
		Email:           email,
		PasswordHash:    hash,
		RoleID:          roleID,
		Status:          auth.UserStatusActive,
		EmailVerifiedAt: &now,
	}); err != nil {
		t.Fatalf("failed to insert limited-role user: %v", err)
	}
}

// TestCapability_Escalation_UserCannotBypassCapability membuktikan
// RequireCapability benar-benar menahan user yang rolenya tidak memiliki
// tenant.<id>.agent.write.
//
// Dua kontrol positif menjaga test ini agar tidak lulus karena alasan yang
// salah: user terbatas harus bisa MEMBACA agent (membuktikan tokennya sah
// dan auth bekerja), dan owner tidak boleh menerima 403 pada body yang sama
// (membuktikan penolakan datang dari capability, bukan dari validasi payload).
func TestCapability_Escalation_UserCannotBypassCapability(t *testing.T) {
	const tenantID = "tenant-cap-escalation"

	env := newTestEnv(t)
	env.bootstrapTenantOwner(t, tenantID, "Cap Escalation",
		"owner-capesc@test.test", "password")

	env.seedLimitedRoleUser(t, tenantID, "limited-reader",
		"reader-capesc@test.test", "password",
		[]string{"tenant." + tenantID + ".agent.read"},
	)

	ownerToken := env.login(t, "owner-capesc@test.test", "password")
	readerToken := env.login(t, "reader-capesc@test.test", "password")

	agentsPath := "/api/tenants/" + tenantID + "/agents"
	body := map[string]string{"name": "probe-agent"}

	// Kontrol positif 1: token user terbatas sah, capability read-nya jalan.
	if status := env.doJSON(t, fiber.MethodGet, agentsPath,
		bearer(readerToken), nil, nil); status != fiber.StatusOK {
		t.Fatalf("user terbatas seharusnya bisa membaca agent, got %d", status)
	}

	// Inti pembuktian: tanpa agent.write, pembuatan agent harus ditolak 403.
	if status := env.doJSON(t, fiber.MethodPost, agentsPath,
		bearer(readerToken), body, nil); status != fiber.StatusForbidden {
		t.Fatalf("SECURITY FAILURE: user tanpa agent.write mendapat status %d, want 403", status)
	}

	// Kontrol positif 2: owner dengan body yang sama tidak boleh kena 403.
	if status := env.doJSON(t, fiber.MethodPost, agentsPath,
		bearer(ownerToken), body, nil); status == fiber.StatusForbidden {
		t.Fatal("owner ikut tertolak 403; penolakan di atas bukan karena capability")
	}
}
