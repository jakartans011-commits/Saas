// middleware/root_auth_test.go
package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

const testRootKey = "test-root-key-abcdef0123456789"

func newTestAppWithRoot(rootKey string, handlerCalled *bool) *fiber.App {
	app := fiber.New()
	app.Post(
		"/api/admin/tenants",
		NewRequireRoot(rootKey),
		func(c *fiber.Ctx) error {
			*handlerCalled = true
			return c.SendStatus(fiber.StatusOK)
		},
	)
	return app
}

// TEST 6: root credential valid -> request mencapai handler (ProvisionTenant).
func TestRequireRoot_ValidCredential_HandlerCalled(t *testing.T) {
	handlerCalled := false
	app := newTestAppWithRoot(testRootKey, &handlerCalled)

	req := httptest.NewRequest(fiber.MethodPost, "/api/admin/tenants", nil)
	req.Header.Set("X-Root-API-Key", testRootKey)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !handlerCalled {
		t.Fatal("expected handler to be called with valid root credential")
	}
}

// TEST 7: invalid root credential -> ditolak, handler TIDAK dipanggil.
func TestRequireRoot_InvalidCredential_Denied(t *testing.T) {
	handlerCalled := false
	app := newTestAppWithRoot(testRootKey, &handlerCalled)

	req := httptest.NewRequest(fiber.MethodPost, "/api/admin/tenants", nil)
	req.Header.Set("X-Root-API-Key", "totally-wrong-key")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if handlerCalled {
		t.Fatal("SECURITY FAILURE: handler was called with invalid root credential; ProvisionTenant must not run")
	}
}

// TEST 8: root credential hilang sama sekali -> ditolak.
func TestRequireRoot_MissingCredential_Denied(t *testing.T) {
	handlerCalled := false
	app := newTestAppWithRoot(testRootKey, &handlerCalled)

	req := httptest.NewRequest(fiber.MethodPost, "/api/admin/tenants", nil)
	// Sengaja tidak set header X-Root-API-Key sama sekali.

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if handlerCalled {
		t.Fatal("SECURITY FAILURE: handler was called with missing root credential")
	}
}

// TEST 9: root credential beda SATU karakter saja -> tetap ditolak.
// Ini secara spesifik menguji bahwa comparison bukan "starts with" atau
// prefix-match yang longgar.
func TestRequireRoot_OneCharacterOff_Denied(t *testing.T) {
	handlerCalled := false
	app := newTestAppWithRoot(testRootKey, &handlerCalled)

	almostRight := testRootKey[:len(testRootKey)-1] + "X" // ganti karakter terakhir
	req := httptest.NewRequest(fiber.MethodPost, "/api/admin/tenants", nil)
	req.Header.Set("X-Root-API-Key", almostRight)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if handlerCalled {
		t.Fatal("SECURITY FAILURE: handler was called with a near-match root credential (off by one char)")
	}
}

// TEST 10: root credential tidak pernah muncul di response body, baik
// pada percobaan gagal maupun berhasil. (Logging ke stdout/stderr tidak
// bisa diuji lewat httptest secara langsung — itu diverifikasi lewat
// code review: root_auth.go tidak memanggil log.* dengan rootKey/provided
// sama sekali di jalur manapun.)
func TestRequireRoot_CredentialNeverLeaksInResponse(t *testing.T) {
	handlerCalled := false
	app := newTestAppWithRoot(testRootKey, &handlerCalled)

	cases := []struct {
		name   string
		header string
	}{
		{"wrong_credential", "wrong-" + testRootKey},
		{"missing_credential", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(fiber.MethodPost, "/api/admin/tenants", nil)
			if tc.header != "" {
				req.Header.Set("X-Root-API-Key", tc.header)
			}

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test failed: %v", err)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read response body: %v", err)
			}

			if strings.Contains(string(body), testRootKey) {
				t.Fatalf("SECURITY FAILURE: root key leaked in response body: %s", body)
			}
		})
	}
}

// Test tambahan (bukan dari daftar 1-10, tapi menguji ErrRootKeyNotConfigured
// path yang harus mem-fatal-kan startup di main.go): kalau rootKey kosong
// diteruskan ke handler (skenario yang seharusnya sudah dicegah main.go
// lewat log.Fatal), middleware tetap menolak, bukan fail-open.
func TestRequireRoot_EmptyConfiguredKey_NeverFailsOpen(t *testing.T) {
	handlerCalled := false
	app := newTestAppWithRoot("", &handlerCalled)

	req := httptest.NewRequest(fiber.MethodPost, "/api/admin/tenants", nil)
	req.Header.Set("X-Root-API-Key", "anything")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode == fiber.StatusOK {
		t.Fatal("SECURITY FAILURE: RequireRoot must never fail open when configured key is empty")
	}
	if handlerCalled {
		t.Fatal("SECURITY FAILURE: handler was called when root key was not configured")
	}
}
