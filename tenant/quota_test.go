package tenant

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// activePeriod — PeriodResetAt di masa depan supaya TryConsumeQuota
// TIDAK mereset CurrentEventCount yang sudah kita set manual.
// Wajib dipakai di semua test yang pre-populate CurrentEventCount.
func activePeriod() *time.Time {
	t := time.Now().Add(15 * 24 * time.Hour)
	return &t
}

// ============================================================
// InMemoryTenantStore — TryConsumeQuota
// ============================================================

func TestTryConsumeQuota_InMemory_AllowsUnderLimit(t *testing.T) {
	store := NewInMemoryTenantStore()
	_ = store.Put(&Tenant{ID: "t1", MonthlyEventLimit: 100})

	allowed, remaining, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected allowed=true under limit")
	}
	if remaining != 99 {
		t.Errorf("expected remaining=99, got %d", remaining)
	}
}

func TestTryConsumeQuota_InMemory_BlocksAtLimit(t *testing.T) {
	store := NewInMemoryTenantStore()
	// PeriodResetAt wajib di-set ke masa depan supaya implementasi
	// tidak mereset CurrentEventCount ke 0 sebelum cek limit.
	_ = store.Put(&Tenant{
		ID:                "t1",
		MonthlyEventLimit: 5,
		CurrentEventCount: 5,
		PeriodResetAt:     activePeriod(),
	})

	allowed, remaining, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected allowed=false when at limit")
	}
	if remaining != 0 {
		t.Errorf("expected remaining=0, got %d", remaining)
	}
}

func TestTryConsumeQuota_InMemory_BlocksOverLimit(t *testing.T) {
	store := NewInMemoryTenantStore()
	_ = store.Put(&Tenant{
		ID:                "t1",
		MonthlyEventLimit: 5,
		CurrentEventCount: 10,
		PeriodResetAt:     activePeriod(),
	})

	allowed, _, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected allowed=false when over limit")
	}
}

func TestTryConsumeQuota_InMemory_FallsBackToDefault(t *testing.T) {
	store := NewInMemoryTenantStore()
	// MonthlyEventLimit=0 → fallback ke DefaultMonthlyEventLimit
	_ = store.Put(&Tenant{ID: "t1"})

	allowed, _, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected allowed=true under default limit")
	}
}

func TestTryConsumeQuota_InMemory_ResetsAfterPeriod(t *testing.T) {
	store := NewInMemoryTenantStore()
	past := time.Now().Add(-31 * 24 * time.Hour)
	_ = store.Put(&Tenant{
		ID:                "t1",
		MonthlyEventLimit: 5,
		CurrentEventCount: 5,
		PeriodResetAt:     &past,
	})

	// Period sudah lewat → reset → boleh lagi
	allowed, _, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected allowed=true after period reset")
	}
}

func TestTryConsumeQuota_InMemory_DoesNotResetBeforePeriod(t *testing.T) {
	store := NewInMemoryTenantStore()
	_ = store.Put(&Tenant{
		ID:                "t1",
		MonthlyEventLimit: 5,
		CurrentEventCount: 5,
		PeriodResetAt:     activePeriod(),
	})

	allowed, _, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected allowed=false — period not yet expired")
	}
}

func TestTryConsumeQuota_InMemory_UnknownTenant(t *testing.T) {
	store := NewInMemoryTenantStore()

	_, _, err := store.TryConsumeQuota("does-not-exist", 1)
	if err == nil {
		t.Fatal("expected error for unknown tenant")
	}
}

func TestTryConsumeQuota_InMemory_SoftDeletedTenant(t *testing.T) {
	store := NewInMemoryTenantStore()
	now := time.Now()
	_ = store.Put(&Tenant{ID: "t1", DeletedAt: &now})

	_, _, err := store.TryConsumeQuota("t1", 1)
	if err == nil {
		t.Fatal("expected error for soft-deleted tenant")
	}
}

func TestTryConsumeQuota_InMemory_CounterIncrements(t *testing.T) {
	store := NewInMemoryTenantStore()
	_ = store.Put(&Tenant{ID: "t1", MonthlyEventLimit: 10})

	for i := 1; i <= 3; i++ {
		allowed, remaining, err := store.TryConsumeQuota("t1", 1)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Fatalf("call %d: expected allowed=true", i)
		}
		want := int64(10 - i)
		if remaining != want {
			t.Errorf("call %d: expected remaining=%d, got %d", i, want, remaining)
		}
	}
}

func TestTryConsumeQuota_InMemory_ConcurrentNeverExceedsLimit(t *testing.T) {
	const limit = 50
	const goroutines = 200

	store := NewInMemoryTenantStore()
	_ = store.Put(&Tenant{ID: "t1", MonthlyEventLimit: limit})

	var wg sync.WaitGroup
	results := make([]bool, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ok, _, _ := store.TryConsumeQuota("t1", 1)
			results[idx] = ok
		}(i)
	}
	wg.Wait()

	count := 0
	for _, ok := range results {
		if ok {
			count++
		}
	}
	if count > limit {
		t.Errorf("quota exceeded: %d allowed out of limit %d", count, limit)
	}
}

