package main

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func createProjectForAPIKeyTest(
	t *testing.T,
	env *testEnv,
	token string,
	tenantID string,
	name string,
) string {
	t.Helper()

	var out struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}

	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/"+tenantID+"/projects",
		bearer(token),
		map[string]any{
			"name": name,
		},
		&out,
	)

	if status != fiber.StatusCreated {
		t.Fatalf(
			"project creation failed: status=%d body=%v",
			status,
			out,
		)
	}

	if out.Project.ID == "" {
		t.Fatal("project creation returned empty project id")
	}

	return out.Project.ID
}

type apiKeyTestResponse struct {
	APIKey struct {
		ID        string `json:"id"`
		ProjectID string `json:"project_id"`
		TenantID  string `json:"tenant_id"`
		KeyPrefix string `json:"key_prefix"`
		Scope     string `json:"scope"`
		KeyHash   string `json:"key_hash"`
	} `json:"api_key"`

	Key     string `json:"key"`
	Warning string `json:"warning"`
}

func createAPIKeyForTest(
	t *testing.T,
	env *testEnv,
	token string,
	tenantID string,
	projectID string,
	name string,
	scope string,
) apiKeyTestResponse {
	t.Helper()

	var out apiKeyTestResponse

	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/"+tenantID+"/projects/"+projectID+"/keys",
		bearer(token),
		map[string]string{
			"name":  name,
			"scope": scope,
		},
		&out,
	)

	if status != fiber.StatusCreated {
		t.Fatalf(
			"api key creation failed: status=%d body=%v",
			status,
			out,
		)
	}

	if out.Key == "" {
		t.Fatal("API key plaintext was not returned on creation")
	}

	if !strings.HasPrefix(out.Key, "tsk_") {
		t.Fatalf("unexpected API key prefix: %q", out.Key)
	}

	if out.APIKey.ID == "" {
		t.Fatal("API key response returned empty id")
	}

	if out.APIKey.ProjectID != projectID {
		t.Fatalf(
			"API key project mismatch: expected=%s got=%s",
			projectID,
			out.APIKey.ProjectID,
		)
	}

	if out.APIKey.TenantID != tenantID {
		t.Fatalf(
			"API key tenant mismatch: expected=%s got=%s",
			tenantID,
			out.APIKey.TenantID,
		)
	}

	return out
}

func TestAPIKeySecurity_CreateAndOneTimeDisclosure(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	tenantID := "tenant-apikey-create"
	email := "apikey-create-owner@example.test"
	password := "apikey-create-password"

	env.bootstrapTenantOwnerPro(
		t,
		tenantID,
		"API Key Create Tenant",
		email,
		password,
	)

	token := env.login(t, email, password)

	projectID := createProjectForAPIKeyTest(
		t,
		env,
		token,
		tenantID,
		"API Key Create Project",
	)

	key := createAPIKeyForTest(
		t,
		env,
		token,
		tenantID,
		projectID,
		"ingest-key",
		"ingest",
	)

	if key.Warning == "" {
		t.Fatal("expected one-time disclosure warning")
	}

	// Plaintext must never be exposed by the list endpoint.
	var list struct {
		APIKeys []map[string]any `json:"api_keys"`
	}

	status := env.doJSON(
		t,
		fiber.MethodGet,
		"/api/tenants/"+tenantID+"/projects/"+projectID+"/keys",
		bearer(token),
		nil,
		&list,
	)

	if status != fiber.StatusOK {
		t.Fatalf("list API keys failed: status=%d body=%v", status, list)
	}

	if len(list.APIKeys) != 1 {
		t.Fatalf("expected exactly one API key, got %d", len(list.APIKeys))
	}

	raw := list.APIKeys[0]

	if _, exists := raw["key"]; exists {
		t.Fatal("SECURITY FAILURE: plaintext API key exposed by list endpoint")
	}

	if _, exists := raw["key_hash"]; exists {
		t.Fatal("SECURITY FAILURE: key_hash exposed by list endpoint")
	}

	if got, _ := raw["key_prefix"].(string); got == "" {
		t.Fatal("expected key_prefix in list response")
	}
}

