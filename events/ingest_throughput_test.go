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
// PENGUKURAN LAJU INGEST
//
// Ini PENGUKURAN, bukan verifikasi: ia tidak pernah gagal karena angkanya
// jelek, hanya melaporkan. Dilewati oleh -short supaya CI tidak melambat.
//
//	go test ./events -run Measure_IngestThroughput -v
//
// Yang diukur dan mengapa:
//
// Append menulis enam kali dalam SATU transaksi yang mengunci
// project_chain_state dengan SELECT ... FOR UPDATE. Kunci itu per-project,
// bukan global. Jadi pertanyaan yang benar bukan "berapa event per detik
// yang sanggup server ini" melainkan dua pertanyaan terpisah:
//
//	A. Berapa laju maksimum SATU project? Ini yang dibatasi kunci.
//	B. Apakah project yang berbeda benar-benar jalan paralel?
//
// Kalau B jauh lebih tinggi dari A, arsitekturnya sehat: pelanggan yang
// berbeda tidak saling menghalangi, dan batas per-project baru jadi
// masalah kalau ada satu pelanggan yang mengirim sangat deras ke satu
// project. Kalau B hampir sama dengan A, ada penyempitan global yang
// jauh lebih serius daripada enam tulisan itu sendiri.
//
// CATATAN PENTING soal angka absolutnya: kalau dijalankan dari laptop
// terhadap Postgres yang jauh (Neon), hasilnya didominasi latensi
// jaringan, bukan oleh kode. Enam tulisan berarti enam perjalanan
// bolak-balik; pada 200 ms per perjalanan itu 1,2 detik per event tanpa
// ada yang salah pada server. Di produksi, aplikasi dan database berada
// di region yang sama (2-5 ms), jadi angka absolutnya akan jauh lebih
// tinggi. RASIO B terhadap A tetap sahih dari mana pun diukur.
// ============================================================

func TestMeasure_IngestThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("pengukuran, bukan verifikasi; dilewati di -short")
	}

	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	store := NewPostgresStore(pool)

	const (
		tenantID       = "retention-test-tenant"
		sequentialN    = 30
		parallelProj   = 8
		perProjectN    = 15
		retentionDays  = 365
		warmupAttempts = 2
	)

	req := func(i int) *IngestRequest {
		return &IngestRequest{
			EventType: "throughput.measure",
			ActorID:   "bench-actor",
			Payload:   json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
		}
	}

	// ---- Pemanasan: koneksi pertama selalu lebih lambat ----

	warmupProject := createRetentionTestProject(t, ctx, pool, retentionDays)
	for i := 0; i < warmupAttempts; i++ {
		if _, err := store.Append(ctx, warmupProject, tenantID, req(i), ""); err != nil {
			t.Fatalf("Append gagal saat pemanasan: %v\n"+
				"Kalau ini soal kuota, tenant %q perlu baris di tenant_quotas "+
				"dengan limit yang cukup sebelum pengukuran bisa jalan.",
				err, tenantID)
		}
	}

	// ---- A. Satu project, berurutan ----

	seqProject := createRetentionTestProject(t, ctx, pool, retentionDays)

	seqStart := time.Now()
	for i := 0; i < sequentialN; i++ {
		if _, err := store.Append(ctx, seqProject, tenantID, req(i), ""); err != nil {
			t.Fatalf("Append gagal pada iterasi %d: %v", i, err)
		}
	}
	seqElapsed := time.Since(seqStart)
	seqRate := float64(sequentialN) / seqElapsed.Seconds()

	// ---- B. Banyak project, bersamaan ----

	projects := make([]string, parallelProj)
	for i := range projects {
		projects[i] = createRetentionTestProject(t, ctx, pool, retentionDays)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, parallelProj)

	parStart := time.Now()
	for _, projectID := range projects {
		wg.Add(1)
		go func(pid string) {
			defer wg.Done()
			for i := 0; i < perProjectN; i++ {
				if _, err := store.Append(ctx, pid, tenantID, req(i), ""); err != nil {
					errCh <- fmt.Errorf("project %s iterasi %d: %w", pid, i, err)
					return
				}
			}
		}(projectID)
	}
	wg.Wait()
	parElapsed := time.Since(parStart)
	close(errCh)

	for err := range errCh {
		t.Fatalf("Append gagal saat pengukuran paralel: %v", err)
	}

	parTotal := parallelProj * perProjectN
	parRate := float64(parTotal) / parElapsed.Seconds()
	perRoundTrip := seqElapsed / time.Duration(sequentialN) / 6

	t.Logf("\n"+
		"=====================================================\n"+
		"  A. SATU project, berurutan\n"+
		"     %d event dalam %v\n"+
		"     %.1f event/detik  (%v per event)\n"+
		"\n"+
		"  B. %d project bersamaan\n"+
		"     %d event dalam %v\n"+
		"     %.1f event/detik\n"+
		"\n"+
		"  Rasio B/A : %.1fx\n"+
		"  Perkiraan satu perjalanan ke database: %v\n"+
		"=====================================================\n"+
		"\n"+
		"Rasio mendekati %d berarti project berjalan benar-benar paralel:\n"+
		"pelanggan yang berbeda tidak saling menghalangi, dan batas\n"+
		"per-project hanya jadi masalah bagi satu pengirim yang sangat\n"+
		"deras ke satu project.\n"+
		"\n"+
		"Rasio mendekati 1 berarti ada penyempitan GLOBAL - ukuran pool\n"+
		"koneksi, atau kunci yang ternyata tidak se-per-project yang\n"+
		"diduga. Itu jauh lebih serius daripada enam tulisan per ingest.\n"+
		"\n"+
		"Angka absolutnya didominasi latensi jaringan kalau dijalankan\n"+
		"dari laptop terhadap database yang jauh. Di produksi, aplikasi\n"+
		"dan database satu region, jadi bacalah rasionya - bukan angkanya.\n",
		sequentialN, seqElapsed.Round(time.Millisecond), seqRate,
		(seqElapsed / time.Duration(sequentialN)).Round(time.Millisecond),
		parallelProj, parTotal, parElapsed.Round(time.Millisecond), parRate,
		parRate/seqRate,
		perRoundTrip.Round(time.Millisecond),
		parallelProj,
	)
}
