package apikey

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ============================================================
// TEST INFRASTRUCTURE
//
//   $env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//   go test ./apikey -v
// ============================================================

func apikeyTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TRAILSEAL_TEST_POSTGRES_URL is not set; apikey postgres integration tests skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create postgres pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping postgres: %v", err)
	}

	// FIX: pool.Close() didaftarkan lewat t.Cleanup (bukan `defer` di
	// tiap caller) supaya jalan LIFO SETELAH t.Cleanup(cleanupProject)
	// yang didaftarkan test. `defer pool.Close()` di caller menutup
	// pool sebelum cleanupProject sempat jalan, sehingga project tidak
	// pernah benar-benar terhapus dan menumpuk di DB test.
	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// seedProject insert baris projects langsung lewat SQL (bukan lewat
// package project) supaya test apikey tidak bergantung pada signature
// project.PostgresStore.Create — api_keys.project_id butuh FK valid,
// itu satu-satunya alasan project harus ada dulu.
func seedProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, name string) string {
	t.Helper()
	var projectID string
	err := pool.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name) VALUES ($1, $2) RETURNING id
	`, tenantID, name).Scan(&projectID)
	if err != nil {
		t.Fatalf("failed to seed project: %v", err)
	}
	return projectID
}

func cleanupProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID string) {
	t.Helper()
	// api_keys punya FK ke projects — hapus dulu sebelum project.
	if _, err := pool.Exec(ctx, `DELETE FROM api_keys WHERE project_id = $1`, projectID); err != nil {
		t.Logf("cleanup api_keys for project %s: %v", projectID, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		t.Logf("cleanup project %s: %v", projectID, err)
	}
}

// ============================================================
// Create
// ============================================================

func TestPostgresStore_Create_Success(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "test project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)
	generated, err := GenerateAPIKey("tenant-1", projectID, "prod key", ScopeIngest)
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}

	if err := store.Create(ctx, generated.Record); err != nil {
		t.Fatalf("unexpected error creating key: %v", err)
	}

	if generated.Record.ID == "" {
		t.Error("expected ID to be populated after Create")
	}
	if generated.Record.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated after Create")
	}
}

func TestPostgresStore_Create_MissingKeyHash_Rejected(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	store := NewPostgresStore(pool)
	key := &APIKey{
		ProjectID: "00000000-0000-0000-0000-000000000000",
		TenantID:  "tenant-1",
		Name:      "no hash",
		Scope:     ScopeIngest,
		// KeyHash sengaja kosong
	}

	err := store.Create(ctx, key)
	if err == nil {
		t.Fatal("expected error for missing key_hash")
	}
}

func TestPostgresStore_Create_InvalidScope_Rejected(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	store := NewPostgresStore(pool)
	key := &APIKey{
		ProjectID: "00000000-0000-0000-0000-000000000000",
		TenantID:  "tenant-1",
		Name:      "bad scope",
		KeyHash:   "somehash",
		Scope:     Scope("bogus"),
	}

	err := store.Create(ctx, key)
	if !errors.Is(err, ErrInvalidScope) {
		t.Errorf("expected ErrInvalidScope, got %v", err)
	}
}

func TestPostgresStore_Create_UnknownProjectID_FailsForeignKey(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	store := NewPostgresStore(pool)
	generated, err := GenerateAPIKey("tenant-1", "00000000-0000-0000-0000-000000000000", "orphan key", ScopeRead)
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}

	err = store.Create(ctx, generated.Record)
	if err == nil {
		t.Fatal("expected FK violation error for nonexistent project_id")
	}
}

// UniqueKeyHash memverifikasi UNIQUE INDEX idx_api_keys_hash benar
// mencegah dua key dengan hash sama tersimpan — walaupun secara
// praktik ini nyaris mustahil terjadi secara alami (32 byte random),
// index ini adalah safety net terakhir.
func TestPostgresStore_Create_DuplicateKeyHash_Rejected(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "dup hash project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)

	// Unik per run — hindari row tersisa dari eksekusi sebelumnya
	// yang gagal di tengah jalan (t.Cleanup tidak sempat/gagal jalan)
	// bikin test ini false-fail selamanya kalau nilainya konstan.
	sharedHash := "dup-hash-" + time.Now().Format("20060102150405.000000000")

	first := &APIKey{
		ProjectID: projectID,
		TenantID:  "tenant-1",
		Name:      "first",
		KeyHash:   sharedHash,
		KeyPrefix: "tsk_aaaaaaaa",
		Scope:     ScopeRead,
	}
	if err := store.Create(ctx, first); err != nil {
		t.Fatalf("unexpected error creating first key: %v", err)
	}

	second := &APIKey{
		ProjectID: projectID,
		TenantID:  "tenant-1",
		Name:      "second",
		KeyHash:   sharedHash,
		KeyPrefix: "tsk_bbbbbbbb",
		Scope:     ScopeRead,
	}
	if err := store.Create(ctx, second); err == nil {
		t.Error("expected duplicate key_hash to be rejected by UNIQUE constraint")
	}
}

// ============================================================
// FindByPlaintext
// ============================================================

func TestPostgresStore_FindByPlaintext_Success(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "find test project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-1", projectID, "findable key", ScopeAdmin)
	if err := store.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to create key: %v", err)
	}

	found, err := store.FindByPlaintext(ctx, generated.Plaintext)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != generated.Record.ID {
		t.Errorf("expected to find key with ID %s, got %s", generated.Record.ID, found.ID)
	}
	if found.Scope != ScopeAdmin {
		t.Errorf("expected scope admin, got %s", found.Scope)
	}
}

func TestPostgresStore_FindByPlaintext_UnknownKey_ReturnsNotFound(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	store := NewPostgresStore(pool)
	_, err := store.FindByPlaintext(ctx, "tsk_this_key_never_existed_0000000000000000000000000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// FindByPlaintext_RevokedKey_ReturnsRevoked adalah inti keamanan
// paling penting: key yang sudah di-revoke TIDAK BOLEH bisa dipakai
// lagi untuk autentikasi, walau hash-nya masih ada di DB.
func TestPostgresStore_FindByPlaintext_RevokedKey_ReturnsRevoked(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "revoke test project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-1", projectID, "to be revoked", ScopeRead)
	if err := store.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to create key: %v", err)
	}

	if err := store.Revoke(ctx, "tenant-1", projectID, generated.Record.ID); err != nil {
		t.Fatalf("failed to revoke key: %v", err)
	}

	_, err := store.FindByPlaintext(ctx, generated.Plaintext)
	if !errors.Is(err, ErrRevoked) {
		t.Errorf("expected ErrRevoked for revoked key, got %v", err)
	}
}

// ============================================================
// ListByProject
// ============================================================

func TestPostgresStore_ListByProject_ReturnsOnlyMatchingProjectAndTenant(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectA := seedProject(t, ctx, pool, "tenant-1", "project A")
	projectB := seedProject(t, ctx, pool, "tenant-1", "project B")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectA) })
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectB) })

	store := NewPostgresStore(pool)

	genA, _ := GenerateAPIKey("tenant-1", projectA, "key in A", ScopeRead)
	_ = store.Create(ctx, genA.Record)

	genB, _ := GenerateAPIKey("tenant-1", projectB, "key in B", ScopeRead)
	_ = store.Create(ctx, genB.Record)

	keys, err := store.ListByProject(ctx, "tenant-1", projectA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected exactly 1 key for project A, got %d", len(keys))
	}
	if keys[0].ID != genA.Record.ID {
		t.Error("expected returned key to belong to project A")
	}
}

// ListByProject_WrongTenant_CrossTenantIsolation adalah pengecekan
// keamanan multi-tenant paling penting untuk endpoint ini: tenant B
// tidak boleh bisa melihat key milik project yang secara ID sama
// persis tapi tenant_id-nya berbeda (mis. tebak project_id).
func TestPostgresStore_ListByProject_WrongTenantID_ReturnsEmpty(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-owner", "isolated project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-owner", projectID, "owner's key", ScopeRead)
	if err := store.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to create key: %v", err)
	}

	keys, err := store.ListByProject(ctx, "tenant-attacker", projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys when tenant_id does not match project owner, got %d", len(keys))
	}
}

func TestPostgresStore_ListByProject_OrderedNewestFirst(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "order test project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)

	gen1, _ := GenerateAPIKey("tenant-1", projectID, "first", ScopeRead)
	_ = store.Create(ctx, gen1.Record)
	time.Sleep(10 * time.Millisecond) // pastikan created_at berbeda
	gen2, _ := GenerateAPIKey("tenant-1", projectID, "second", ScopeRead)
	_ = store.Create(ctx, gen2.Record)

	keys, err := store.ListByProject(ctx, "tenant-1", projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0].ID != gen2.Record.ID {
		t.Error("expected newest key (second) to be first in the list")
	}
}

// ============================================================
// Revoke
// ============================================================

func TestPostgresStore_Revoke_Success(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "revoke success project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-1", projectID, "to revoke", ScopeIngest)
	if err := store.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to create key: %v", err)
	}

	if err := store.Revoke(ctx, "tenant-1", projectID, generated.Record.ID); err != nil {
		t.Fatalf("unexpected error revoking key: %v", err)
	}
}

func TestPostgresStore_Revoke_UnknownKeyID_ReturnsNotFound(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "revoke notfound project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)
	err := store.Revoke(ctx, "tenant-1", projectID, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// Revoke_WrongTenantID_ReturnsNotFound adalah pengecekan keamanan
// KRUSIAL: tenant B tidak boleh bisa me-revoke key milik tenant A
// walau tahu/menebak key_id yang benar. Query WHERE di store harus
// menyertakan tenant_id, bukan cuma id.
func TestPostgresStore_Revoke_WrongTenantID_ReturnsNotFoundNotSuccess(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-victim", "victim project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-victim", projectID, "victim's key", ScopeAdmin)
	if err := store.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to create key: %v", err)
	}

	err := store.Revoke(ctx, "tenant-attacker", projectID, generated.Record.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound when tenant_id does not match key owner, got %v", err)
	}

	// Pastikan key MASIH AKTIF — revoke attacker benar-benar tidak berefek.
	found, findErr := store.FindByPlaintext(ctx, generated.Plaintext)
	if findErr != nil {
		t.Fatalf("expected key to still be findable after failed cross-tenant revoke: %v", findErr)
	}
	if found.RevokedAt != nil {
		t.Error("key must NOT be revoked by a request from the wrong tenant")
	}
}

// Revoke_AlreadyRevoked_ReturnsNotFound membuktikan revoke bersifat
// idempotent-safe: revoke kedua pada key yang sudah revoked TIDAK
// dianggap sukses (revoked_at IS NULL di WHERE clause mencegah ini),
// mencegah caller salah kira "berhasil revoke" dua kali.
func TestPostgresStore_Revoke_AlreadyRevoked_ReturnsNotFound(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "double revoke project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-1", projectID, "double revoke key", ScopeRead)
	if err := store.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to create key: %v", err)
	}

	if err := store.Revoke(ctx, "tenant-1", projectID, generated.Record.ID); err != nil {
		t.Fatalf("unexpected error on first revoke: %v", err)
	}

	err := store.Revoke(ctx, "tenant-1", projectID, generated.Record.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on second revoke of already-revoked key, got %v", err)
	}
}

// ============================================================
// UpdateLastUsed
// ============================================================

func TestPostgresStore_UpdateLastUsed_Success(t *testing.T) {
	pool := apikeyTestPool(t)
	ctx := context.Background()

	projectID := seedProject(t, ctx, pool, "tenant-1", "last used project")
	t.Cleanup(func() { cleanupProject(t, context.Background(), pool, projectID) })

	store := NewPostgresStore(pool)
	generated, _ := GenerateAPIKey("tenant-1", projectID, "used key", ScopeIngest)
	if err := store.Create(ctx, generated.Record); err != nil {
		t.Fatalf("failed to create key: %v", err)
	}

	if err := store.UpdateLastUsed(ctx, generated.Record.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := store.FindByPlaintext(ctx, generated.Plaintext)
	if err != nil {
		t.Fatalf("unexpected error re-fetching key: %v", err)
	}
	if found.LastUsedAt == nil {
		t.Error("expected LastUsedAt to be set after UpdateLastUsed")
	}
}
