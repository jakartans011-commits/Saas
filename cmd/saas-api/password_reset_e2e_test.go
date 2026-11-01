// cmd/saas-api/password_reset_e2e_test.go
package main

import (
	"bytes"
	"log"
	"os"
	"regexp"
	"testing"

	"github.com/gofiber/fiber/v2"

	"trailseal/auth"
)

var resetTokenRe = regexp.MustCompile(`token=([A-Za-z0-9_\-]+)`)

// TestE2E_ForgotPassword_ResetPassword_OldCredFails_NewCredWorks menutup
// checklist 2A.3: request reset -> complete -> cred lama gagal -> cred
// baru jalan.
//
// Sama seperti TestE2E_Signup_VerifyEmail_Login (2A.2), token diambil dari
// output log LoggingMailer karena tidak ada provider email sungguhan di
// test ini.
func TestE2E_ForgotPassword_ResetPassword_OldCredFails_NewCredWorks(t *testing.T) {
	env := newTestEnv(t)

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	const email = "reset-e2e@example.test"
	const oldPassword = "correct-horse-battery-staple"
	const newPassword = "new-correct-horse-staple-9"

	// 1. SIGNUP
	var registerOut map[string]any
	status := env.doJSON(t, fiber.MethodPost, "/api/auth/register", nil,
		map[string]string{
			"company_name": "Acme Reset Test",
			"email":        email,
			"password":     oldPassword,
			"plan":         "free",
		}, &registerOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("register failed: status %d body %v", status, registerOut)
	}

	if status != fiber.StatusOK {
		t.Fatalf("register failed: status %d body %v", status, registerOut)
	}

	// 1b. VERIFY EMAIL — wajib, karena login menolak akun yang
	// EmailVerifiedAt-nya masih nil (lihat email_verification_e2e_test.go).
	// Token diekstrak dari log SEBELUM logBuf di-reset, karena reset akan
	// membuang token verifikasi ini.
	verifyTokenRe := regexp.MustCompile(`token=([A-Za-z0-9_\-]+)`)
	verifyMatch := verifyTokenRe.FindStringSubmatch(logBuf.String())
	if verifyMatch == nil {
		t.Fatalf(
			"no verification token found in mailer log after signup — "+
				"verification email was not sent. Log output:\n%s",
			logBuf.String(),
		)
	}
	var verifyOut map[string]any
	status = env.doJSON(t, fiber.MethodGet, "/api/auth/verify-email?token="+verifyMatch[1], nil, nil, &verifyOut)
	if status != fiber.StatusOK {
		t.Fatalf("verify-email failed: status %d body %v", status, verifyOut)
	}

	// Buang log signup+verify (berisi token verifikasi email) supaya langkah
	// berikutnya hanya menangkap token reset password, bukan salah tangkap
	// token verifikasi yang muncul lebih dulu di buffer.
	logBuf.Reset()

	// 2. REQUEST RESET
	var forgotOut map[string]any
	status = env.doJSON(t, fiber.MethodPost, "/api/auth/forgot-password", nil,
		map[string]string{"email": email}, &forgotOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("forgot-password failed: status %d body %v", status, forgotOut)
	}

	// 3. "TERIMA EMAIL" — ekstrak token reset dari log LoggingMailer.
	match := resetTokenRe.FindStringSubmatch(logBuf.String())
	if match == nil {
		t.Fatalf(
			"no reset token found in mailer log after forgot-password — "+
				"reset email was not sent. Log output:\n%s",
			logBuf.String(),
		)
	}
	token := match[1]

	// 4. COMPLETE RESET
	var resetOut map[string]any
	status = env.doJSON(t, fiber.MethodPost, "/api/auth/reset-password", nil,
		map[string]string{"token": token, "new_password": newPassword}, &resetOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("reset-password failed: status %d body %v", status, resetOut)
	}
	if resetOut["status"] != "password_reset" {
		t.Fatalf("expected status=password_reset, got: %v", resetOut)
	}

	// 5. CRED LAMA GAGAL
	var oldLoginOut map[string]any
	status = env.doJSON(t, fiber.MethodPost, "/api/auth/login", nil,
		map[string]string{"email": email, "password": oldPassword}, &oldLoginOut,
	)
	if status == fiber.StatusOK {
		t.Fatalf("CORRECTNESS FAILURE: login with old password still succeeds after reset (status %d)", status)
	}

	// 6. CRED BARU JALAN
	accessToken := env.login(t, email, newPassword)
	if accessToken == "" {
		t.Fatal("login with new password after reset returned empty access_token")
	}

	// 7. Pastikan PasswordHash benar-benar berubah di store (bukan cuma
	// respons HTTP yang kelihatan sukses).
	user, err := env.userStore.FindByEmail(email)
	if err != nil {
		t.Fatalf("failed to fetch user after reset: %v", err)
	}
	if err := auth.VerifyPassword(user.PasswordHash, oldPassword); err == nil {
		t.Fatal("CORRECTNESS FAILURE: stored PasswordHash still matches the old password after reset")
	}
}

