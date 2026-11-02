package main

import (
	"strings"
	"testing"
)

// ============================================================
// validateProductionEnv
//
// Fungsi ini adalah satu-satunya yang berdiri antara konfigurasi yang
// salah dan pelanggan yang dirugikan diam-diam: cookie yang tidak pernah
// sampai, webhook Stripe yang tidak pernah menaikkan paket, atau bbolt
// yang hidup di disk sementara sehingga setiap deploy menghapus semua
// akun.
//
// Semua kegagalan itu TIDAK memunculkan error saat server menyala. Jadi
// satu-satunya cara memastikan penjaganya masih bekerja adalah menguji
// penjaganya sendiri.
//
//	go test ./cmd/saas-api -run ValidateProductionEnv -v
// ============================================================

// validProductionEnv memasang satu set konfigurasi yang sah, lalu
// mengembalikan fungsi untuk menimpa satu kunci. t.Setenv memulihkan
// nilai aslinya secara otomatis setelah test selesai.
func validProductionEnv(t *testing.T) {
	t.Helper()

	for k, v := range map[string]string{
		"APP_ENV":               "production",
		"CORS_ALLOWED_ORIGINS":  "https://app.example.com",
		"STRIPE_SECRET_KEY":     "sk_live_dummy",
		"STRIPE_WEBHOOK_SECRET": "whsec_dummy",
		"APP_BASE_URL":          "https://api.example.com",
		"FRONTEND_BASE_URL":     "https://app.example.com",
		"RESEND_API_KEY":        "re_dummy",
		"NOTIFY_FROM_EMAIL":     "no-reply@example.com",
		"ALERT_WEBHOOK_URL":     "https://hooks.example.com/dummy",
		"BILLING_SUCCESS_URL":   "https://app.example.com/billing/ok",
		"BILLING_CANCEL_URL":    "https://app.example.com/billing/cancel",
		"STRIPE_PRICE_PRO":      "price_pro_dummy",
		"STRIPE_PRICE_BUSINESS": "price_business_dummy",
		"COOKIE_SECURE":         "true",
		"COOKIE_SAMESITE":       "Lax",
		"TRAILSEAL_DATA_DIR":    "/var/data",

		// Harus kosong di production. Dikosongkan eksplisit karena shell
		// pengembang sering memasangnya untuk mempercepat test.
		"SHADOWHARVEST_TEST_BCRYPT_COST": "",
	} {
		t.Setenv(k, v)
	}
}

func TestValidateProductionEnv_AcceptsCompleteConfig(t *testing.T) {
	validProductionEnv(t)

	if err := validateProductionEnv(); err != nil {
		t.Fatalf("konfigurasi yang sah ditolak: %v", err)
	}
}

