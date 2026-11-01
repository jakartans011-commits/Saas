package main

import (
	"errors"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"

	"trailseal/audit"
	"trailseal/auth"
)

// getFrontendBaseURL dipanggil per request, bukan disimpan di var package-level,
// supaya env yang di-set belakangan (termasuk oleh test) ikut terbaca.
// Trailing slash dibuang supaya FRONTEND_BASE_URL="https://app.trailseal.io/"
// tidak menghasilkan "//dashboard".
// Fallback hanya untuk dev: di production validateProductionEnv() mewajibkan
// FRONTEND_BASE_URL, jadi baris terakhir tidak pernah tercapai di sana.
func getFrontendBaseURL() string {
	v := os.Getenv("FRONTEND_BASE_URL")
	for len(v) > 0 && v[len(v)-1] == '/' {
		v = v[:len(v)-1]
	}
	if v != "" {
		return v
	}
	return "http://localhost:3000"
}

func oauthRedirectURI(c *fiber.Ctx, provider string) string {
	return c.BaseURL() + "/api/auth/oauth/" + provider + "/callback"
}

// GET /api/auth/oauth/:provider/start — TIDAK BERUBAH
func OAuthStartHandler(providers map[string]auth.OAuthProvider, states *auth.OAuthStateStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		providerName := c.Params("provider")
		provider, ok := providers[providerName]
		if !ok {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "unknown or unconfigured oauth provider"})
		}
		state, err := states.Generate(providerName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to generate oauth state"})
		}
		redirectURI := oauthRedirectURI(c, providerName)
		return c.Redirect(provider.AuthURL(state, redirectURI), fiber.StatusFound)
	}
}

// GET /api/auth/oauth/:provider/callback — 2b+2d+2e ditambahkan.
// Access token & refresh token dikirim lewat cookie HttpOnly (bukan query
// param) supaya browser bisa langsung akses /overview tanpa JS handoff.
func OAuthCallbackHandler(
	providers map[string]auth.OAuthProvider,
	states *auth.OAuthStateStore,
	lookup auth.UserEmailLookupStore,
	userStore auth.UserPutStore,
	refreshStore auth.RefreshTokenStore,
	signingKey []byte,
	newTenant auth.NewTenantFunc,
	auditStore audit.Store,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		providerName := c.Params("provider")
		provider, ok := providers[providerName]
		if !ok {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "unknown or unconfigured oauth provider"})
		}
		if errParam := c.Query("error"); errParam != "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "oauth_provider_error", "detail": errParam})
		}
		state := c.Query("state")
		code := c.Query("code")
		if state == "" || code == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing state or code"})
		}
		if !states.Consume(providerName, state) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid or expired state"})
		}

		redirectURI := oauthRedirectURI(c, providerName)
		accessToken, err := provider.Exchange(code, redirectURI)
		if err != nil {
			if saveErr := audit.RecordEvent(auditStore, "unauthenticated", "unauthenticated", "oauth_login", providerName, audit.OutcomeFailure, map[string]string{"reason": "token_exchange_failed"}); saveErr != nil {
				log.Printf("WARN: audit logging failed for oauth callback (%s): %v", providerName, saveErr)
			}
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "token exchange failed"})
		}

		email, err := provider.FetchEmail(accessToken) // sudah verified-only, per audit sebelumnya
		if err != nil {
			if saveErr := audit.RecordEvent(auditStore, "unauthenticated", "unauthenticated", "oauth_login", providerName, audit.OutcomeFailure, map[string]string{"reason": "fetch_email_failed"}); saveErr != nil {
				log.Printf("WARN: audit logging failed for oauth callback (%s): %v", providerName, saveErr)
			}
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "failed to fetch email from provider"})
		}

		user, err := auth.ResolveOAuthUser(lookup, userStore, newTenant, email)
		if err != nil {
			if errors.Is(err, auth.ErrUserSuspended) {
				if saveErr := audit.RecordEvent(auditStore, "unauthenticated", "unauthenticated", "oauth_login", providerName, audit.OutcomeDenied, map[string]string{"reason": "user_suspended"}); saveErr != nil {
					log.Printf("WARN: audit logging failed for oauth callback (%s): %v", providerName, saveErr)
				}
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "user_suspended"})
			}
			if saveErr := audit.RecordEvent(auditStore, "unauthenticated", "unauthenticated", "oauth_login", providerName, audit.OutcomeFailure, map[string]string{"reason": "account_resolution_failed"}); saveErr != nil {
				log.Printf("WARN: audit logging failed for oauth callback (%s): %v", providerName, saveErr)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "oauth_account_resolution_failed"})
		}

		accessTok, err := auth.GenerateAccessToken(user.ID, user.TenantID, user.RoleID, signingKey)
		if err != nil {
			if saveErr := audit.RecordEvent(auditStore, user.TenantID, user.ID, "oauth_login", providerName, audit.OutcomeFailure, map[string]string{"reason": "token_issuance_failed"}); saveErr != nil {
				log.Printf("WARN: audit logging failed for tenant %s: %v", user.TenantID, saveErr)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "token_issuance_failed"})
		}
		refreshTok, err := auth.GenerateRefreshToken(user.ID, refreshStore)
		if err != nil {
			if saveErr := audit.RecordEvent(auditStore, user.TenantID, user.ID, "oauth_login", providerName, audit.OutcomeFailure, map[string]string{"reason": "token_issuance_failed"}); saveErr != nil {
				log.Printf("WARN: audit logging failed for tenant %s: %v", user.TenantID, saveErr)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "token_issuance_failed"})
		}

		if saveErr := audit.RecordEvent(auditStore, user.TenantID, user.ID, "oauth_login", providerName, audit.OutcomeSuccess, nil); saveErr != nil {
			log.Printf("WARN: audit logging failed for tenant %s: %v", user.TenantID, saveErr)
		}

		// Cookie auth disamakan lewat helper di auth_cookies.go supaya Path
		// konsisten dengan endpoint refresh (dulu /api/auth → cookie ganda).
		clearLegacyRefreshCookie(c)
		setRefreshCookie(c, refreshTok, auth.RefreshTokenTTL)
		setAccessCookie(c, accessTok, auth.AccessTokenTTL)

		return c.Redirect(getFrontendBaseURL(), fiber.StatusFound)
	}
}

func OAuthRateLimit(limiter *auth.CredentialStuffingLimiter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ip := c.IP()
		if !limiter.Allow(ip) {
			c.Set("Retry-After", "60")
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "too_many_requests"})
		}
		limiter.RecordAttempt(ip)
		return c.Next()
	}
}
