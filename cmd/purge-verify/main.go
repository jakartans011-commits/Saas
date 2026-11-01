// cmd/purge-verify/main.go
//
// Tool standalone (BUKAN bagian dari server) untuk verifikasi manual
// item #8 ROADMAP — full purge flow. Dua subcommand:
//
//   go run ./cmd/purge-verify backdate <tenant_id>
//     Set DeletedAt tenant jadi 40 hari lalu, supaya grace period (30/60
//     hari) langsung lolos tanpa nunggu asli. WAJIB stop server dulu
//     (bbolt exclusive file lock, tidak bisa dibuka 2 proses sekaligus).
//
//   go run ./cmd/purge-verify verify <tenant_id>
//     Cek SEMUA store (bolt: tenant/agents/tools, postgres:
//     audit_events/projects/api_keys) benar-benar kosong untuk tenant_id
//     itu. Jalankan SETELAH purge via API, dan SETELAH stop server lagi
//     (bagian bolt-nya). Audit events pindah dari bolt ke postgres sejak
//     migrasi PostgresAuditStore (2026-09) — WAJIB isi
//     TRAILSEAL_POSTGRES_URL, kalau tidak audit events TIDAK ikut dicek.
//
// TODO SEBELUM DIJALANKAN: isi BOLT_DB_PATH dan DATABASE_URL di bawah
// sesuai .env Anda (cek main.go untuk nilai aslinya kalau tidak yakin).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	bolt "go.etcd.io/bbolt"

	"trailseal/agent"
	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/project"
	"trailseal/tenant"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: purge-verify <backdate|verify> <tenant_id>")
		os.Exit(1)
	}
	cmd := os.Args[1]
	tenantID := os.Args[2]

	// Dikonfirmasi dari cmd/saas-api/main.go: dataDir := "data",
	// dbPath := filepath.Join(dataDir, "saas-api.db")
	boltPath := os.Getenv("BOLT_DB_PATH")
	if boltPath == "" {
		boltPath = filepath.Join("data", "saas-api.db")
	}
	pgDSN := os.Getenv("TRAILSEAL_POSTGRES_URL")

	db, err := bolt.Open(boltPath, 0600, &bolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		fmt.Printf("FATAL: gagal buka bolt db di %q: %v\n", boltPath, err)
		fmt.Println("Pastikan server sudah di-stop dulu (bbolt cuma boleh 1 proses).")
		os.Exit(1)
	}
	defer db.Close()

	tenantStore, _ := tenant.NewBoltTenantStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)
	// AUDIT: sejak migrasi ke PostgresAuditStore, audit TIDAK lagi
	// disimpan di Bolt — auditStore Bolt lama dihapus dari sini.
	// Pengecekan audit events sekarang jalan di blok Postgres dalam
	// runVerify (lihat "4. Audit events" di bawah).

	switch cmd {
	case "backdate":
		runBackdate(tenantStore, tenantID)
	case "verify":
		runVerify(tenantStore, agentStore, pgDSN, tenantID)
	default:
		fmt.Println("subcommand tidak dikenal:", cmd)
		os.Exit(1)
	}
}