func TestValidateProductionEnv_SkippedOutsideProduction(t *testing.T) {
	validProductionEnv(t)

	// Semua dikosongkan, tapi APP_ENV bukan production.
	for _, k := range []string{
		"CORS_ALLOWED_ORIGINS", "STRIPE_SECRET_KEY", "APP_BASE_URL",
		"COOKIE_SECURE", "TRAILSEAL_DATA_DIR",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("APP_ENV", "development")

	if err := validateProductionEnv(); err != nil {
		t.Fatalf("validasi tidak boleh aktif di luar production: %v", err)
	}
}

func TestValidateProductionEnv_RejectsEmptyRequiredVars(t *testing.T) {
	required := []string{
		"CORS_ALLOWED_ORIGINS", "STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET",
		"APP_BASE_URL", "FRONTEND_BASE_URL", "RESEND_API_KEY", "NOTIFY_FROM_EMAIL",
		"ALERT_WEBHOOK_URL", "BILLING_SUCCESS_URL", "BILLING_CANCEL_URL",
		"STRIPE_PRICE_PRO", "STRIPE_PRICE_BUSINESS",
	}

	for _, key := range required {
		t.Run(key, func(t *testing.T) {
			validProductionEnv(t)
			t.Setenv(key, "")

			err := validateProductionEnv()
			if err == nil {
				t.Fatalf("%s kosong tapi konfigurasi diterima", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("pesan error tidak menyebut %s, jadi operator tidak tahu "+
					"apa yang harus diperbaiki: %v", key, err)
			}
		})
	}
}

func TestValidateProductionEnv_RequiresHTTPS(t *testing.T) {
	for _, key := range []string{
		"APP_BASE_URL", "FRONTEND_BASE_URL", "BILLING_SUCCESS_URL", "BILLING_CANCEL_URL",
	} {
		t.Run(key, func(t *testing.T) {
			validProductionEnv(t)
			t.Setenv(key, "http://insecure.example.com")

			err := validateProductionEnv()
			if err == nil {
				t.Fatalf("%s memakai http:// tapi diterima", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("pesan error tidak menyebut %s: %v", key, err)
			}
		})
	}

	t.Run("CORS_ALLOWED_ORIGINS", func(t *testing.T) {
		validProductionEnv(t)
		t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com,http://evil.example.com")

		if err := validateProductionEnv(); err == nil {
			t.Fatal("origin http:// di tengah daftar lolos")
		}
	})
}

func TestValidateProductionEnv_CookieRules(t *testing.T) {
	t.Run("COOKIE_SECURE harus true", func(t *testing.T) {
		validProductionEnv(t)
		t.Setenv("COOKIE_SECURE", "false")

		if err := validateProductionEnv(); err == nil {
			t.Fatal("COOKIE_SECURE=false diterima di production")
		}
	})

	t.Run("SameSite None menuntut Secure", func(t *testing.T) {
		validProductionEnv(t)
		t.Setenv("COOKIE_SAMESITE", "None")
		t.Setenv("COOKIE_SECURE", "false")

		if err := validateProductionEnv(); err == nil {
			t.Fatal("SameSite=None tanpa Secure diterima; browser akan menolak " +
				"cookie itu dan setiap login gagal tanpa pesan yang jelas")
		}
	})

	t.Run("SameSite None dengan Secure diterima", func(t *testing.T) {
		validProductionEnv(t)
		t.Setenv("COOKIE_SAMESITE", "None")

		// Kombinasi yang dibutuhkan saat API dan frontend berada di domain
		// berbeda — misalnya sebelum domain kustom dipasang.
		if err := validateProductionEnv(); err != nil {
			t.Fatalf("SameSite=None dengan Secure=true seharusnya sah: %v", err)
		}
	})

	t.Run("SameSite tidak dikenal ditolak", func(t *testing.T) {
		validProductionEnv(t)
		t.Setenv("COOKIE_SAMESITE", "Laxx")

		if err := validateProductionEnv(); err == nil {
			t.Fatal("nilai SameSite yang salah ketik diterima; diam-diam jatuh " +
				"ke Lax dan bisa mematikan seluruh API")
		}
	})
}

func TestValidateProductionEnv_DataDirMustBeAbsolute(t *testing.T) {
	t.Run("kosong", func(t *testing.T) {
		validProductionEnv(t)
		t.Setenv("TRAILSEAL_DATA_DIR", "")

		err := validateProductionEnv()
		if err == nil {
			t.Fatal("TRAILSEAL_DATA_DIR kosong diterima. bbolt akan jatuh ke " +
				"penyimpanan sementara container, dan setiap deploy menghapus " +
				"seluruh tenant, user, dan refresh token tanpa satu pun error")
		}
		if !strings.Contains(err.Error(), "TRAILSEAL_DATA_DIR") {
			t.Errorf("pesan error tidak menyebut TRAILSEAL_DATA_DIR: %v", err)
		}
	})

	t.Run("relatif", func(t *testing.T) {
		validProductionEnv(t)
		t.Setenv("TRAILSEAL_DATA_DIR", "data")

		if err := validateProductionEnv(); err == nil {
			t.Fatal("path relatif diterima; hanya path absolut yang bisa " +
				"menunjuk volume yang benar-benar dipasang")
		}
	})
}

func TestValidateProductionEnv_RejectsTestBcryptCost(t *testing.T) {
	validProductionEnv(t)
	t.Setenv("SHADOWHARVEST_TEST_BCRYPT_COST", "4")

	err := validateProductionEnv()
	if err == nil {
		t.Fatal("SHADOWHARVEST_TEST_BCRYPT_COST terisi tapi diterima di production. " +
			"Biaya bcrypt turun dari 12 dan setiap password pelanggan jadi jauh " +
			"lebih murah dipecahkan, tanpa tanda apa pun di log")
	}
	if !strings.Contains(err.Error(), "SHADOWHARVEST_TEST_BCRYPT_COST") {
		t.Errorf("pesan error tidak menyebut variabelnya: %v", err)
	}
}
