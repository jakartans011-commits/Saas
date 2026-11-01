package middleware

import (
	"github.com/gofiber/fiber/v2"
)

// ============================================================
// MCP IDENTITY BRIDGE
//
// mcp.Server.ServeHTTP butuh X-Tenant-ID dan X-User-ID di header,
// dan sengaja tidak tahu cara auth apapun (lihat komentar di
// server.go). Middleware ini mengisi header itu dari APIKey yang
// sudah divalidasi RequireAPIKey — TANPA mengubah mcp/server.go.
//
// Pasang urutannya: RequireAPIKey -> RequireScope -> InjectMCPIdentity
// -> adaptor.HTTPHandler(mcpServer)
// ============================================================

// InjectMCPIdentityFromAPIKey mengambil *apikey.APIKey yang sudah
// divalidasi RequireAPIKey, lalu menuliskan X-Tenant-ID/X-User-ID
// ke request sebelum diteruskan ke mcp.Server (net/http handler).
//
// userID disintesis dari key.ID karena caller di sini adalah
// machine/API key, bukan human user — tetap perlu identitas untuk
// audit trail (jangan biarkan kosong, mcp/server.go akan menolak
// request tanpa X-User-ID).
func InjectMCPIdentityFromAPIKey() fiber.Handler {
	return func(c *fiber.Ctx) error {
		key, ok := APIKeyFromFiberCtx(c)
		if !ok {
			// Seharusnya tidak pernah sampai sini kalau RequireAPIKey
			// dipasang duluan di chain — fail closed kalau terjadi.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated_context",
			})
		}

		// key.TenantID adalah sumber kebenaran tenant — bukan dari
		// path param atau body request MCP manapun.
		c.Request().Header.Set("X-Tenant-ID", key.TenantID)
		c.Request().Header.Set("X-User-ID", "apikey:"+key.ID)

	   // FIX (privilege escalation): X-Role-ID/X-Plan-Tier TIDAK pernah
       // di-set middleware ini — tapi kalau client API key mengirim
       // header itu sendiri di request mentah, tidak ada yang
       // menghapusnya sebelum diteruskan ke mcpServer.ServeHTTP.
       // Akibatnya caller API key bisa selipkan X-Role-ID: owner +
       // X-Plan-Tier: business dan lolos gate handlePromptsCreate
       // (Owner-only + Business-only), walau API key tidak punya
       // konsep role/tier sama sekali. Del eksplisit, bukan Set(""),
       // supaya jelas: identitas ini TIDAK ADA untuk jalur API key.
       c.Request().Header.Del("X-Role-ID")
       c.Request().Header.Del("X-Plan-Tier")

		return c.Next()
	}
}
