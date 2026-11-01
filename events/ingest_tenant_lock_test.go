package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ============================================================
// SIAPA YANG MENGANTRE: TENANT ATAU DATABASE?
//
// TestMeasure_IngestThroughput menemukan rasio paralel 1,8x padahal
// delapan project dijalankan bersamaan — seharusnya mendekati 8 kalau
// project benar-benar independen.
//
// Ada dua penjelasan yang sama masuk akal, dan keduanya menuntut
// tindakan yang sangat berbeda:
//
//	(1) Append mengunci tenant_usage_counters, satu baris per tenant.
//	    Kedelapan project di pengukuran itu milik SATU tenant, jadi
//	    mereka antre di baris kuota yang sama. Kalau ini penyebabnya,
//	    batas laju TrailSeal adalah per-PELANGGAN, bukan per-project —
//	    menambah project tidak menolong pelanggan yang mengirim deras.
//
//	(2) Compute database kecil (branch Neon testing) dan delapan
//	    transaksi bersamaan antre di CPU-nya. Kalau ini penyebabnya,
//	    tidak ada yang salah dengan kodenya sama sekali.
//
// Pembedanya satu: ulangi pengukuran paralel dengan delapan TENANT
// yang berbeda, bukan delapan project dalam satu tenant.
//
//	Rasio naik mendekati 8  -> penyebabnya kunci tenant. (1)
//	Rasio tetap sekitar 1,8 -> penyebabnya database.      (2)
//
//	go test ./events -run Measure_TenantLock -v
// ============================================================

func TestMeasure_TenantLockContention(t *testing.T) {
	if testing.Short() {
		t.Skip("pengukuran, bukan verifikasi; dilewati di -short")
	}

	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	store := NewPostgresStore(pool)

	const (
		workers     = 8
		perWorkerN  = 15
		sequentialN = 30
	)

	req := func(i int) *IngestRequest {
		return &IngestRequest{
			EventType: "tenantlock.measure",
			ActorID:   "bench-actor",
			Payload:   json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
		}
	}

	// Satu project per tenant, tenant-nya berbeda-beda. Dibuat langsung
	// lewat SQL karena helper retention mengunci tenant_id-nya.
	stamp := time.Now().UnixNano()
	type target struct{ projectID, tenantID string }
	targets := make([]target, 0, workers)

	for i := 0; i < workers; i++ {
		tenantID := fmt.Sprintf("tenantlock-%d-%d", stamp, i)

		var projectID string
		err := pool.QueryRow(ctx, `
			INSERT INTO projects (tenant_id, name, status)
			VALUES ($1, $2, 'active')
			RETURNING id
		`, tenantID, fmt.Sprintf("tenantlock-%d-%d", stamp, i)).Scan(&projectID)
		if err != nil {
			t.Fatalf("gagal membuat project untuk tenant %s: %v", tenantID, err)
		}

		targets = append(targets, target{projectID: projectID, tenantID: tenantID})
	}

	t.Cleanup(func() {
		bg := context.Background()
		for _, tg := range targets {
			_, _ = pool.Exec(bg, `DELETE FROM idempotency_keys WHERE project_id = $1`, tg.projectID)
			_, _ = pool.Exec(bg, `DELETE FROM events WHERE project_id = $1`, tg.projectID)
			_, _ = pool.Exec(bg, `DELETE FROM project_chain_state WHERE project_id = $1`, tg.projectID)
			_, _ = pool.Exec(bg, `DELETE FROM project_usage_counters WHERE project_id = $1`, tg.projectID)
			_, _ = pool.Exec(bg, `DELETE FROM event_retention_checkpoints WHERE project_id = $1`, tg.projectID)
			_, _ = pool.Exec(bg, `DELETE FROM projects WHERE id = $1`, tg.projectID)
			_, _ = pool.Exec(bg, `DELETE FROM tenant_usage_counters WHERE tenant_id = $1`, tg.tenantID)
			_, _ = pool.Exec(bg, `DELETE FROM tenant_quotas WHERE tenant_id = $1`, tg.tenantID)
		}
	})

	// Pemanasan sekaligus memastikan tenant baru memang bisa ingest.
	if _, err := store.Append(ctx, targets[0].projectID, targets[0].tenantID, req(0), ""); err != nil {
		t.Fatalf("Append gagal untuk tenant baru: %v", err)
	}

	// ---- Dasar: satu tenant, berurutan ----

	seqStart := time.Now()
	for i := 0; i < sequentialN; i++ {
		if _, err := store.Append(ctx, targets[0].projectID, targets[0].tenantID, req(i), ""); err != nil {
			t.Fatalf("Append berurutan gagal pada iterasi %d: %v", i, err)
		}
	}
	seqElapsed := time.Since(seqStart)
	seqRate := float64(sequentialN) / seqElapsed.Seconds()

	// ---- Paralel: delapan TENANT berbeda ----

	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	parStart := time.Now()
	for _, tg := range targets {
		wg.Add(1)
		go func(tg target) {
			defer wg.Done()
			for i := 0; i < perWorkerN; i++ {
				if _, err := store.Append(ctx, tg.projectID, tg.tenantID, req(i), ""); err != nil {
					errCh <- fmt.Errorf("tenant %s iterasi %d: %w", tg.tenantID, i, err)
					return
				}
			}
		}(tg)
	}
	wg.Wait()
	parElapsed := time.Since(parStart)
	close(errCh)

	for err := range errCh {
		t.Fatalf("Append paralel gagal: %v", err)
	}

	parTotal := workers * perWorkerN
	parRate := float64(parTotal) / parElapsed.Seconds()
	ratio := parRate / seqRate

	verdict := "RASIO RENDAH -> antreannya BUKAN di kunci tenant.\n" +
		"  Delapan tenant berbeda tetap tidak paralel, jadi yang membatasi\n" +
		"  ada di luar kode: kemungkinan besar compute database kecil, atau\n" +
		"  ukuran connection pool. Enam tulisan per ingest bukan masalahnya.\n" +
		"  Ukur ulang dari region yang sama dengan database sebelum\n" +
		"  menyentuh apa pun di jalur ingest."

	if ratio >= float64(workers)/2 {
		verdict = "RASIO TINGGI -> antreannya ADA di kunci tenant.\n" +
			"  Delapan tenant berbeda jalan paralel, delapan project dalam\n" +
			"  satu tenant tidak. Artinya batas laju TrailSeal adalah\n" +
			"  per-PELANGGAN: menambah project tidak menolong pelanggan yang\n" +
			"  mengirim deras, karena setiap ingest mengunci satu baris\n" +
			"  tenant_usage_counters milik tenant itu.\n" +
			"  Server tetap menskala lintas pelanggan. Yang punya langit-langit\n" +
			"  adalah setiap pelanggan, satu per satu."
	}

	t.Logf("\n"+
		"=====================================================\n"+
		"  Berurutan, satu tenant : %.1f event/detik\n"+
		"  Paralel, %d TENANT beda : %.1f event/detik\n"+
		"  Rasio                   : %.1fx\n"+
		"\n"+
		"  (pembanding: %d PROJECT dalam satu tenant = 1.8x)\n"+
		"=====================================================\n"+
		"\n%s\n",
		seqRate, workers, parRate, ratio, workers, verdict,
	)
}
