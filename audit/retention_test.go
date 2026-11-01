package audit

import (
	"os"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openTestRetentionDB(t *testing.T) (*bolt.DB, func()) {
	t.Helper()
	f, err := os.CreateTemp("", "retention-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	f.Close()

	db, err := bolt.Open(f.Name(), 0600, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(f.Name())
	}
	return db, cleanup
}

func TestRetentionPolicy_Default(t *testing.T) {
	policy := DefaultRetentionPolicy()
	if policy.RetentionDays != DefaultRetentionDays {
		t.Errorf("expected retention days %d, got %d", DefaultRetentionDays, policy.RetentionDays)
	}
	if policy.AnonymizationDays != DefaultAnonymizationDays {
		t.Errorf("expected anonymization days %d, got %d", DefaultAnonymizationDays, policy.AnonymizationDays)
	}
}

func TestRetentionStore_GetPolicy_DefaultsToPlan(t *testing.T) {
	db, cleanup := openTestRetentionDB(t)
	defer cleanup()

	store, err := NewRetentionStore(db)
	if err != nil {
		t.Fatalf("NewRetentionStore failed: %v", err)
	}

	// Tenant Pro (plan retention 180 hari), belum pernah override.
	policy, err := store.GetPolicy("tenant-pro", 180)
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}
	if policy.RetentionDays != 180 {
		t.Errorf("expected retention days to default to plan (180), got %d", policy.RetentionDays)
	}
	if policy.AnonymizationDays != DefaultAnonymizationDays {
		t.Errorf("expected default anonymization days %d, got %d", DefaultAnonymizationDays, policy.AnonymizationDays)
	}
}

func TestRetentionStore_SetPolicy_TenantIsolation(t *testing.T) {
	db, cleanup := openTestRetentionDB(t)
	defer cleanup()

	store, err := NewRetentionStore(db)
	if err != nil {
		t.Fatalf("NewRetentionStore failed: %v", err)
	}

	// Tenant A (Business, plan 365) set retensi lebih pendek (90).
	if err := store.SetPolicy("tenant-a", &RetentionPolicy{
		RetentionDays:     90,
		AnonymizationDays: 15,
	}, 365); err != nil {
		t.Fatalf("SetPolicy tenant-a failed: %v", err)
	}

	// Tenant B tidak pernah di-set — harus tetap default plan-nya sendiri,
	// TIDAK boleh ikut kebawa perubahan tenant A.
	policyB, err := store.GetPolicy("tenant-b", 14)
	if err != nil {
		t.Fatalf("GetPolicy tenant-b failed: %v", err)
	}
	if policyB.RetentionDays != 14 {
		t.Errorf("tenant-b harus tetap default plan (14), got %d — kemungkinan bocor dari tenant lain", policyB.RetentionDays)
	}

	policyA, err := store.GetPolicy("tenant-a", 365)
	if err != nil {
		t.Fatalf("GetPolicy tenant-a failed: %v", err)
	}
	if policyA.RetentionDays != 90 {
		t.Errorf("expected tenant-a retention days 90, got %d", policyA.RetentionDays)
	}
	if policyA.AnonymizationDays != 15 {
		t.Errorf("expected tenant-a anonymization days 15, got %d", policyA.AnonymizationDays)
	}
}

func TestRetentionStore_SetPolicy_CannotExceedPlan(t *testing.T) {
	db, cleanup := openTestRetentionDB(t)
	defer cleanup()

	store, err := NewRetentionStore(db)
	if err != nil {
		t.Fatalf("NewRetentionStore failed: %v", err)
	}

	// Tenant Free (plan retention 14 hari) coba set ke 365 — harus ditolak.
	err = store.SetPolicy("tenant-free", &RetentionPolicy{
		RetentionDays:     365,
		AnonymizationDays: 10,
	}, 14)
	if err == nil {
		t.Fatal("expected error: retention_days melebihi plan, tapi SetPolicy sukses")
	}
}

func TestRetentionStore_SetPolicy_AnonymizationCannotExceedRetention(t *testing.T) {
	db, cleanup := openTestRetentionDB(t)
	defer cleanup()

	store, err := NewRetentionStore(db)
	if err != nil {
		t.Fatalf("NewRetentionStore failed: %v", err)
	}

	err = store.SetPolicy("tenant-x", &RetentionPolicy{
		RetentionDays:     30,
		AnonymizationDays: 60,
	}, 180)
	if err == nil {
		t.Fatal("expected error: anonymization_days > retention_days, tapi SetPolicy sukses")
	}
}

func TestRetentionStore_GetPolicy_ClampsToDowngradedPlan(t *testing.T) {
	db, cleanup := openTestRetentionDB(t)
	defer cleanup()

	store, err := NewRetentionStore(db)
	if err != nil {
		t.Fatalf("NewRetentionStore failed: %v", err)
	}

	// Set saat masih Business (365).
	if err := store.SetPolicy("tenant-downgrade", &RetentionPolicy{
		RetentionDays:     300,
		AnonymizationDays: 30,
	}, 365); err != nil {
		t.Fatalf("SetPolicy failed: %v", err)
	}

	// Tenant downgrade ke Pro (180) — GetPolicy harus clamp turun,
	// bukan tetap 300 (menyandera retensi lebih lama dari yang dibayar).
	policy, err := store.GetPolicy("tenant-downgrade", 180)
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}
	if policy.RetentionDays != 180 {
		t.Errorf("expected clamp to new plan ceiling (180), got %d", policy.RetentionDays)
	}
}

