// cmd/perf-verify-test/main.go
//
// Alat pengukuran performa untuk ROADMAP §2D — salah satu dari dua
// item terbuka di track dashboard backend (satunya lagi §3D, alat
// terpisah di cmd/perf-history-test). TIDAK dipanggil server, TIDAK
// didaftarkan sebagai route. Jalankan manual, baca hasilnya, tempel
// ke ROADMAP.md.
//
// WAJIB pakai TRAILSEAL_TEST_POSTGRES_URL (env var yang sama dipakai
// semua *_test.go di repo ini, mis. events/retention_test.go).
// JANGAN PERNAH isi TRAILSEAL_POSTGRES_URL di sini — itu connection
// string PRODUKSI (lihat cmd/purge-verify/main.go). Program ini
// menolak jalan kalau TRAILSEAL_TEST_POSTGRES_URL kosong.
//
// Yang diukur (sesuai §2D, "ukur VerifyRange"), plus satu tambahan
// yang direkomendasikan supaya angkanya representatif untuk endpoint
// sungguhan:
//   1. VerifyRange mentah — baseline murni, tanpa overhead orkestrasi.
//   2. VerifyTenant — jalur yang BENAR-BENAR dipakai
//      GET/POST /api/tenants/:id/audit/verify (semaphore 2 + dedupe,
//      lihat events/verify_tenant.go §2B poin 4).
//
// VerifyRange/VerifyChain/ComputeHash dipakai APA ADANYA — tidak
// diubah sedikit pun, sesuai §3 zona "jangan disentuh".
//
// Cara pakai:
//   $env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//   go run ./cmd/perf-verify-test -events=1000000
//
// SARAN: jalankan dulu dengan -events=100 sebagai smoke test (skema
// jsonb payload belum diverifikasi terhadap CopyFrom di lingkungan
// ini — kalau error tipe payload muncul, ganti baris `payload` di
// chainRows.Values() jadi `string(payload)`).
//
// Untuk mengukur ulang project yang sudah di-seed tanpa seed ulang:
//   go run ./cmd/perf-verify-test -skip-seed -project-id=<id>
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"trailseal/events"
	"trailseal/project"
)

