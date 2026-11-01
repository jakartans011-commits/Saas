package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
)

// validateProductionEnv aktif HANYA bila APP_ENV=production, jadi test dan
// dev tidak terpengaruh. Hanya nama env yang dilaporkan, tidak pernah nilainya.
func validateProductionEnv() error {
	if os.Getenv("APP_ENV") != "production" {
		return nil
	}
	var problems []string
	// FRONTEND_BASE_URL wajib: bila kosong, /dashboard dan redirect OAuth
	// jatuh ke fallback dev dan setiap login pelanggan patah tanpa error log.
	for _, k := range []string{
		"CORS_ALLOWED_ORIGINS", "STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET",
		"APP_BASE_URL", "FRONTEND_BASE_URL", "RESEND_API_KEY", "NOTIFY_FROM_EMAIL",
		"ALERT_WEBHOOK_URL", "BILLING_SUCCESS_URL", "BILLING_CANCEL_URL",
	} {
		if os.Getenv(k) == "" {
			problems = append(problems, k+" is empty")
		}
	}
	for _, k := range []string{"APP_BASE_URL", "FRONTEND_BASE_URL", "BILLING_SUCCESS_URL", "BILLING_CANCEL_URL"} {
		if v := os.Getenv(k); v != "" && !strings.HasPrefix(v, "https://") {
			problems = append(problems, k+" must start with https://")
		}
	}
	for _, o := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" && !strings.HasPrefix(o, "https://") {
			problems = append(problems, "CORS_ALLOWED_ORIGINS contains a non-https origin")
			break
		}
	}
	if os.Getenv("COOKIE_SECURE") != "true" {
		problems = append(problems, "COOKIE_SECURE must be \"true\"")
	}

	// Nilai COOKIE_SAMESITE yang salah ketik diam-diam jatuh ke "Lax" dan
	// bisa mematikan seluruh API di production. Tolak nilai yang tidak dikenal.
	switch sameSite := strings.ToLower(strings.TrimSpace(os.Getenv("COOKIE_SAMESITE"))); sameSite {
	case "", "lax", "strict":
		// ok
	case "none":
		// Browser menolak SameSite=None tanpa Secure, sehingga cookie tidak
		// pernah tersimpan dan login gagal total tanpa pesan yang jelas.
		if os.Getenv("COOKIE_SECURE") != "true" {
			problems = append(problems, "COOKIE_SAMESITE=None requires COOKIE_SECURE=true")
		}
	default:
		problems = append(problems, "COOKIE_SAMESITE must be Lax, None, or Strict")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid production config: %s", strings.Join(problems, "; "))
	}

	warnIfCookiesWillNotReachFrontend()
	return nil
}

// warnIfCookiesWillNotReachFrontend memperingatkan bila frontend tampaknya
// berada di domain lain sementara cookie masih SameSite=Lax — kombinasi yang
// membuat setiap panggilan API dari frontend tidak terautentikasi.
//
// Sengaja peringatan, bukan error: perbandingan dua label terakhir salah
// untuk domain seperti example.co.uk, dan memblokir deploy yang sebenarnya
// sah lebih merugikan daripada satu baris log yang terlewat.
func warnIfCookiesWillNotReachFrontend() {
	sameSite := strings.ToLower(strings.TrimSpace(os.Getenv("COOKIE_SAMESITE")))
	if sameSite == "none" {
		return
	}
	appHost := hostOf(os.Getenv("APP_BASE_URL"))
	frontHost := hostOf(os.Getenv("FRONTEND_BASE_URL"))
	if appHost == "" || frontHost == "" {
		return
	}
	if registrableSuffix(appHost) == registrableSuffix(frontHost) {
		return
	}
	log.Printf(
		"WARNING: APP_BASE_URL dan FRONTEND_BASE_URL tampak berbeda domain "+
			"(%s vs %s) sementara COOKIE_SAMESITE masih Lax. Browser tidak akan "+
			"mengirim cookie sesi pada panggilan API dari frontend. Setel "+
			"COOKIE_SAMESITE=None bila keduanya memang beda domain.",
		appHost, frontHost,
	)
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func registrableSuffix(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}
