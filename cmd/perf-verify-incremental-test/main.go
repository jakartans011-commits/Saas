// cmd/perf-verify-incremental-test/main.go
//
// Uji cepat VerifyTenantIncremental TANPA lewat HTTP/auth — panggil
// langsung fungsi Go-nya, pakai TRAILSEAL_TEST_POSTGRES_URL yang sama.
// Dipakai karena test lewat endpoint HTTP asli butuh token milik user
// yang punya akses ke tenant perf-test (bukan token biasa), dan token
// AI sedang terbatas untuk bolak-balik debug auth.
//
// TIDAK dipanggil server, TIDAK didaftarkan sebagai route. Jalankan
// manual, seperti cmd/perf-verify-test yang sudah ada.
//
// Cara pakai (2 kali, project sudah pernah di-seed):
//   $env:TRAILSEAL_TEST_POSTGRES_URL="postgres://..."
//   go run ./cmd/perf-verify-incremental-test -tenant-id=perf-test-tenant-1790364532
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"trailseal/events"
	"trailseal/project"
)

func memDeltaMB(before, after runtime.MemStats) string {
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	return fmt.Sprintf("%+.2f MB", float64(delta)/1024/1024)
}

func main() {
	tenantID := flag.String("tenant-id", "", "tenant ID yang sudah di-seed (wajib)")
	flag.Parse()

	if *tenantID == "" {
		log.Fatal("FATAL: -tenant-id wajib diisi")
	}

	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		log.Fatal("FATAL: TRAILSEAL_TEST_POSTGRES_URL kosong.\n" +
			"JANGAN isi TRAILSEAL_POSTGRES_URL — itu database PRODUKSI.")
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

	runtime.GC()
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	start := time.Now()
	result, err := events.VerifyTenantIncremental(ctx, eventStore, projectStore, *tenantID)
	elapsed := time.Since(start)

	runtime.ReadMemStats(&memAfter)

	if err != nil {
		log.Fatalf("FATAL: VerifyTenantIncremental error: %v", err)
	}

	fmt.Println("\n=== VerifyTenantIncremental ===")
	fmt.Printf("  status=%s events_checked=%d\n", result.Status, result.EventsChecked)
	fmt.Printf("  waktu: %s\n", elapsed)
	fmt.Printf("  memori (heap alloc, delta bertanda): %s\n", memDeltaMB(memBefore, memAfter))
	if result.FirstBroken != nil {
		fmt.Printf("  first_broken.event_id: %s\n", result.FirstBroken.EventID)
	}

	fmt.Println("\nJalankan lagi dengan tenant-id yang sama TANPA event baru masuk:")
	fmt.Println("  expect: events_checked=0, waktu jauh lebih cepat, status tetap sama.")
}