func main() {
	eventCount := flag.Int("events", 1_000_000, "jumlah event yang di-seed (default: kuota plan Business, lihat BUSINESS_MODEL.md §3)")
	skipSeed := flag.Bool("skip-seed", false, "lewati seeding, langsung ukur project yang sudah ada")
	projectIDFlag := flag.String("project-id", "", "project ID yang sudah ada (wajib bila -skip-seed)")
	flag.Parse()

	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		log.Fatal("FATAL: TRAILSEAL_TEST_POSTGRES_URL kosong.\n" +
			"JANGAN isi TRAILSEAL_POSTGRES_URL — itu database PRODUKSI.\n" +
			"Set env var test yang sama dipakai *_test.go di repo ini, lalu ulangi.")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("FATAL: gagal connect: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("FATAL: gagal ping: %v", err)
	}

	eventStore := events.NewPostgresStore(pool)
	projectStore := project.NewPostgresStore(pool)

	var projectID string

	if *skipSeed {
		if *projectIDFlag == "" {
			log.Fatal("FATAL: -skip-seed butuh -project-id")
		}
		projectID = *projectIDFlag
		fmt.Printf("Melewati seeding, memakai project %s\n", projectID)
	} else {
		tenantID := fmt.Sprintf("perf-test-tenant-%d", time.Now().Unix())
		projectID, err = createProject(ctx, pool, tenantID, int64(*eventCount))
		if err != nil {
			log.Fatalf("FATAL: gagal buat project uji: %v", err)
		}
		fmt.Printf("Project uji dibuat: %s (tenant %s)\n", projectID, tenantID)

		fmt.Printf("Seeding %d event dengan hash chain valid...\n", *eventCount)
		seedStart := time.Now()
		if err := seedChain(ctx, pool, projectID, *eventCount); err != nil {
			log.Fatalf("FATAL: seeding gagal: %v", err)
		}
		fmt.Printf("Seeding selesai dalam %s\n", time.Since(seedStart))
	}

	tenantID, err := lookupTenantID(ctx, pool, projectID)
	if err != nil {
		log.Fatalf("FATAL: gagal cari tenant_id project %s: %v", projectID, err)
	}

	// --- 1. VerifyRange mentah (baseline) ---
	runtime.GC()
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	rawStart := time.Now()
	valid, brokenAt, checked, err := eventStore.VerifyRange(ctx, projectID, 1)
	rawElapsed := time.Since(rawStart)

	runtime.ReadMemStats(&memAfter)

	if err != nil {
		log.Fatalf("FATAL: VerifyRange error: %v", err)
	}
	fmt.Println("\n=== 1. VerifyRange mentah (baseline, tanpa orkestrasi) ===")
	fmt.Printf("  valid=%v brokenAt=%q checked=%d\n", valid, brokenAt, checked)
	fmt.Printf("  waktu: %s\n", rawElapsed)
	fmt.Printf("  memori (heap alloc, delta bertanda): %s\n", memDeltaMB(memBefore, memAfter))

	// --- 2. VerifyTenant (jalur endpoint asli, §2B) ---
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	tenantStart := time.Now()
	result, err := events.VerifyTenant(ctx, eventStore, projectStore, tenantID)
	tenantElapsed := time.Since(tenantStart)

	runtime.ReadMemStats(&memAfter)

	if err != nil {
		log.Fatalf("FATAL: VerifyTenant error: %v", err)
	}
	fmt.Println("\n=== 2. VerifyTenant (jalur GET/POST /api/tenants/:id/audit/verify) ===")
	fmt.Printf("  status=%s events_checked=%d\n", result.Status, result.EventsChecked)
	fmt.Printf("  waktu: %s\n", tenantElapsed)
	fmt.Printf("  memori (heap alloc, delta bertanda): %s\n", memDeltaMB(memBefore, memAfter))

	fmt.Println("\n=== Kriteria §2D ===")
	fmt.Println("  ≤ ±2 detik dan memori wajar → selesai, JANGAN dioptimasi.")
	fmt.Println("  Lebih lambat / memori tinggi → tambah METHOD BARU verifikasi")
	fmt.Println("  inkremental (jangan ubah VerifyRange), checkpoint di tabel cache §2A.")
	fmt.Printf("\nProject ID (buat -skip-seed run ulang): %s\n", projectID)
	fmt.Printf("Tenant ID: %s\n", tenantID)
}

// memDeltaMB — selisih heap bertanda dalam MB. HeapAlloc adalah
// "bytes hidup sekarang", bukan kumulatif, jadi bisa turun antara dua
// pembacaan (GC jalan di tengah). Pengurangan uint64 polos akan
// underflow jadi angka raksasa kalau itu terjadi.
func memDeltaMB(before, after runtime.MemStats) string {
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	return fmt.Sprintf("%+.2f MB", float64(delta)/1024/1024)
}

func createProject(ctx context.Context, pool *pgxpool.Pool, tenantID string, monthlyLimit int64) (string, error) {
	var projectID string
	err := pool.QueryRow(ctx, `
		INSERT INTO projects (
			tenant_id, name, status, retention_days, monthly_event_limit
		)
		VALUES ($1, $2, 'active', $3, $4)
		RETURNING id
	`,
		tenantID,
		"perf-test-"+time.Now().Format("20060102150405.000000000"),
		365, // retensi plan Business, BUSINESS_MODEL.md §3
		monthlyLimit,
	).Scan(&projectID)
	return projectID, err
}

func lookupTenantID(ctx context.Context, pool *pgxpool.Pool, projectID string) (string, error) {
	var tenantID string
	err := pool.QueryRow(ctx, `SELECT tenant_id FROM projects WHERE id = $1`, projectID).Scan(&tenantID)
	return tenantID, err
}

