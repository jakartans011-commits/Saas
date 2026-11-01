package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestAppend_QuotaIsPooledPerTenant_NotPerProject membuktikan bug:
// SEBELUM fix, tenant Pro dengan 2 project bisa ingest sampai
// 2x monthly_event_limit (karena quota di-cek per project_id).
// SETELAH fix, total ingest across kedua project dibatasi SATU
// limit tenant (pooled), sesuai Locked Pricing & BUSINESS_MODEL.md.
//
// Pakai retentionTestPool(t) — helper test DB yang sudah ada di
// retention_test.go (package sama), konsisten dengan pola test
// existing di events package. Tenant ID dibuat UNIK per test run
// (bukan konstanta "events-test-tenant" yang dipakai test lain),
// supaya tidak saling kontaminasi kuota antar test sekarang kuota
// sudah pooled per tenant.
func TestAppend_QuotaIsPooledPerTenant_NotPerProject(t *testing.T) {
	pool := retentionTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantID := "events-test-pooled-quota-" + time.Now().Format("20060102150405.000000000")

	const limit = 5 // limit kecil sengaja, supaya test cepat & jelas

	_, err := pool.Exec(ctx, `
		INSERT INTO tenant_quotas (tenant_id, monthly_event_limit)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id) DO UPDATE SET monthly_event_limit = $2
	`, tenantID, limit)
	if err != nil {
		t.Fatalf("failed to seed tenant quota: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM tenant_usage_counters WHERE tenant_id = $1`, tenantID)
		pool.Exec(context.Background(), `DELETE FROM tenant_quotas WHERE tenant_id = $1`, tenantID)
	})

	newProject := func() string {
		var id string
		err := pool.QueryRow(ctx, `
			INSERT INTO projects (tenant_id, name, status, retention_days, monthly_event_limit)
			VALUES ($1, $2, 'active', 90, 999999)
			RETURNING id
		`, tenantID, "pooled-quota-test-"+time.Now().Format("20060102150405.000000000")).Scan(&id)
		if err != nil {
			t.Fatalf("failed to create test project: %v", err)
		}
		t.Cleanup(func() {
			cleanupRetentionTestProject(t, context.Background(), pool, id)
		})
		return id
	}

	projectAID := newProject()
	projectBID := newProject()

	store := NewPostgresStore(pool)

	ingest := func(projectID string) error {
		req := &IngestRequest{
			EventType: "test.event",
			ActorID:   "tester",
			Payload:   json.RawMessage(`{}`),
		}
		_, err := store.Append(ctx, projectID, tenantID, req, "")
		return err
	}

	for i := 0; i < limit; i++ {
		if err := ingest(projectAID); err != nil {
			t.Fatalf("ingest #%d on project A should succeed, got: %v", i+1, err)
		}
	}

	if err := ingest(projectAID); err != ErrQuotaExceeded {
		t.Fatalf("expected ErrQuotaExceeded on project A after limit reached, got: %v", err)
	}

	if err := ingest(projectBID); err != ErrQuotaExceeded {
		t.Fatalf(
			"BUG: quota tidak pooled per tenant — ingest via project B (project "+
				"berbeda, tenant sama) berhasil padahal limit tenant sudah habis. got: %v",
			err,
		)
	}

	count, err := store.GetTenantUsageCount(ctx, tenantID, CurrentPeriod())
	if err != nil {
		t.Fatalf("GetTenantUsageCount failed: %v", err)
	}
	if count != limit {
		t.Fatalf("expected tenant usage count = %d (pooled), got %d", limit, count)
	}
}