// TestE2E_ResetPassword_InvalidToken_Rejected — jalur negatif: token
// asal-asalan tidak boleh mereset password akun manapun.
func TestE2E_ResetPassword_InvalidToken_Rejected(t *testing.T) {
	env := newTestEnv(t)

	var out map[string]any
	status := env.doJSON(t, fiber.MethodPost, "/api/auth/reset-password", nil,
		map[string]string{"token": "not-a-real-token", "new_password": "irrelevant-but-long-enough"}, &out,
	)
	if status == fiber.StatusOK {
		t.Fatalf("expected non-200 for invalid reset token, got %d", status)
	}
}

// TestE2E_ResetPassword_TokenReuse_Rejected — token reset harus single-use;
// dipakai dua kali harus ditolak di percobaan kedua (lihat MarkUsed di
// password_reset_store_bbolt.go).
func TestE2E_ResetPassword_TokenReuse_Rejected(t *testing.T) {
	env := newTestEnv(t)

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	const email = "reset-reuse-e2e@example.test"
	const password = "correct-horse-battery-staple"

	var registerOut map[string]any
	status := env.doJSON(t, fiber.MethodPost, "/api/auth/register", nil,
		map[string]string{
			"company_name": "Acme Reset Reuse Test",
			"email":        email,
			"password":     password,
			"plan":         "free",
		}, &registerOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("register failed: status %d body %v", status, registerOut)
	}
	logBuf.Reset()

	var forgotOut map[string]any
	status = env.doJSON(t, fiber.MethodPost, "/api/auth/forgot-password", nil,
		map[string]string{"email": email}, &forgotOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("forgot-password failed: status %d body %v", status, forgotOut)
	}

	match := resetTokenRe.FindStringSubmatch(logBuf.String())
	if match == nil {
		t.Fatalf("no reset token found in mailer log. Log output:\n%s", logBuf.String())
	}
	token := match[1]

	// Pemakaian PERTAMA — harus sukses.
	var firstOut map[string]any
	status = env.doJSON(t, fiber.MethodPost, "/api/auth/reset-password", nil,
		map[string]string{"token": token, "new_password": "first-new-password-123"}, &firstOut,
	)
	if status != fiber.StatusOK {
		t.Fatalf("first reset-password should succeed: status %d body %v", status, firstOut)
	}

	// Pemakaian KEDUA (token yang sama) — harus ditolak.
	var secondOut map[string]any
	status = env.doJSON(t, fiber.MethodPost, "/api/auth/reset-password", nil,
		map[string]string{"token": token, "new_password": "second-new-password-456"}, &secondOut,
	)
	if status == fiber.StatusOK {
		t.Fatalf("CORRECTNESS FAILURE: reused reset token was accepted a second time (status %d)", status)
	}
}
