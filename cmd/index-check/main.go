// cmd/index-check: membandingkan definisi indeks pada tabel events.
//
// Dipakai sekali sebelum migrasi 000011 (drop indeks kembar). Membaca
// pg_indexes — keadaan database yang SEBENARNYA — bukan menyimpulkan dari
// berkas migrasi, yang hanya menyatakan maksud.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		dsn = os.Getenv("TRAILSEAL_POSTGRES_URL")
	}
	if dsn == "" {
		fmt.Println("BERHENTI: TRAILSEAL_TEST_POSTGRES_URL / TRAILSEAL_POSTGRES_URL kosong")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Println("gagal konek:", err)
		os.Exit(1)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `
		SELECT indexname, indexdef
		FROM pg_indexes
		WHERE tablename = 'events'
		ORDER BY indexname
	`)
	if err != nil {
		fmt.Println("gagal query:", err)
		os.Exit(1)
	}
	defer rows.Close()

	defs := map[string]string{}
	fmt.Println("Indeks pada tabel events:")
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			fmt.Println("gagal scan:", err)
			os.Exit(1)
		}
		defs[name] = def
		fmt.Printf("  %-32s %s\n", name, def)
	}
	if err := rows.Err(); err != nil {
		fmt.Println("gagal iterasi:", err)
		os.Exit(1)
	}

	a, okA := defs["idx_events_project_time"]
	b, okB := defs["idx_events_retention"]

	fmt.Println()
	switch {
	case !okA && !okB:
		fmt.Println("HASIL: keduanya TIDAK ADA. Migrasi 000011 jadi no-op. Aman, tapi periksa kenapa.")
	case okA && !okB:
		fmt.Println("HASIL: idx_events_retention sudah tidak ada. Migrasi 000011 sudah jalan atau tidak perlu.")
	case !okA && okB:
		fmt.Println("HASIL: JANGAN JALANKAN 000011.")
		fmt.Println("  idx_events_project_time TIDAK ADA; idx_events_retention satu-satunya")
		fmt.Println("  indeks pada (project_id, occurred_at). Menjatuhkannya membuat retention")
		fmt.Println("  dan filter since/until jadi seq-scan.")
	default:
		// Bandingkan tanpa nama indeks, karena nama pasti berbeda.
		normA := a[len("CREATE INDEX idx_events_project_time"):]
		normB := b[len("CREATE INDEX idx_events_retention"):]
		if normA == normB {
			fmt.Println("HASIL: KEMBAR PERSIS. Aman menjalankan migrasi 000011.")
		} else {
			fmt.Println("HASIL: TIDAK kembar. JANGAN jalankan 000011 sebelum diperiksa.")
			fmt.Printf("  project_time : %s\n", normA)
			fmt.Printf("  retention    : %s\n", normB)
		}
	}
}