// chainRows — pgx.CopyFromSource yang generate baris hash chain SAAT
// DIMINTA (streaming), bukan buffer 1 juta event ke memori dulu.
// events.ComputeHash dipakai APA ADANYA (§3 "jangan disentuh"), jadi
// chain yang di-seed pasti valid saat dibaca ulang oleh VerifyChain.
type chainRows struct {
	projectID string
	tenantID  string
	total     int
	i         int
	prevHash  string
	runPrefix string // unik per proses, cegah tabrakan ID lintas run
}

func (r *chainRows) Next() bool { return r.i < r.total }

func (r *chainRows) Values() ([]interface{}, error) {
	r.i++
	seq := int64(r.i)
	occurredAt := time.Now().UTC().Truncate(time.Microsecond) // presisi Postgres timestamptz — HARUS dipotong SEBELUM hash, bukan sesudah, atau hash tidak akan cocok saat dibaca ulang
	eventType := "perf.test.event"
	actorID := "perf-test-seeder"
	payload := json.RawMessage(fmt.Sprintf(`{"i":%d}`, r.i))
	id := fmt.Sprintf("%s-0000-4000-8000-%012x", r.runPrefix, r.i) // UUID valid, prefix unik per run + index di segmen terakhir

	hash := events.ComputeHash(r.projectID, seq, occurredAt, eventType, actorID, payload, r.prevHash)

	var prevHashArg interface{}
	if r.prevHash == "" {
		prevHashArg = nil // event pertama: prev_hash NULL, konsisten dengan GetByID (*string)
	} else {
		prevHashArg = r.prevHash
	}

	row := []interface{}{
		id, r.projectID, r.tenantID, seq, eventType, actorID,
		payload, // json.RawMessage — biarkan tipe aslinya, JANGAN cast ke []byte
		// (pgx menangani encoding jsonb lewat json.Marshaler; []byte polos
		// bisa ke-encode sebagai bytea dan gagal). Kalau tetap error tipe,
		// ganti jadi string(payload).
		prevHashArg, hash, occurredAt, occurredAt,
	}
	r.prevHash = hash
	return row, nil
}

func (r *chainRows) Err() error { return nil }

func seedChain(ctx context.Context, pool *pgxpool.Pool, projectID string, total int) error {
	tenantID, err := lookupTenantID(ctx, pool, projectID)
	if err != nil {
		return err
	}

	src := &chainRows{
		projectID: projectID,
		tenantID:  tenantID,
		total:     total,
		runPrefix: fmt.Sprintf("%08x", uint32(time.Now().UnixNano())),
	}

	batchSize := 50_000
	inserted := 0
	for inserted < total {
		n := batchSize
		if remaining := total - inserted; remaining < n {
			n = remaining
		}
		batch := &limitedSource{inner: src, remaining: n}
		copied, err := pool.CopyFrom(ctx,
			pgx.Identifier{"events"},
			[]string{"id", "project_id", "tenant_id", "sequence", "event_type",
				"actor_id", "payload", "prev_hash", "hash", "occurred_at", "created_at"},
			batch,
		)
		if err != nil {
			return fmt.Errorf("copy batch at %d: %w", inserted, err)
		}
		inserted += int(copied)
		if inserted%200_000 == 0 || inserted == total {
			fmt.Printf("  ...%d/%d\n", inserted, total)
		}
	}
	return nil
}

// limitedSource membungkus chainRows supaya CopyFrom dipanggil per
// batch (bukan satu transaksi raksasa untuk 1 juta baris sekaligus).
type limitedSource struct {
	inner     *chainRows
	remaining int
}

func (l *limitedSource) Next() bool {
	if l.remaining <= 0 {
		return false
	}
	l.remaining--
	return l.inner.Next()
}
func (l *limitedSource) Values() ([]interface{}, error) { return l.inner.Values() }
func (l *limitedSource) Err() error                     { return l.inner.Err() }