func TestAPIKeySecurity_ScopeEnforcement(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	tenantID := "tenant-apikey-scope"
	email := "apikey-scope-owner@example.test"
	password := "apikey-scope-password"

	env.bootstrapTenantOwnerPro(
		t,
		tenantID,
		"API Key Scope Tenant",
		email,
		password,
	)

	token := env.login(t, email, password)

	projectID := createProjectForAPIKeyTest(
		t,
		env,
		token,
		tenantID,
		"API Key Scope Project",
	)

	ingestKey := createAPIKeyForTest(
		t,
		env,
		token,
		tenantID,
		projectID,
		"ingest-only",
		"ingest",
	)

	readKey := createAPIKeyForTest(
		t,
		env,
		token,
		tenantID,
		projectID,
		"read-only",
		"read",
	)

	adminKey := createAPIKeyForTest(
		t,
		env,
		token,
		tenantID,
		projectID,
		"admin",
		"admin",
	)

	eventBody := map[string]any{
		"event_type": "apikey.scope.test",
		"actor_id":   "security-test",
		"payload": map[string]any{
			"scope": "test",
		},
	}

	// ingest -> POST allowed.
	var ingestOut map[string]any
	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/v1/events",
		map[string]string{
			"Authorization": "Bearer " + ingestKey.Key,
		},
		eventBody,
		&ingestOut,
	)

	if status != fiber.StatusCreated {
		t.Fatalf(
			"ingest key should POST successfully: status=%d body=%v",
			status,
			ingestOut,
		)
	}

	// ingest -> GET forbidden.
	var readOut map[string]any
	status = env.doJSON(
		t,
		fiber.MethodGet,
		"/v1/events",
		map[string]string{
			"Authorization": "Bearer " + ingestKey.Key,
		},
		nil,
		&readOut,
	)

	if status != fiber.StatusForbidden {
		t.Fatalf(
			"ingest-only key should not read events: expected=403 got=%d body=%v",
			status,
			readOut,
		)
	}

	// read -> GET allowed.
	status = env.doJSON(
		t,
		fiber.MethodGet,
		"/v1/events",
		map[string]string{
			"Authorization": "Bearer " + readKey.Key,
		},
		nil,
		&readOut,
	)

	if status != fiber.StatusOK {
		t.Fatalf(
			"read key should GET events: status=%d body=%v",
			status,
			readOut,
		)
	}

	// read -> POST forbidden.
	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/v1/events",
		map[string]string{
			"Authorization": "Bearer " + readKey.Key,
		},
		eventBody,
		&readOut,
	)

	if status != fiber.StatusForbidden {
		t.Fatalf(
			"read-only key should not ingest: expected=403 got=%d body=%v",
			status,
			readOut,
		)
	}

	// admin -> POST allowed.
	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/v1/events",
		map[string]string{
			"Authorization": "Bearer " + adminKey.Key,
		},
		map[string]any{
			"event_type": "apikey.admin.test",
			"actor_id":   "security-test",
		},
		&readOut,
	)

	if status != fiber.StatusCreated {
		t.Fatalf(
			"admin key should POST successfully: status=%d body=%v",
			status,
			readOut,
		)
	}

	// admin -> GET allowed.
	status = env.doJSON(
		t,
		fiber.MethodGet,
		"/v1/events",
		map[string]string{
			"Authorization": "Bearer " + adminKey.Key,
		},
		nil,
		&readOut,
	)

	if status != fiber.StatusOK {
		t.Fatalf(
			"admin key should GET successfully: status=%d body=%v",
			status,
			readOut,
		)
	}
}

func TestAPIKeySecurity_RevokedKeyRejected(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	tenantID := "tenant-apikey-revoke"
	email := "apikey-revoke-owner@example.test"
	password := "apikey-revoke-password"

	env.bootstrapTenantOwner(
		t,
		tenantID,
		"API Key Revoke Tenant",
		email,
		password,
	)

	token := env.login(t, email, password)

	projectID := createProjectForAPIKeyTest(
		t,
		env,
		token,
		tenantID,
		"API Key Revoke Project",
	)

	key := createAPIKeyForTest(
		t,
		env,
		token,
		tenantID,
		projectID,
		"revoke-me",
		"ingest",
	)

	var out map[string]any

	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/"+tenantID+"/projects/"+projectID+"/keys/"+key.APIKey.ID+"/revoke",
		bearer(token),
		nil,
		&out,
	)

	if status != fiber.StatusOK {
		t.Fatalf(
			"revoke failed: status=%d body=%v",
			status,
			out,
		)
	}

	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/v1/events",
		map[string]string{
			"Authorization": "Bearer " + key.Key,
		},
		map[string]any{
			"event_type": "apikey.revoked.test",
			"actor_id":   "security-test",
		},
		&out,
	)

	if status != fiber.StatusUnauthorized {
		t.Fatalf(
			"SECURITY FAILURE: revoked API key accepted: expected=401 got=%d body=%v",
			status,
			out,
		)
	}
}

