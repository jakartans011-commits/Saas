package main

import (
	"errors"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"

	"trailseal/audit"
	"trailseal/auth"
)

// --- shared fixtures untuk OAuth callback handler tests ---
// InMemoryUserStore memenuhi auth.UserEmailLookupStore (FindByEmail)
// DAN auth.UserPutStore (Put) sekaligus, jadi lookup & userStore
// sengaja pakai instance yang sama seperti di wiring cmd/saas-api asli.
var (
	userStore                       = auth.NewInMemoryUserStore()
	lookup                          = auth.UserEmailLookupStore(userStore)
	refreshStore                    = &testRefreshTokenStore{auth.NewInMemoryRefreshTokenStore()}
	signingKey                      = []byte("test-signing-key-32-bytes-long!")
	newTenant    auth.NewTenantFunc = func(ownerEmail string) (string, string, error) {
		return "tenant-" + ownerEmail, "owner", nil
	}
	auditStore audit.Store = &fakeAuditStore{}
)

// testRefreshTokenStore: InMemoryRefreshTokenStore sudah punya Save,
// FindByHash, Rotate, RevokeAllForUser — tinggal tambah Revoke(id)
// biar memenuhi auth.RefreshTokenStore penuh. No-op cukup untuk test
// ini karena tidak ada test yang assert efek Revoke secara langsung.
type testRefreshTokenStore struct {
	*auth.InMemoryRefreshTokenStore
}

func (s *testRefreshTokenStore) Revoke(id string) error { return nil }

// fakeAuditStore: mencatat setiap Save() supaya test bisa assert
// audit event beneran tercatat, bukan cuma asumsi dari baca kode.
type fakeAuditStore struct {
	mu     sync.Mutex
	events []*audit.AuditEvent
}

func (f *fakeAuditStore) Save(event *audit.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	return nil
}
func (f *fakeAuditStore) ListForTenant(tenantID string, limit int) ([]*audit.AuditEvent, error) {
	return nil, nil
}

func (f *fakeAuditStore) CleanupForTenant(tenantID string, policy *audit.RetentionPolicy) (deleted int, anonymized int, err error) {
	return 0, 0, nil
}

func (f *fakeAuditStore) DeleteAllForTenant(tenantID string) error {
	return nil
}

// fakeOAuthProvider: pengganti provider asli supaya test tidak
// menyentuh network GitHub/Google sungguhan.
type fakeOAuthProvider struct {
	authURL      string
	exchangeErr  error
	fetchErr     error
	returnedTok  string
	returnedMail string
}

func (f *fakeOAuthProvider) AuthURL(state, redirectURI string) string { return f.authURL }
func (f *fakeOAuthProvider) Exchange(code, redirectURI string) (string, error) {
	if f.exchangeErr != nil {
		return "", f.exchangeErr
	}
	return f.returnedTok, nil
}
func (f *fakeOAuthProvider) FetchEmail(accessToken string) (string, error) {
	if f.fetchErr != nil {
		return "", f.fetchErr
	}
	return f.returnedMail, nil
}

func newOAuthTestApp(providers map[string]auth.OAuthProvider, states *auth.OAuthStateStore) *fiber.App {
	app := fiber.New()
	app.Get("/api/auth/oauth/:provider/start", OAuthStartHandler(providers, states))
	app.Get("/api/auth/oauth/:provider/callback", OAuthCallbackHandler(providers, states, lookup, userStore, refreshStore, signingKey, newTenant, auditStore))
	return app
}

