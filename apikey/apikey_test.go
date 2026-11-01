package apikey

import (
	"strings"
	"testing"
)

// ============================================================
// SCOPE VALIDATION
// ============================================================

func TestScope_Valid(t *testing.T) {
	tests := []struct {
		scope Scope
		want  bool
	}{
		{ScopeIngest, true},
		{ScopeRead, true},
		{ScopeAdmin, true},
		{Scope("bogus"), false},
		{Scope(""), false},
	}

	for _, tt := range tests {
		if got := tt.scope.Valid(); got != tt.want {
			t.Errorf("Scope(%q).Valid() = %v, want %v", tt.scope, got, tt.want)
		}
	}
}

func TestScope_CanIngest(t *testing.T) {
	if !ScopeIngest.CanIngest() {
		t.Error("ScopeIngest should be able to ingest")
	}
	if !ScopeAdmin.CanIngest() {
		t.Error("ScopeAdmin should be able to ingest")
	}
	if ScopeRead.CanIngest() {
		t.Error("ScopeRead should NOT be able to ingest")
	}
}

func TestScope_CanRead(t *testing.T) {
	if !ScopeRead.CanRead() {
		t.Error("ScopeRead should be able to read")
	}
	if !ScopeAdmin.CanRead() {
		t.Error("ScopeAdmin should be able to read")
	}
	if ScopeIngest.CanRead() {
		t.Error("ScopeIngest should NOT be able to read")
	}
}

// ============================================================
// GENERATE API KEY
// ============================================================