func TestAPIKeySecurity_SameTenantCrossProjectRevokeDenied(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	tenantID := "tenant-apikey-cross-project"
	email := "apikey-cross-project-owner@example.test"
	password := "apikey-cross-project-password"

	env.bootstrapTenantOwnerPro(
		t,
		tenantID,
		"API Key Cross Project Tenant",
		email,
		password,
	)

	token := env.login(t, email, password)

	projectA := createProjectForAPIKeyTest(
		t,
		env,
		token,
		tenantID,
		"Project A",
	)

	projectB := createProjectForAPIKeyTest(
		t,
		env,
		token,
		tenantID,
		"Project B",
	)

	keyB := createAPIKeyForTest(
		t,
		env,
		token,
		tenantID,
		projectB,
		"project-b-key",
		"ingest",
	)

	var out map[string]any

	// Deliberately put Project B's key_id into Project A's revoke route.
	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/api/tenants/"+tenantID+"/projects/"+projectA+"/keys/"+keyB.APIKey.ID+"/revoke",
		bearer(token),
		nil,
		&out,
	)

	if status != fiber.StatusNotFound {
		t.Fatalf(
			"SECURITY FAILURE: Project A route revoked Project B key: expected=404 got=%d body=%v",
			status,
			out,
		)
	}

	// The Project B key must still work.
	status = env.doJSON(
		t,
		fiber.MethodPost,
		"/v1/events",
		map[string]string{
			"Authorization": "Bearer " + keyB.Key,
		},
		map[string]any{
			"event_type": "apikey.cross-project.revoke.test",
			"actor_id":   "security-test",
		},
		&out,
	)

	if status != fiber.StatusCreated {
		t.Fatalf(
			"Project B key was unexpectedly revoked: status=%d body=%v",
			status,
			out,
		)
	}
}

func TestAPIKeySecurity_CrossTenantKeyCannotBeUsedAsOtherTenant(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	tenantA := "tenant-apikey-a"
	tenantB := "tenant-apikey-b"

	emailA := "apikey-a@example.test"
	emailB := "apikey-b@example.test"

	passwordA := "apikey-a-password"
	passwordB := "apikey-b-password"

	env.bootstrapTenantOwnerPro(
		t,
		tenantA,
		"API Key Tenant A",
		emailA,
		passwordA,
	)

	env.bootstrapTenantOwnerPro(
		t,
		tenantB,
		"API Key Tenant B",
		emailB,
		passwordB,
	)

	tokenA := env.login(t, emailA, passwordA)
	tokenB := env.login(t, emailB, passwordB)

	projectA := createProjectForAPIKeyTest(
		t,
		env,
		tokenA,
		tenantA,
		"Tenant A Project",
	)

	projectB := createProjectForAPIKeyTest(
		t,
		env,
		tokenB,
		tenantB,
		"Tenant B Project",
	)

	keyA := createAPIKeyForTest(
		t,
		env,
		tokenA,
		tenantA,
		projectA,
		"tenant-a-key",
		"admin",
	)

	var out map[string]any

	// There is deliberately no project_id in /v1/events.
	// The API key itself MUST be the authorization source.
	status := env.doJSON(
		t,
		fiber.MethodPost,
		"/v1/events",
		map[string]string{
			"Authorization": "Bearer " + keyA.Key,
		},
		map[string]any{
			"event_type": "apikey.tenant-a.test",
			"actor_id":   "tenant-a",
		},
		&out,
	)

	if status != fiber.StatusCreated {
		t.Fatalf(
			"Tenant A key should ingest its own project: status=%d body=%v",
			status,
			out,
		)
	}

	// Try to manipulate a dashboard route using Tenant A's JWT against
	// Tenant B. This confirms the tenant boundary around key management.
	status = env.doJSON(
		t,
		fiber.MethodGet,
		"/api/tenants/"+tenantB+"/projects/"+projectB+"/keys",
		bearer(tokenA),
		nil,
		&out,
	)

	if status != fiber.StatusForbidden {
		t.Fatalf(
			"SECURITY FAILURE: Tenant A accessed Tenant B API keys: expected=403 got=%d body=%v",
			status,
			out,
		)
	}
}
