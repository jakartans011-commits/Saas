package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestAPIKeyLimit_FreePlanOneActiveKey(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	tenantID := fmt.Sprintf("tenant-apikey-limit-%d", time.Now().UnixNano())
	email := tenantID + "@example.test"
	password := "apikey-limit-password"

	env.bootstrapTenantOwner(t, tenantID, "API Key Limit Tenant", email, password) // plan default = Free
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = env.pgPool.Exec(ctx, `DELETE FROM api_keys WHERE tenant_id = $1`, tenantID)
		_, _ = env.pgPool.Exec(ctx, `DELETE FROM projects WHERE tenant_id = $1`, tenantID)
	})

	token := env.login(t, email, password)
	projectID := createProjectForAPIKeyTest(t, env, token, tenantID, "Limit Project")

	// Key pertama lolos.
	first := createAPIKeyForTest(t, env, token, tenantID, projectID, "key-1", "ingest")

	// Key kedua ditolak 403 api_key_limit_reached.
	var denied struct {
		Error string `json:"error"`
		Limit int    `json:"limit"`
	}
	status := env.doJSON(t, fiber.MethodPost,
		"/api/tenants/"+tenantID+"/projects/"+projectID+"/keys",
		bearer(token),
		map[string]string{"name": "key-2", "scope": "ingest"},
		&denied,
	)
	if status != fiber.StatusForbidden || denied.Error != "api_key_limit_reached" || denied.Limit != 1 {
		t.Fatalf("key kedua Free: status=%d body=%+v, want 403 api_key_limit_reached limit=1", status, denied)
	}

	// Setelah revoke, bisa membuat lagi.
	status = env.doJSON(t, fiber.MethodPost,
		"/api/tenants/"+tenantID+"/projects/"+projectID+"/keys/"+first.APIKey.ID+"/revoke",
		bearer(token), nil, nil,
	)
	if status != fiber.StatusOK {
		t.Fatalf("revoke gagal: status=%d", status)
	}
	createAPIKeyForTest(t, env, token, tenantID, projectID, "key-3", "ingest")
}
