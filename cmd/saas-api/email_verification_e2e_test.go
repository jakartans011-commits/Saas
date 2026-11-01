// cmd/saas-api/email_verification_e2e_test.go
package main

import (
	"bytes"
	"log"
	"os"
	"regexp"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestE2E_Signup_VerifyEmail_Login menutup checklist 2A.2:
// signup -> terima email -> verify -> login/use protected flow.
//
// Karena tidak ada provider email sungguhan di test (LoggingMailer
// hanya log.Printf), token verifikasi ditangkap dari output log,
// bukan dari inbox — ini pengganti "menerima email" yang valid untuk
// E2E test tanpa provider eksternal.
func TestE2E_Signup_VerifyEmail_Login(t *testing.T) {
	env := newTestEnv(t)

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// 1. SIGNUP
	var registerOut map[string]any
	status := env.doJSON(t, fiber.MethodPost, "/api/auth/register", nil,
		map[string]string{
			"company_name": "Acme Verify Test",
			"email":        "verify-e2e@example.test",
			"password":     "correct-horse-battery-staple",
			"plan":         "free",
		}, &registerOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("register failed: status %d body %v", status, registerOut)
	}

	// 2. "TERIMA EMAIL" — ekstrak token dari log LoggingMailer.
	// Kalau ini gagal menemukan token, artinya signup sukses TIDAK
	// mengirim email verifikasi sama sekali (lihat catatan bug di
	// app.go: blok kirim-email sebelumnya ada di jalur ERROR, bukan
	// jalur SUKSES — harus dipindah lebih dulu sebelum test ini pass).
	re := regexp.MustCompile(`token=([A-Za-z0-9_\-]+)`)
	match := re.FindStringSubmatch(logBuf.String())
	if match == nil {
		t.Fatalf(
			"no verification token found in mailer log after successful signup — "+
				"verification email was not sent. Log output:\n%s",
			logBuf.String(),
		)
	}
	token := match[1]

	// 3. VERIFY
	var verifyOut map[string]any
	status = env.doJSON(t, fiber.MethodGet, "/api/auth/verify-email?token="+token, nil, nil, &verifyOut)
	if status != fiber.StatusOK {
		t.Fatalf("verify-email failed: status %d body %v", status, verifyOut)
	}
	if verifyOut["status"] != "verified" {
		t.Fatalf("expected status=verified, got: %v", verifyOut)
	}

	// 4. LOGIN — pastikan akun tetap bisa dipakai normal setelah verifikasi.
	accessToken := env.login(t, "verify-e2e@example.test", "correct-horse-battery-staple")
	if accessToken == "" {
		t.Fatal("login after verification returned empty access_token")
	}

	// 5. Pastikan EmailVerifiedAt benar-benar tersimpan di store (bukan
	// cuma respons HTTP yang kelihatan sukses).
	user, err := env.userStore.FindByEmail("verify-e2e@example.test")
	if err != nil {
		t.Fatalf("failed to fetch user after verification: %v", err)
	}
	if user.EmailVerifiedAt == nil {
		t.Fatal("CORRECTNESS FAILURE: user.EmailVerifiedAt is nil after successful verify-email call")
	}
}

// TestE2E_VerifyEmail_InvalidToken_Rejected — jalur negatif: token
// asal-asalan tidak boleh memverifikasi akun manapun.
func TestE2E_VerifyEmail_InvalidToken_Rejected(t *testing.T) {
	env := newTestEnv(t)

	var out map[string]any
	status := env.doJSON(t, fiber.MethodGet, "/api/auth/verify-email?token=not-a-real-token", nil, nil, &out)
	if status == fiber.StatusOK {
		t.Fatalf("expected non-200 for invalid verification token, got %d", status)
	}
}
