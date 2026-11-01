package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// mcpRPCCall mengirim satu request ke /mcp/v1/rpc dengan API key.
// Isi body tidak penting: limiter berjalan sebelum handler MCP.
func mcpRPCCall(t *testing.T, env *testEnv, apiKey string) (int, string, http.Header) {
	t.Helper()

	req := httptest.NewRequest(
		fiber.MethodPost,
		"/mcp/v1/rpc",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := env.app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

// isRateLimited hanya true untuk 429 dari rate limiter, bukan 429 lain
// (mis. kuota usage) yang bisa datang dari middleware sesudahnya.
func isRateLimited(status int, body string) bool {
	return status == fiber.StatusTooManyRequests &&
		strings.Contains(body, "rate_limit_exceeded")
}

func TestMCPRPCAPIKey_RateLimitFollowsPlan(t *testing.T) {
	env := newTestEnv(t)
	requirePostgres(t, env)

	tenantID := fmt.Sprintf("tenant-mcp-rl-%d", time.Now().UnixNano())
	email := tenantID + "@example.test"
	password := "mcp-rate-limit-password"

	env.bootstrapTenantOwner(t, tenantID, "MCP Rate Limit Tenant", email, password) // plan default = Free
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = env.pgPool.Exec(ctx, `DELETE FROM api_keys WHERE tenant_id = $1`, tenantID)
		_, _ = env.pgPool.Exec(ctx, `DELETE FROM projects WHERE tenant_id = $1`, tenantID)
	})

	token := env.login(t, email, password)
	projectID := createProjectForAPIKeyTest(t, env, token, tenantID, "MCP Rate Limit Project")
	key := createAPIKeyForTest(t, env, token, tenantID, projectID, "mcp-rl", "read")

	// Request pertama: limiter harus memakai limit plan Free (60) lewat jalur API key.
	status, body, hdr := mcpRPCCall(t, env, key.Key)
	if isRateLimited(status, body) {
		t.Fatalf("request pertama tidak boleh kena rate limit: %d %s", status, body)
	}
	if got := hdr.Get("X-RateLimit-Limit"); got != "60" {
		t.Fatalf("X-RateLimit-Limit Free = %q, want 60 (status=%d body=%s)", got, status, body)
	}
	sent := 1

	// Hindari pergantian window di tengah test: bila window hampir habis,
	// tunggu window baru lalu hitung ulang dari nol.
	reset, err := strconv.ParseInt(hdr.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		t.Fatalf("X-RateLimit-Reset tidak valid: %q", hdr.Get("X-RateLimit-Reset"))
	}
	if until := reset - time.Now().Unix(); until < 10 {
		time.Sleep(time.Duration(until+1) * time.Second)
		sent = 0
	}

	for ; sent < 60; sent++ {
		status, body, _ = mcpRPCCall(t, env, key.Key)
		if isRateLimited(status, body) {
			t.Fatalf("request ke-%d (batas Free 60) tidak boleh ditolak: %s", sent+1, body)
		}
	}

	// Request ke-61 harus ditolak limiter, lengkap dengan Retry-After.
	status, body, hdr = mcpRPCCall(t, env, key.Key)
	if !isRateLimited(status, body) {
		t.Fatalf("request ke-61 Free harus 429 rate_limit_exceeded, got %d %s", status, body)
	}
	if ra, err := strconv.Atoi(hdr.Get("Retry-After")); err != nil || ra < 1 {
		t.Fatalf("Retry-After tidak valid: %q", hdr.Get("Retry-After"))
	}

	// Upgrade ke Pro: berlaku seketika, memakai hitungan yang sama (bukan jatah baru).
	tn, err := env.tenantStore.Get(tenantID)
	if err != nil {
		t.Fatalf("get tenant %s: %v", tenantID, err)
	}
	tn.PlanTier = "pro"
	if err := env.tenantStore.Put(tn); err != nil {
		t.Fatalf("set plan pro: %v", err)
	}

	status, body, hdr = mcpRPCCall(t, env, key.Key)
	if isRateLimited(status, body) {
		t.Fatalf("setelah upgrade Pro request harus lolos: %d %s", status, body)
	}
	if got := hdr.Get("X-RateLimit-Limit"); got != "300" {
		t.Fatalf("X-RateLimit-Limit Pro = %q, want 300", got)
	}
}
