package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================
// KUOTA DI BAWAH BEBAN BERSAMAAN
//
// Append menjaga kuota bulanan dengan mengunci satu baris
// tenant_usage_counters. Kalau penguncian itu diubah — misalnya
// dipindah ke akhir transaksi supaya tidak menahan laju — perilaku yang
// HARUS tetap sama adalah: tidak satu pun event melewati batas, berapa
// pun banyaknya permintaan yang datang bersamaan.
//
// Gejala kalau penjagaannya salah bukan error, melainkan diam: pelanggan
// menulis lebih banyak event daripada yang dibayarnya, dan Anda baru tahu
// saat menagih. Tidak ada test lain yang akan menangkap itu.
//
// Test ini sengaja memakai BANYAK PROJECT dalam SATU TENANT, karena
// kuota TrailSeal dikumpulkan per tenant. Project yang berbeda tidak
// saling mengunci chain state-nya, jadi tekanan benar-benar jatuh ke
// baris kuota tenant — persis titik yang diubah.
//
// Harus hijau SEBELUM dan SESUDAH perubahan apa pun pada penguncian.
//
//	go test ./events -run Quota_ConcurrentAppends -v
// ============================================================

func TestQuota_ConcurrentAppendsNeverExceedLimit(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	store := NewPostgresStore(pool)

	const (
		projectCount  = 5
		perProjectN   = 10
		attempts      = projectCount * perProjectN
		quotaLimit    = 20 // sengaja di bawah attempts
		retentionDays = 365
	)

	stamp := time.Now().UnixNano()
	tenantID := fmt.Sprintf("quota-conc-%d", stamp)
	period := currentPeriod()

	projects := make([]string, projectCount)
	for i := range projects {
		if err := pool.QueryRow(ctx, `
			INSERT INTO projects (tenant_id, name, status)
			VALUES ($1, $2, 'active')
			RETURNING id
		`, tenantID, fmt.Sprintf("quota-conc-%d-%d", stamp, i)).Scan(&projects[i]); err != nil {
			t.Fatalf("gagal membuat project %d: %v", i, err)
		}
	}

	t.Cleanup(func() {
		bg := context.Background()
		for _, pid := range projects {
			_, _ = pool.Exec(bg, `DELETE FROM idempotency_keys WHERE project_id = $1`, pid)
			_, _ = pool.Exec(bg, `DELETE FROM events WHERE project_id = $1`, pid)
			_, _ = pool.Exec(bg, `DELETE FROM project_chain_state WHERE project_id = $1`, pid)
			_, _ = pool.Exec(bg, `DELETE FROM project_usage_counters WHERE project_id = $1`, pid)
			_, _ = pool.Exec(bg, `DELETE FROM event_retention_checkpoints WHERE project_id = $1`, pid)
			_, _ = pool.Exec(bg, `DELETE FROM projects WHERE id = $1`, pid)
		}
		_, _ = pool.Exec(bg, `DELETE FROM tenant_usage_counters WHERE tenant_id = $1`, tenantID)
		_, _ = pool.Exec(bg, `DELETE FROM tenant_quotas WHERE tenant_id = $1`, tenantID)
	})

	// Kuota tenant dipasang persis di bawah jumlah percobaan, supaya
	// sebagian permintaan WAJIB ditolak.
	if _, err := pool.Exec(ctx, `
		INSERT INTO tenant_quotas (tenant_id, monthly_event_limit)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id) DO UPDATE SET monthly_event_limit = $2
	`, tenantID, int64(quotaLimit)); err != nil {
		t.Fatalf("gagal memasang kuota tenant: %v", err)
	}

	var accepted, rejected, unexpected int64

	var wg sync.WaitGroup
	for _, pid := range projects {
		wg.Add(1)
		go func(projectID string) {
			defer wg.Done()
			for i := 0; i < perProjectN; i++ {
				_, err := store.Append(ctx, projectID, tenantID, &IngestRequest{
					EventType: "quota.concurrency.test",
					ActorID:   "actor",
					Payload:   json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
				}, "")

				switch {
				case err == nil:
					atomic.AddInt64(&accepted, 1)
				case errors.Is(err, ErrQuotaExceeded):
					atomic.AddInt64(&rejected, 1)
				default:
					atomic.AddInt64(&unexpected, 1)
					t.Errorf("error tak terduga dari Append: %v", err)
				}
			}
		}(pid)
	}
	wg.Wait()

	if unexpected > 0 {
		t.Fatalf("%d Append gagal dengan error di luar kuota", unexpected)
	}

	// ---- Tidak boleh ada yang menembus batas ----

	if accepted > quotaLimit {
		t.Errorf(
			"KUOTA TERTEMBUS: %d event diterima padahal batasnya %d.\n"+
				"Dua permintaan bersamaan sama-sama lolos pemeriksaan sebelum "+
				"salah satunya menambah penghitung. Pelanggan menulis lebih "+
				"banyak daripada yang dibayarnya, dan tidak ada error yang "+
				"muncul di mana pun.",
			accepted, quotaLimit,
		)
	}
	if accepted != quotaLimit {
		t.Errorf("diterima %d event, harusnya tepat %d — batasnya harus terpakai habis, "+
			"bukan kurang", accepted, quotaLimit)
	}
	if accepted+rejected != attempts {
		t.Errorf("diterima %d + ditolak %d = %d, harusnya %d",
			accepted, rejected, accepted+rejected, attempts)
	}

	// ---- Penghitung dan isi tabel harus sepakat ----

	var counter int64
	if err := pool.QueryRow(ctx, `
		SELECT event_count FROM tenant_usage_counters
		WHERE tenant_id = $1 AND period = $2
	`, tenantID, period).Scan(&counter); err != nil {
		t.Fatalf("gagal membaca tenant_usage_counters: %v", err)
	}
	if counter != accepted {
		t.Errorf("tenant_usage_counters=%d tapi %d event diterima — penghitung "+
			"kuota dan kenyataan berbeda, jadi tagihan Anda akan salah ke salah "+
			"satu arah", counter, accepted)
	}

	var stored int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM events WHERE tenant_id = $1
	`, tenantID).Scan(&stored); err != nil {
		t.Fatalf("gagal menghitung events: %v", err)
	}
	if stored != accepted {
		t.Errorf("tersimpan %d event tapi %d dilaporkan diterima — transaksi yang "+
			"ditolak meninggalkan jejak, atau sebaliknya", stored, accepted)
	}
}
