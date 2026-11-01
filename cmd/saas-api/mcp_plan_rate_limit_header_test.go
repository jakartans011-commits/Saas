package main

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestMCPRPCAPIKey_RateLimitHeaderMatchesPlan membuktikan limiter MCP membaca
// batas dari plan tenant untuk KETIGA tier di tabel pricing.
//
// Test yang sudah ada hanya menembus batas Free (60 request). Menghabiskan
// 300 dan 1.000 request demi Pro dan Business akan menambah puluhan detik
// tanpa menambah keyakinan: jalur kodenya sama, hanya angkanya berbeda.
// Satu request per tier sudah cukup — header X-RateLimit-Limit memperlihatkan
// angka yang dipakai limiter sebelum batas itu tersentuh.
func TestMCPRPCAPIKey_RateLimitHeaderMatchesPlan(t *testing.T) {
	cases := []struct {
		tier string
		want string
	}{
		{"free", "60"},
		{"pro", "300"},
		{"business", "1000"},
	}

	for _, tc := range cases {
		t.Run(tc.tier, func(t *testing.T) {
			env := newTestEnv(t)
			requirePostgres(t, env)

			tenantID := fmt.Sprintf("tenant-mcp-hdr-%s-%d", tc.tier, time.Now().UnixNano())
			email := tenantID + "@example.test"
			const password = "mcp-header-password"

			env.bootstrapTenantOwner(t, tenantID, "MCP Header Tenant", email, password)
			env.setTenantPlan(t, tenantID, tc.tier)

			t.Cleanup(func() {
				ctx := context.Background()
				_, _ = env.pgPool.Exec(ctx, `DELETE FROM api_keys WHERE tenant_id = $1`, tenantID)
				_, _ = env.pgPool.Exec(ctx, `DELETE FROM projects WHERE tenant_id = $1`, tenantID)
			})

			token := env.login(t, email, password)
			projectID := createProjectForAPIKeyTest(t, env, token, tenantID, "MCP Header Project")
			key := createAPIKeyForTest(t, env, token, tenantID, projectID, "mcp-hdr", "read")

			status, body, hdr := mcpRPCCall(t, env, key.Key)
			if isRateLimited(status, body) {
				t.Fatalf("request pertama tidak boleh kena rate limit: %d %s", status, body)
			}
			if got := hdr.Get("X-RateLimit-Limit"); got != tc.want {
				t.Fatalf("X-RateLimit-Limit untuk plan %s = %q, want %q (status=%d body=%s)",
					tc.tier, got, tc.want, status, body)
			}
		})
	}
}