func TestGenerateAPIKey_Success(t *testing.T) {
	generated, err := GenerateAPIKey("tenant-1", "project-1", "prod key", ScopeIngest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if generated.Record.TenantID != "tenant-1" {
		t.Errorf("expected TenantID tenant-1, got %s", generated.Record.TenantID)
	}
	if generated.Record.ProjectID != "project-1" {
		t.Errorf("expected ProjectID project-1, got %s", generated.Record.ProjectID)
	}
	if generated.Record.Name != "prod key" {
		t.Errorf("expected Name 'prod key', got %s", generated.Record.Name)
	}
	if generated.Record.Scope != ScopeIngest {
		t.Errorf("expected Scope ingest, got %s", generated.Record.Scope)
	}
	if generated.Record.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestGenerateAPIKey_MissingTenantID_Rejected(t *testing.T) {
	_, err := GenerateAPIKey("", "project-1", "name", ScopeIngest)
	if err == nil {
		t.Fatal("expected error for missing tenant_id")
	}
}

func TestGenerateAPIKey_MissingProjectID_Rejected(t *testing.T) {
	_, err := GenerateAPIKey("tenant-1", "", "name", ScopeIngest)
	if err == nil {
		t.Fatal("expected error for missing project_id")
	}
}

func TestGenerateAPIKey_MissingName_Rejected(t *testing.T) {
	_, err := GenerateAPIKey("tenant-1", "project-1", "", ScopeIngest)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestGenerateAPIKey_InvalidScope_Rejected(t *testing.T) {
	_, err := GenerateAPIKey("tenant-1", "project-1", "name", Scope("bogus"))
	if err == nil {
		t.Fatal("expected error for invalid scope")
	}
	if err != ErrInvalidScope {
		t.Errorf("expected ErrInvalidScope, got %v", err)
	}
}

// PlaintextFormat memverifikasi kontrak "tsk_" prefix + hex — komentar
// di kode menjanjikan format ini untuk memudahkan identifikasi di
// log/secret scanner, jadi ini bagian dari kontrak publik yang wajib
// tidak berubah diam-diam.
func TestGenerateAPIKey_PlaintextFormat(t *testing.T) {
	generated, err := GenerateAPIKey("tenant-1", "project-1", "name", ScopeRead)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(generated.Plaintext, "tsk_") {
		t.Errorf("expected plaintext to start with 'tsk_', got %s", generated.Plaintext)
	}
	// "tsk_" (4 char) + 64 hex char (32 byte random) = 68 total.
	if len(generated.Plaintext) != 68 {
		t.Errorf("expected plaintext length 68, got %d", len(generated.Plaintext))
	}
}

func TestGenerateAPIKey_KeyPrefixMatchesPlaintextStart(t *testing.T) {
	generated, err := GenerateAPIKey("tenant-1", "project-1", "name", ScopeRead)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if generated.Record.KeyPrefix != generated.Plaintext[:12] {
		t.Errorf("expected KeyPrefix to match first 12 chars of plaintext")
	}
	if len(generated.Record.KeyPrefix) != 12 {
		t.Errorf("expected KeyPrefix length 12, got %d", len(generated.Record.KeyPrefix))
	}
}

// KeyHashMatchesHashKey memverifikasi Record.KeyHash yang disimpan
// benar-benar hasil HashKey(plaintext) — kalau ini salah, FindByPlaintext
// tidak akan pernah bisa menemukan key yang baru dibuat.
func TestGenerateAPIKey_KeyHashMatchesHashKeyOfPlaintext(t *testing.T) {
	generated, err := GenerateAPIKey("tenant-1", "project-1", "name", ScopeAdmin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedHash := HashKey(generated.Plaintext)
	if generated.Record.KeyHash != expectedHash {
		t.Error("Record.KeyHash does not match HashKey(plaintext) — FindByPlaintext lookup would break")
	}
}

// PlaintextNeverEqualsHash — sanity check paling dasar bahwa kita
// benar-benar tidak menyimpan plaintext apa adanya sebagai "hash".
func TestGenerateAPIKey_PlaintextIsNeverStoredAsHash(t *testing.T) {
	generated, err := GenerateAPIKey("tenant-1", "project-1", "name", ScopeAdmin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if generated.Record.KeyHash == generated.Plaintext {
		t.Fatal("KeyHash must never equal the raw plaintext key")
	}
}

// Uniqueness — generate berkali-kali tidak boleh collision. 1000 iterasi
// cukup untuk mendeteksi bug generator yang jelas (mis. rand.Read salah
// pakai, atau seed yang sama dipakai ulang) tanpa membuat test lambat.
func TestGenerateAPIKey_NoCollisionsAcrossManyGenerations(t *testing.T) {
	seen := make(map[string]bool)
	const n = 1000

	for i := 0; i < n; i++ {
		generated, err := GenerateAPIKey("tenant-1", "project-1", "name", ScopeIngest)
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
		if seen[generated.Plaintext] {
			t.Fatalf("collision detected on iteration %d: %s", i, generated.Plaintext)
		}
		seen[generated.Plaintext] = true
	}
}

// ============================================================
// HASH KEY
// ============================================================

func TestHashKey_Deterministic(t *testing.T) {
	plaintext := "tsk_abc123"
	h1 := HashKey(plaintext)
	h2 := HashKey(plaintext)

	if h1 != h2 {
		t.Errorf("expected HashKey to be deterministic, got %s vs %s", h1, h2)
	}
}

func TestHashKey_DifferentInputsProduceDifferentHashes(t *testing.T) {
	h1 := HashKey("tsk_aaa")
	h2 := HashKey("tsk_bbb")

	if h1 == h2 {
		t.Error("expected different plaintexts to produce different hashes")
	}
}

func TestHashKey_OutputIsHexSHA256Length(t *testing.T) {
	h := HashKey("tsk_abc123")
	// SHA-256 = 32 byte = 64 hex char.
	if len(h) != 64 {
		t.Errorf("expected hash length 64 (hex SHA-256), got %d", len(h))
	}
}

func TestHashKey_NeverReturnsPlaintextVerbatim(t *testing.T) {
	plaintext := "tsk_abc123"
	h := HashKey(plaintext)
	if h == plaintext {
		t.Fatal("HashKey must not return the plaintext unchanged")
	}
}