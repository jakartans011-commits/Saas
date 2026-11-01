package middleware

import (
	"encoding/json"
	"log"

	"github.com/gofiber/fiber/v2"

	"trailseal/usage"
)

// billableMCPMethods — daftar method JSON-RPC yang dihitung sebagai 1 event
// billable, sesuai keputusan final "TrailSeal MCP Gateway" bagian 4.
// tools/list, resources/list, prompts/list, server/discover, notifications/*
// SENGAJA TIDAK dimasukkan — itu operasi discovery, non-billable.
var billableMCPMethods = map[string]bool{
	"tools/call":     true,
	"resources/read": true,
	"prompts/get":    true,
	// TODO (roadmap, belum dibangun): aktifkan begitu Elicitation (MRTR)
	// selesai diimplementasi.
	// "elicitation/create": true,
}

// mcpJSONRPCEnvelope dipakai untuk mengintip field "method" dari request body
// dan field "error" dari response body, tanpa decode penuh ke struct
// mcp.MCPRequest — biar middleware ini tidak coupling ke tipe internal
// package mcp.
type mcpJSONRPCEnvelope struct {
	Method string          `json:"method,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

// MCPUsageMetering mencatat 1 billable event per request JSON-RPC yang
// BERHASIL dieksekusi ke endpoint MCP native (/mcp/v1/rpc).
//
// Beda dengan UsageMetering (dashboard, JWT-based):
//   - Tenant diambil dari APIKeyFromFiberCtx, BUKAN ClaimsFromFiberCtx —
//     endpoint ini diautentikasi API key, bukan JWT dashboard.
//   - Hanya method JSON-RPC yang ada di billableMCPMethods yang dihitung,
//     bukan setiap HTTP request — tools/list dkk adalah discovery, non-billable.
//   - "Berhasil" berarti tidak ada field "error" di body response JSON-RPC,
//     BUKAN cuma status HTTP < 400 — JSON-RPC error tetap bisa balik dengan
//     HTTP 200 (lihat mcp/jsonrpc.go / mcp/server.go).
//
// Pasang di chain SETELAH InjectMCPIdentityFromAPIKey() (perlu tenant dari
// situ). Urutan terhadap AuditLogger bebas — keduanya independen.
func MCPUsageMetering(eventStore usage.EventStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key, ok := APIKeyFromFiberCtx(c)
		if !ok {
			// Seharusnya tidak pernah sampai sini kalau RequireAPIKey sudah
			// dipasang duluan di chain — fail closed kalau terjadi.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		// Intip "method" dari body SEBELUM diteruskan ke handler. Fiber
		// sudah buffer c.Body(), jadi aman di-peek di sini — tidak
		// mengonsumsi body, adaptor.HTTPHandlerFunc di belakang masih baca
		// body yang sama dari awal.
		var reqEnvelope mcpJSONRPCEnvelope
		method := ""
		if err := json.Unmarshal(c.Body(), &reqEnvelope); err == nil {
			method = reqEnvelope.Method
		}
		// Kalau body tidak valid JSON di sini, biarkan saja — handler MCP
		// asli yang akan menolaknya dengan JSON-RPC parse error yang sesuai.
		// Middleware metering tidak menduplikasi validasi itu.

		err := c.Next()

		if !billableMCPMethods[method] {
			return err
		}

		// Hanya request yang BERHASIL DIEKSEKUSI yang dihitung.
		if err != nil {
			return err
		}
		if status := c.Response().StatusCode(); status >= 400 {
			return err
		}

		var respEnvelope mcpJSONRPCEnvelope
		if uErr := json.Unmarshal(c.Response().Body(), &respEnvelope); uErr != nil {
			// Response tidak bisa di-parse sebagai JSON — lebih aman TIDAK
			// mencatat event daripada salah catat event yang sebetulnya gagal.
			return err
		}
		if len(respEnvelope.Error) > 0 {
			// JSON-RPC-level error (tool gagal, timeout, validasi input
			// salah) — TIDAK mengurangi kuota, sesuai keputusan final
			// bagian 4. Rate-limit/anti-abuse untuk retry berlebihan
			// ditangani terpisah, bukan lewat quota counter ini.
			return err
		}

		metadata := map[string]string{
			"method":  method,
			"api_key": key.ID,
		}

		if saveErr := usage.RecordEvent(
			eventStore,
			key.TenantID,
			usage.EventType(method),
			metadata,
		); saveErr != nil {
			log.Printf("WARN: mcp usage metering failed for tenant %s method %s: %v", key.TenantID, method, saveErr)
		}

		return err
	}
}

// MCPUsageMeteringDashboard adalah counterpart JWT-based dari
// MCPUsageMetering, untuk endpoint dashboard (/api/tenants/:id/mcp/rpc).
//
// Logic billable-method dan JSON-RPC-error-check IDENTIK dengan
// MCPUsageMetering (lihat komentar di situ) — cuma sumber identitas
// tenant yang beda: ClaimsFromFiberCtx (JWT), bukan APIKeyFromFiberCtx.
//
// Pasang di chain SETELAH requireAuth (perlu claims sudah tervalidasi).
// JANGAN pasang di route DELETE (terminate session) — sama seperti
// jalur API key, terminate bukan pemakaian tool.
func MCPUsageMeteringDashboard(eventStore usage.EventStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := ClaimsFromFiberCtx(c)
		if !ok {
			// Seharusnya tidak pernah sampai sini kalau requireAuth sudah
			// dipasang duluan di chain — fail closed kalau terjadi.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		var reqEnvelope mcpJSONRPCEnvelope
		method := ""
		if err := json.Unmarshal(c.Body(), &reqEnvelope); err == nil {
			method = reqEnvelope.Method
		}

		err := c.Next()

		if !billableMCPMethods[method] {
			return err
		}

		if err != nil {
			return err
		}
		if status := c.Response().StatusCode(); status >= 400 {
			return err
		}

		var respEnvelope mcpJSONRPCEnvelope
		if uErr := json.Unmarshal(c.Response().Body(), &respEnvelope); uErr != nil {
			return err
		}
		if len(respEnvelope.Error) > 0 {
			return err
		}

		metadata := map[string]string{
			"method":  method,
			"user_id": claims.UserID,
		}

		if saveErr := usage.RecordEvent(
			eventStore,
			claims.TenantID,
			usage.EventType(method),
			metadata,
		); saveErr != nil {
			log.Printf("WARN: mcp usage metering (dashboard) failed for tenant %s method %s: %v", claims.TenantID, method, saveErr)
		}

		return err
	}
}
