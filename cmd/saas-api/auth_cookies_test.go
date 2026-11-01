package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// captureSetCookie memulangkan header Set-Cookie dalam huruf kecil.
//
// fiber menulis atribut sebagai "path=/" dan "secure", bukan "Path=/" dan
// "Secure". Perbandingan yang peka huruf besar-kecil membuat pemeriksaan
// "Secure tidak boleh ada" selalu lolos — test yang tidak pernah bisa gagal.
func captureSetCookie(t *testing.T, write func(*fiber.Ctx)) string {
	t.Helper()
	app := fiber.New()
	app.Get("/probe", func(c *fiber.Ctx) error {
		write(c)
		return c.SendStatus(fiber.StatusOK)
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return strings.ToLower(resp.Header.Get("Set-Cookie"))
}

func assertCookieHas(t *testing.T, header string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(header, want) {
			t.Errorf("Set-Cookie %q tidak memuat %q", header, want)
		}
	}
}

func TestResolveCookieSameSite(t *testing.T) {
	for in, want := range map[string]string{
		"": "Lax", "lax": "Lax", "Lax": "Lax",
		"none": "None", "NONE": "None",
		"strict": "Strict",
		"typo":   "Lax",
	} {
		t.Setenv("COOKIE_SAMESITE", in)
		if got := resolveCookieSameSite(); got != want {
			t.Errorf("COOKIE_SAMESITE=%q -> %q, want %q", in, got, want)
		}
	}
}

// Konfigurasi lintas domain: cookie harus membawa secure dan SameSite=None,
// kalau tidak browser tidak pernah mengirimnya ke API.
func TestSetAccessCookie_CrossSiteConfiguration(t *testing.T) {
	origSecure, origSameSite := cookieSecure, cookieSameSite
	t.Cleanup(func() { cookieSecure, cookieSameSite = origSecure, origSameSite })
	cookieSecure, cookieSameSite = true, "None"

	header := captureSetCookie(t, func(c *fiber.Ctx) {
		setAccessCookie(c, "tok-123", time.Minute)
	})

	assertCookieHas(t, header,
		"access_token=tok-123", "path=/", "httponly", "secure", "samesite=none")
}

// Dev di http://localhost: secure tidak boleh disetel, kalau tidak browser
// membuang cookie dan login lokal tidak pernah berhasil.
func TestSetRefreshCookie_DevConfiguration(t *testing.T) {
	origSecure, origSameSite := cookieSecure, cookieSameSite
	t.Cleanup(func() { cookieSecure, cookieSameSite = origSecure, origSameSite })
	cookieSecure, cookieSameSite = false, "Lax"

	header := captureSetCookie(t, func(c *fiber.Ctx) {
		setRefreshCookie(c, "rt-123", time.Hour)
	})

	assertCookieHas(t, header,
		"refresh_token=rt-123", "path=/api/auth/refresh", "httponly", "samesite=lax")

	if strings.Contains(header, "secure") {
		t.Errorf("Set-Cookie %q memuat secure padahal COOKIE_SECURE tidak aktif", header)
	}
}

// Cookie penghapus harus membawa atribut keamanan yang sama dengan cookie
// yang disetel, supaya browser benar-benar menimpa entri yang ada.
func TestClearCookies_MatchAttributes(t *testing.T) {
	origSecure, origSameSite := cookieSecure, cookieSameSite
	t.Cleanup(func() { cookieSecure, cookieSameSite = origSecure, origSameSite })
	cookieSecure, cookieSameSite = true, "None"

	assertCookieHas(t,
		captureSetCookie(t, clearAccessCookie),
		"access_token=", "path=/", "httponly", "secure", "samesite=none")

	assertCookieHas(t,
		captureSetCookie(t, clearRefreshCookie),
		"refresh_token=", "path=/api/auth/refresh", "httponly", "secure", "samesite=none")
}