// ============================================================
// BoltTenantStore — TryConsumeQuota
// ============================================================

func openQuotaTestDB(t *testing.T) (*bolt.DB, func()) {
	t.Helper()
	db, err := bolt.Open(filepath.Join(t.TempDir(), "quota_test.db"), 0600, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}
	return db, func() { _ = db.Close() }
}

func TestTryConsumeQuota_Bolt_AllowsUnderLimit(t *testing.T) {
	db, cleanup := openQuotaTestDB(t)
	defer cleanup()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore: %v", err)
	}
	_ = store.Put(&Tenant{ID: "t1", MonthlyEventLimit: 100})

	allowed, remaining, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected allowed=true")
	}
	if remaining != 99 {
		t.Errorf("expected remaining=99, got %d", remaining)
	}
}

func TestTryConsumeQuota_Bolt_BlocksAtLimit(t *testing.T) {
	db, cleanup := openQuotaTestDB(t)
	defer cleanup()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore: %v", err)
	}
	_ = store.Put(&Tenant{
		ID:                "t1",
		MonthlyEventLimit: 5,
		CurrentEventCount: 5,
		PeriodResetAt:     activePeriod(),
	})

	allowed, remaining, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected allowed=false")
	}
	if remaining != 0 {
		t.Errorf("expected remaining=0, got %d", remaining)
	}
}

func TestTryConsumeQuota_Bolt_ResetsAfterPeriod(t *testing.T) {
	db, cleanup := openQuotaTestDB(t)
	defer cleanup()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore: %v", err)
	}
	past := time.Now().Add(-31 * 24 * time.Hour)
	_ = store.Put(&Tenant{
		ID:                "t1",
		MonthlyEventLimit: 5,
		CurrentEventCount: 5,
		PeriodResetAt:     &past,
	})

	allowed, _, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected allowed=true after period reset")
	}
}

func TestTryConsumeQuota_Bolt_DoesNotResetBeforePeriod(t *testing.T) {
	db, cleanup := openQuotaTestDB(t)
	defer cleanup()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore: %v", err)
	}
	_ = store.Put(&Tenant{
		ID:                "t1",
		MonthlyEventLimit: 5,
		CurrentEventCount: 5,
		PeriodResetAt:     activePeriod(),
	})

	allowed, _, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected allowed=false — period not yet expired")
	}
}

func TestTryConsumeQuota_Bolt_UnknownTenant(t *testing.T) {
	db, cleanup := openQuotaTestDB(t)
	defer cleanup()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore: %v", err)
	}

	_, _, err = store.TryConsumeQuota("does-not-exist", 1)
	if err == nil {
		t.Fatal("expected error for unknown tenant")
	}
}

func TestTryConsumeQuota_Bolt_PersistsCounterAcrossCalls(t *testing.T) {
	db, cleanup := openQuotaTestDB(t)
	defer cleanup()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore: %v", err)
	}
	_ = store.Put(&Tenant{ID: "t1", MonthlyEventLimit: 10})

	for i := 0; i < 5; i++ {
		allowed, _, err := store.TryConsumeQuota("t1", 1)
		if err != nil || !allowed {
			t.Fatalf("call %d: unexpected result allowed=%v err=%v", i, allowed, err)
		}
	}

	got, err := store.Get("t1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.CurrentEventCount != 5 {
		t.Errorf("expected CurrentEventCount=5, got %d", got.CurrentEventCount)
	}
}

func TestTryConsumeQuota_Bolt_PersistsCounterAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	db1, err := bolt.Open(filepath.Join(dir, "quota.db"), 0600, nil)
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	store1, err := NewBoltTenantStore(db1)
	if err != nil {
		t.Fatalf("NewBoltTenantStore: %v", err)
	}
	_ = store1.Put(&Tenant{ID: "t1", MonthlyEventLimit: 10})
	_, _, _ = store1.TryConsumeQuota("t1", 1)
	_, _, _ = store1.TryConsumeQuota("t1", 1)
	_ = db1.Close()

	db2, err := bolt.Open(filepath.Join(dir, "quota.db"), 0600, nil)
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer db2.Close()
	store2, err := NewBoltTenantStore(db2)
	if err != nil {
		t.Fatalf("NewBoltTenantStore: %v", err)
	}

	got, err := store2.Get("t1")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.CurrentEventCount != 2 {
		t.Errorf("expected CurrentEventCount=2 after reopen, got %d", got.CurrentEventCount)
	}
}

func TestTryConsumeQuota_Bolt_FallsBackToDefault(t *testing.T) {
	db, cleanup := openQuotaTestDB(t)
	defer cleanup()

	store, err := NewBoltTenantStore(db)
	if err != nil {
		t.Fatalf("NewBoltTenantStore: %v", err)
	}
	// MonthlyEventLimit=0 → fallback ke DefaultMonthlyEventLimit
	_ = store.Put(&Tenant{ID: "t1"})

	allowed, _, err := store.TryConsumeQuota("t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected allowed=true under default limit")
	}
}