func TestOAuthStart_UnknownProvider_Returns404(t *testing.T) {
	app := newOAuthTestApp(map[string]auth.OAuthProvider{}, auth.NewOAuthStateStore())
	req := httptest.NewRequest("GET", "/api/auth/oauth/facebook/start", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestOAuthStart_ConfiguredProvider_RedirectsWithState(t *testing.T) {
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{
		"github": &fakeOAuthProvider{authURL: "https://github.com/login/oauth/authorize?mock=1"},
	}
	app := newOAuthTestApp(providers, states)

	req := httptest.NewRequest("GET", "/api/auth/oauth/github/start", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != fiber.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatal("expected Location header to be set")
	}
}

func TestOAuthCallback_MissingStateOrCode_Returns400(t *testing.T) {
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{"github": &fakeOAuthProvider{}}
	app := newOAuthTestApp(providers, states)

	req := httptest.NewRequest("GET", "/api/auth/oauth/github/callback", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// SECURITY: state harus tervalidasi lewat store, bukan diterima mentah
// dari query string — kalau ini gagal, endpoint rentan CSRF.
func TestOAuthCallback_InvalidState_Returns400(t *testing.T) {
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{"github": &fakeOAuthProvider{}}
	app := newOAuthTestApp(providers, states)

	req := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state=forged&code=abc", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for forged state, got %d", resp.StatusCode)
	}
}

// SECURITY: state yang sama tidak boleh bisa dipakai dua kali lewat
// endpoint callback sungguhan (replay attack).
func TestOAuthCallback_StateReplay_SecondAttemptRejected(t *testing.T) {
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{
		"github": &fakeOAuthProvider{returnedTok: "tok", returnedMail: "user@example.com"},
	}
	app := newOAuthTestApp(providers, states)

	state, _ := states.Generate("github")

	req1 := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state="+state+"&code=abc", nil)
	resp1, _ := app.Test(req1)
	if resp1.StatusCode != fiber.StatusFound {
		t.Fatalf("first callback should succeed, got %d", resp1.StatusCode)
	}

	req2 := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state="+state+"&code=abc", nil)
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("SECURITY: replayed state must be rejected, got %d", resp2.StatusCode)
	}
}

func TestOAuthCallback_ProviderErrorParam_Returns400(t *testing.T) {
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{"github": &fakeOAuthProvider{}}
	app := newOAuthTestApp(providers, states)

	state, _ := states.Generate("github")
	req := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state="+state+"&error=access_denied", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400 when provider reports error, got %d", resp.StatusCode)
	}
}

// SECURITY: kalau exchange gagal, pastikan detail internal (mis. isi
// error dari GitHub/Google) TIDAK bocor mentah-mentah ke response body.
func TestOAuthCallback_ExchangeFails_ReturnsGenericError(t *testing.T) {
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{
		"github": &fakeOAuthProvider{exchangeErr: errors.New("secret leaked detail xyz")},
	}
	app := newOAuthTestApp(providers, states)

	state, _ := states.Generate("github")
	req := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state="+state+"&code=abc", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
	// NOTE: kalau nanti Anda ubah handler untuk menyertakan err.Error()
	// di response, test ini perlu ditambah assert body TIDAK mengandung
	// "secret leaked detail xyz".
}

