package main

import (
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	refreshCookieName = "refresh_token"
	// Cookie hanya dikirim browser ke endpoint refresh, bukan ke semua request.
	refreshCookiePath = "/api/auth/refresh"
	// Path warisan dari oauth_handlers.go sebelum unifikasi cookie (2026-09).
	// Browser menyimpan (name, domain, path) sebagai entri terpisah, jadi
	// cookie lama ini tidak ikut tertimpa dan harus dihapus eksplisit.
	legacyRefreshCookiePath = "/api/auth"
)

// COOKIE_SECURE=true wajib di production (HTTPS). Dev http://localhost: biarkan kosong.
var cookieSecure = os.Getenv("COOKIE_SECURE") == "true"

// cookieSameSite menentukan apakah browser mengirim cookie sesi pada
// request yang berasal dari situs lain.
//
// "Lax" (bawaan) benar bila frontend dan API berbagi registrable domain
// yang sama, misalnya app.trailseal.io memanggil api.trailseal.io, dan
// juga di dev (localhost ke localhost).
//
// "None" WAJIB bila frontend berada di domain yang berbeda, misalnya
// frontend di trailseal.vercel.app memanggil api.trailseal.io. Dengan
// "Lax", browser tidak pernah mengirim cookie pada fetch lintas situs:
// login tampak berhasil lalu setiap panggilan API balas 401. Kegagalan
// ini tidak muncul sama sekali di dev, hanya setelah deploy.
//
// "None" mengharuskan Secure, dan melonggarkan proteksi CSRF bawaan
// SameSite — jangan dipakai kalau "Lax" sudah cukup.
var cookieSameSite = resolveCookieSameSite()

func resolveCookieSameSite() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("COOKIE_SAMESITE"))) {
	case "none":
		return "None"
	case "strict":
		return "Strict"
	default:
		return "Lax"
	}
}

func setAccessCookie(c *fiber.Ctx, token string, ttl time.Duration) {
	c.Cookie(&fiber.Cookie{
		Name: "access_token", Value: token, Path: "/",
		MaxAge: int(ttl.Seconds()), HTTPOnly: true, SameSite: cookieSameSite,
		Secure: cookieSecure,
	})
}

// clearAccessCookie menghapus cookie access_token. Atributnya harus sama
// dengan setAccessCookie supaya browser benar-benar menimpa entri yang ada.
func clearAccessCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name: "access_token", Value: "", Path: "/",
		MaxAge: -1, Expires: time.Now().Add(-time.Hour), HTTPOnly: true,
		SameSite: cookieSameSite, Secure: cookieSecure,
	})
}

func setRefreshCookie(c *fiber.Ctx, token string, ttl time.Duration) {
	c.Cookie(&fiber.Cookie{
		Name: refreshCookieName, Value: token, Path: refreshCookiePath,
		MaxAge: int(ttl.Seconds()), HTTPOnly: true, SameSite: cookieSameSite,
		Secure: cookieSecure,
	})
}

func clearRefreshCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name: refreshCookieName, Value: "", Path: refreshCookiePath,
		MaxAge: -1, Expires: time.Now().Add(-time.Hour), HTTPOnly: true,
		SameSite: cookieSameSite, Secure: cookieSecure,
	})
}

// clearLegacyRefreshCookie menghapus cookie refresh_token bersisa dengan
// Path=/api/auth. Tanpa ini browser mengirim DUA cookie refresh_token ke
// POST /api/auth/refresh dan server memakai yang salah.
func clearLegacyRefreshCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name: refreshCookieName, Value: "", Path: legacyRefreshCookiePath,
		MaxAge: -1, Expires: time.Now().Add(-time.Hour), HTTPOnly: true,
		SameSite: cookieSameSite, Secure: cookieSecure,
	})
}
