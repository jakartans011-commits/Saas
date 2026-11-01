package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestE2E_AccessTokenReplayAfterLogoutRejected(t *testing.T) {
	env := newTestEnv(t)

	tenantID := "tenant-security-replay-1"
	email := "replay-owner@test.local"
	password := "ReplayPassword-001!"

	provisionTenantOnly(t, env, tenantID, "Security Replay Tenant")

	status, _ := env.bootstrapOwnerViaHTTP(
		t,
		tenantID,
		email,
		password,
		map[string]string{
			"X-Root-API-Key": e2eRootKey,
		},
	)

	if status != fiber.StatusOK {
		t.Fatalf("bootstrap owner failed: got status %d", status)
	}

	token := env.login(t, email, password)

	if token == "" {
		t.Fatal("expected non-empty access token")
	}

	// ------------------------------------------------------------
	// 1. Token masih valid sebelum logout.
	// ------------------------------------------------------------

	var beforeLogout map[string]any

	status = env.doJSON(
		t,
		fiber.MethodGet,
		"/api/tenants/"+tenantID+"/usage",
		bearer(token),
		nil,
		&beforeLogout,
	)

	if status != fiber.StatusOK {
		t.Fatalf(
			"expected access token to work before logout, got %d, body: %v",
			status,
			beforeLogout,
		)
	}

	// ------------------------------------------------------------
	// 2. Logout token tersebut.
	// ------------------------------------------------------------

	var logoutBody map[string]any

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/api/auth/logout",
		bearer(token),
		nil,
		&logoutBody,
	)

	if status != fiber.StatusOK {
		t.Fatalf(
			"expected logout 200, got %d, body: %v",
			status,
			logoutBody,
		)
	}

	if logoutBody["status"] != "logged_out" {
		t.Fatalf(
			"expected status=logged_out, got: %v",
			logoutBody,
		)
	}

	// ------------------------------------------------------------
	// 3. REPLAY ATTACK:
	//    gunakan kembali access token yang sama.
	// ------------------------------------------------------------

	var replayBody map[string]any

	status = env.doJSON(
		t,
		fiber.MethodGet,
		"/api/tenants/"+tenantID+"/usage",
		bearer(token),
		nil,
		&replayBody,
	)

	if status != fiber.StatusUnauthorized {
		t.Fatalf(
			"SECURITY FAILURE: revoked access token was accepted on replay, got %d, body: %v",
			status,
			replayBody,
		)
	}

	if replayBody["error"] != "token_revoked" {
		t.Fatalf(
			"expected token_revoked after replay, got: %v",
			replayBody,
		)
	}
}