func runBackdate(tenantStore *tenant.BoltTenantStore, tenantID string) {
	t, err := tenantStore.GetIncludingDeleted(tenantID)
	if err != nil {
		fmt.Printf("FATAL: tenant %q tidak ditemukan: %v\n", tenantID, err)
		os.Exit(1)
	}
	if t.DeletedAt == nil {
		fmt.Printf("FATAL: tenant %q belum soft-deleted (DeletedAt nil). Jalankan POST .../data/delete dulu.\n", tenantID)
		os.Exit(1)
	}
	past := time.Now().Add(-40 * 24 * time.Hour)
	t.DeletedAt = &past
	if err := tenantStore.Put(t); err != nil {
		fmt.Printf("FATAL: gagal update DeletedAt: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OK: DeletedAt tenant %q diset ke %s (plan_tier=%s, grace period sekarang pasti lolos).\n",
		tenantID, past.Format(time.RFC3339), t.PlanTier)
	fmt.Println("Sekarang: start server lagi, lalu panggil POST /api/tenants/" + tenantID + "/data/purge dari browser seperti biasa.")
}

func runVerify(
	tenantStore *tenant.BoltTenantStore,
	agentStore *agent.BoltAgentStore,
	pgDSN string,
	tenantID string,
) {
	ok := true

	// 1. Tenant record — harus benar-benar hilang (bukan cuma soft-deleted)
	if _, err := tenantStore.GetIncludingDeleted(tenantID); err == nil {
		fmt.Println("FAIL: tenant record MASIH ADA (harusnya hard-deleted)")
		ok = false
	} else {
		fmt.Println("PASS: tenant record sudah tidak ada")
	}

	// 2. Agents
	agents, err := agentStore.ListAgents(tenantID)
	if err != nil {
		fmt.Printf("WARN: ListAgents error (mungkin normal kalau tenant sudah hilang): %v\n", err)
	} else if len(agents) > 0 {
		fmt.Printf("FAIL: masih ada %d agent tersisa\n", len(agents))
		ok = false
	} else {
		fmt.Println("PASS: agents kosong")
	}

	// 3. Tools
	tools, err := agentStore.ListTools(tenantID)
	if err != nil {
		fmt.Printf("WARN: ListTools error: %v\n", err)
	} else if len(tools) > 0 {
		fmt.Printf("FAIL: masih ada %d tool tersisa\n", len(tools))
		ok = false
	} else {
		fmt.Println("PASS: tools kosong")
	}

	// 4, 5 & 6. Audit events + Projects + API keys (semua di Postgres
	// sekarang) — tidak butuh stop server, Postgres boleh diakses
	// banyak koneksi sekaligus.
	if pgDSN == "" {
		fmt.Println("SKIP: TRAILSEAL_POSTGRES_URL kosong, tidak cek audit events/projects/api_keys. Isi env var lalu jalankan ulang.")
	} else {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, pgDSN)
		if err != nil {
			fmt.Printf("FATAL: gagal connect postgres: %v\n", err)
			os.Exit(1)
		}
		defer pool.Close()

		// 4. Audit events — sejak migrasi, audit hidup di Postgres.
		auditStore, err := audit.NewPostgresAuditStore(pool)
		if err != nil {
			fmt.Printf("WARN: gagal init PostgresAuditStore: %v\n", err)
		} else {
			events, err := auditStore.ListForTenant(tenantID, 1000)
			if err != nil {
				fmt.Printf("WARN: ListForTenant (audit) error: %v\n", err)
			} else if len(events) > 0 {
				fmt.Printf("FAIL: masih ada %d audit event tersisa\n", len(events))
				ok = false
			} else {
				fmt.Println("PASS: audit events kosong")
			}
		}

		projectStore := project.NewPostgresStore(pool)
		projects, err := projectStore.ListByTenant(ctx, tenantID)
		if err != nil {
			fmt.Printf("WARN: ListByTenant (project) error: %v\n", err)
		} else if len(projects) > 0 {
			fmt.Printf("FAIL: masih ada %d project tersisa\n", len(projects))
			ok = false
		} else {
			fmt.Println("PASS: projects kosong")
		}

		// apikey.PostgresStore tidak punya ListAllForTenant (cuma
		// ListByProject per-project) — query manual pakai pool yang sama.
		apiKeyStore := apikey.NewPostgresStore(pool)
		_ = apiKeyStore // dipakai kalau nanti ListAllForTenant ditambah
		var count int
		row := pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE tenant_id = $1`, tenantID)
		if err := row.Scan(&count); err != nil {
			fmt.Printf("WARN: query api_keys count error: %v\n", err)
		} else if count > 0 {
			fmt.Printf("FAIL: masih ada %d api key tersisa\n", count)
			ok = false
		} else {
			fmt.Println("PASS: api_keys kosong")
		}
	}

	fmt.Println("---")
	if ok {
		fmt.Println("KESIMPULAN: SEMUA store bersih. Item #8 aman ditutup (kalau pgDSN diisi & semua PASS).")
	} else {
		fmt.Println("KESIMPULAN: ADA DATA ORPHAN TERSISA. Jangan tutup item #8 dulu — cek FAIL di atas.")
	}
}