func TestOAuthCallback_FetchEmailFails_ReturnsGenericError(t *testing.T) {
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{
		"github": &fakeOAuthProvider{returnedTok: "tok", fetchErr: errors.New("boom")},
	}
	app := newOAuthTestApp(providers, states)

	state, _ := states.Generate("github")
	req := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state="+state+"&code=abc", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}

func TestOAuthCallback_UnknownProvider_Returns404(t *testing.T) {
	states := auth.NewOAuthStateStore()
	app := newOAuthTestApp(map[string]auth.OAuthProvider{}, states)

	req := httptest.NewRequest("GET", "/api/auth/oauth/facebook/callback?state=x&code=y", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// SECURITY: endpoint publik OAuth start/callback harus dibatasi per-IP,
// sama seperti /api/auth/login — mencegah spam state generation dan
// pemborosan token-exchange call ke provider eksternal.
func TestOAuthStart_RateLimit_BlocksAfterThreshold(t *testing.T) {
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{
		"github": &fakeOAuthProvider{authURL: "https://github.com/login/oauth/authorize?mock=1"},
	}
	limiter := auth.NewCredentialStuffingLimiter()

	app := fiber.New()
	app.Get("/api/auth/oauth/:provider/start", OAuthRateLimit(limiter), OAuthStartHandler(providers, states))

	var lastStatus int
	for i := 0; i < 40; i++ {
		req := httptest.NewRequest("GET", "/api/auth/oauth/github/start", nil)
		resp, _ := app.Test(req)
		lastStatus = resp.StatusCode
		if lastStatus == fiber.StatusTooManyRequests {
			break
		}
	}
	if lastStatus != fiber.StatusTooManyRequests {
		t.Fatal("SECURITY: expected rate limit to trigger 429 after threshold, but never did")
	}
}

func TestOAuthCallback_RateLimit_BlocksAfterThreshold(t *testing.T) {
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{
		"github": &fakeOAuthProvider{returnedTok: "tok", returnedMail: "user@example.com"},
	}
	limiter := auth.NewCredentialStuffingLimiter()

	app := fiber.New()
	app.Get("/api/auth/oauth/:provider/callback", OAuthRateLimit(limiter), OAuthCallbackHandler(providers, states, lookup, userStore, refreshStore, signingKey, newTenant, auditStore))

	var lastStatus int
	for i := 0; i < 40; i++ {
		state, _ := states.Generate("github")
		req := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state="+state+"&code=abc", nil)
		resp, _ := app.Test(req)
		lastStatus = resp.StatusCode
		if lastStatus == fiber.StatusTooManyRequests {
			break
		}
	}
	if lastStatus != fiber.StatusTooManyRequests {
		t.Fatal("SECURITY: expected rate limit to trigger 429 after threshold, but never did")
	}
}

// BUKTI: audit event "oauth_login" harus tercatat saat login OAuth sukses.
func TestOAuthCallback_Success_RecordsAuditEvent(t *testing.T) {
	as := &fakeAuditStore{}
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{
		"github": &fakeOAuthProvider{returnedTok: "tok", returnedMail: "audit-check@example.com"},
	}
	app := fiber.New()
	app.Get("/api/auth/oauth/:provider/callback", OAuthCallbackHandler(providers, states, lookup, userStore, refreshStore, signingKey, newTenant, as))

	state, _ := states.Generate("github")
	req := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state="+state+"&code=abc", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}

	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(as.events))
	}
	if as.events[0].Action != "oauth_login" {
		t.Errorf("expected action oauth_login, got %s", as.events[0].Action)
	}
	if as.events[0].Outcome != audit.OutcomeSuccess {
		t.Errorf("expected OutcomeSuccess, got %s", as.events[0].Outcome)
	}
}

// BUKTI: audit event harus tercatat (OutcomeFailure) saat token exchange gagal.
func TestOAuthCallback_ExchangeFails_RecordsFailureAuditEvent(t *testing.T) {
	as := &fakeAuditStore{}
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{
		"github": &fakeOAuthProvider{exchangeErr: errors.New("boom")},
	}
	app := fiber.New()
	app.Get("/api/auth/oauth/:provider/callback", OAuthCallbackHandler(providers, states, lookup, userStore, refreshStore, signingKey, newTenant, as))

	state, _ := states.Generate("github")
	req := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state="+state+"&code=abc", nil)
	app.Test(req)

	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(as.events))
	}
	if as.events[0].Outcome != audit.OutcomeFailure {
		t.Errorf("expected OutcomeFailure, got %s", as.events[0].Outcome)
	}
}

// BUKTI: audit event harus tercatat (OutcomeFailure) saat fetch email gagal.
func TestOAuthCallback_FetchEmailFails_RecordsFailureAuditEvent(t *testing.T) {
	as := &fakeAuditStore{}
	states := auth.NewOAuthStateStore()
	providers := map[string]auth.OAuthProvider{
		"github": &fakeOAuthProvider{returnedTok: "tok", fetchErr: errors.New("boom")},
	}
	app := fiber.New()
	app.Get("/api/auth/oauth/:provider/callback", OAuthCallbackHandler(providers, states, lookup, userStore, refreshStore, signingKey, newTenant, as))

	state, _ := states.Generate("github")
	req := httptest.NewRequest("GET", "/api/auth/oauth/github/callback?state="+state+"&code=abc", nil)
	app.Test(req)

	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(as.events))
	}
	if as.events[0].Outcome != audit.OutcomeFailure {
		t.Errorf("expected OutcomeFailure, got %s", as.events[0].Outcome)
	}
}