func TestBoltAuditStore_CleanupForTenant(t *testing.T) {
	db, cleanup := openTestRetentionDB(t)
	defer cleanup()

	store, err := NewBoltAuditStore(db)
	if err != nil {
		t.Fatalf("NewBoltAuditStore failed: %v", err)
	}

	now := time.Now()

	events := []*AuditEvent{
		{ID: "recent", TenantID: "tenant-1", UserID: "user-1", Action: "GET /x", Resource: "/x", Outcome: "success", Timestamp: now.Add(-1 * 24 * time.Hour)},
		{ID: "to-anonymize", TenantID: "tenant-1", UserID: "user-2", Action: "GET /y", Resource: "/y", Outcome: "success", Timestamp: now.Add(-40 * 24 * time.Hour), Metadata: map[string]string{"ip": "1.2.3.4", "user_agent": "test-agent", "method": "GET"}},
		{ID: "to-delete", TenantID: "tenant-1", UserID: "user-3", Action: "GET /z", Resource: "/z", Outcome: "success", Timestamp: now.Add(-100 * 24 * time.Hour)},
		{ID: "other-tenant", TenantID: "tenant-2", UserID: "user-4", Action: "GET /w", Resource: "/w", Outcome: "success", Timestamp: now.Add(-100 * 24 * time.Hour)},
	}
	for _, e := range events {
		if err := store.Save(e); err != nil {
			t.Fatalf("Save %s failed: %v", e.ID, err)
		}
	}

	policy := &RetentionPolicy{RetentionDays: 90, AnonymizationDays: 30}

	deleted, anonymized, err := store.CleanupForTenant("tenant-1", policy)
	if err != nil {
		t.Fatalf("CleanupForTenant failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}
	if anonymized != 1 {
		t.Errorf("expected 1 anonymized, got %d", anonymized)
	}

	remaining, err := store.ListForTenant("tenant-1", 50)
	if err != nil {
		t.Fatalf("ListForTenant failed: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining events for tenant-1, got %d", len(remaining))
	}

	for _, e := range remaining {
		if e.ID == "to-anonymize" {
			if e.UserID != "[anonymized]" {
				t.Errorf("expected UserID scrubbed, got %q", e.UserID)
			}
			if _, hasIP := e.Metadata["ip"]; hasIP {
				t.Error("expected ip to be scrubbed from metadata")
			}
			if e.Action != "GET /y" {
				t.Errorf("expected action preserved for compliance trail, got %q", e.Action)
			}
		}
	}

	// tenant-2 tidak boleh tersentuh sama sekali.
	otherTenant, err := store.ListForTenant("tenant-2", 50)
	if err != nil {
		t.Fatalf("ListForTenant tenant-2 failed: %v", err)
	}
	if len(otherTenant) != 1 {
		t.Fatalf("expected tenant-2 untouched (1 event), got %d", len(otherTenant))
	}
	if otherTenant[0].UserID == "[anonymized]" {
		t.Error("tenant-2 event ikut ter-anonymize — cross-tenant bug di CleanupForTenant")
	}
}
