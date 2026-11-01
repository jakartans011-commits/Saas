// cmd/testdb-reset: mengosongkan database TEST.
//
// Pengaman ada di DALAM program, bukan di skrip pemanggil. Skrip PowerShell
// yang memakai `return` di baris teratas TIDAK menghentikan baris berikutnya —
// kesalahan yang hampir membuat perintah TRUNCATE jalan di host yang salah.
// Di sini os.Exit benar-benar berhenti.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Hanya host yang MENGANDUNG salah satu ini yang boleh dikosongkan.
var hostTestDiizinkan = []string{"ep-red-dream-azemavuv"}

// Host produksi — ditolak mutlak, bahkan kalau ikut terdaftar di atas.
var hostProduksi = []string{"ep-lucky-fog"}

func main() {
	yes := flag.Bool("yes", false, "benar-benar kosongkan (tanpa ini hanya melapor)")
	flag.Parse()

	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		fmt.Println("BERHENTI: TRAILSEAL_TEST_POSTGRES_URL kosong")
		os.Exit(1)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		fmt.Println("BERHENTI: DSN tidak terbaca:", err)
		os.Exit(1)
	}
	host := cfg.ConnConfig.Host
	fmt.Println("Host:", host)

	for _, p := range hostProduksi {
		if strings.Contains(host, p) {
			fmt.Println("BERHENTI: ini host PRODUKSI. Tidak ada yang dihapus.")
			os.Exit(1)
		}
	}
	cocok := false
	for _, t := range hostTestDiizinkan {
		if strings.Contains(host, t) {
			cocok = true
		}
	}
	if !cocok {
		fmt.Println("BERHENTI: host bukan branch testing yang dikenal. Tidak ada yang dihapus.")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Println("gagal konek:", err)
		os.Exit(1)
	}
	defer pool.Close()

	var projek, event int64
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM projects`).Scan(&projek)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&event)
	fmt.Printf("SEBELUM: projects=%d events=%d\n", projek, event)

	if !*yes {
		fmt.Println("\nMode lapor saja. Jalankan ulang dengan -yes untuk mengosongkan.")
		return
	}

	_, err = pool.Exec(ctx, `
		TRUNCATE events, idempotency_keys, project_chain_state,
		         event_retention_checkpoints, audit_verify_checkpoints,
		         project_usage_counters, tenant_usage_counters, tenant_quotas,
		         projects, api_keys, audit_events, stripe_webhook_events,
		         tenant_audit_verify_cache
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		fmt.Println("TRUNCATE gagal:", err)
		os.Exit(1)
	}

	_ = pool.QueryRow(ctx, `SELECT count(*) FROM projects`).Scan(&projek)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&event)
	fmt.Printf("SESUDAH: projects=%d events=%d\n", projek, event)
}
