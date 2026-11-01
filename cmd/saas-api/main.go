// cmd/saas-api/main.go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gofiber/template/html/v2"
	bolt "go.etcd.io/bbolt"

	"trailseal/db"
)

func main() {
	// ============================================================
	// 1. SIGNING KEY — WAJIB dari environment, JANGAN hardcode.
	// ============================================================
	signingKey := os.Getenv("SAAS_JWT_SIGNING_KEY")
	if signingKey == "" {
		log.Fatal(
			"SAAS_JWT_SIGNING_KEY environment variable is required. " +
				"Set it before starting, e.g.:\n" +
				"  $env:SAAS_JWT_SIGNING_KEY = \"<random-secret-min-32-chars>\"",
		)
	}

	// ============================================================
	// 1b. ROOT API KEY — WAJIB dari environment, JANGAN hardcode.
	// ============================================================
	rootAPIKey := os.Getenv("SAAS_ROOT_API_KEY")
	if rootAPIKey == "" {
		log.Fatal(
			"SAAS_ROOT_API_KEY environment variable is required to start " +
				"root-protected platform administration routes. Set it before " +
				"starting, e.g.:\n" +
				"  $env:SAAS_ROOT_API_KEY = \"<random-secret-min-32-chars>\"",
		)
	}

	// ============================================================
	// 1c. POSTGRES CONNECTION STRING — WAJIB dari environment.
	// ============================================================
	postgresURL := os.Getenv("TRAILSEAL_POSTGRES_URL")
	if postgresURL == "" {
		log.Fatal(
			"TRAILSEAL_POSTGRES_URL environment variable is required. " +
				"Set it before starting, e.g.:\n" +
				"  $env:TRAILSEAL_POSTGRES_URL = \"postgres://trailseal:trailseal_dev_password@localhost:5432/trailseal?sslmode=disable\"",
		)
	}

	// ============================================================
	// 2. DATABASE (bbolt)
	// ============================================================
	// WAJIB absolut di production. "data" relatif terhadap working directory:
	// service yang restart dengan CWD berbeda akan membuat database KOSONG,
	// dan seluruh tenant, user, serta refresh token tampak hilang meskipun
	// berkas lama masih utuh di tempat lain.
	dataDir := os.Getenv("TRAILSEAL_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("failed to create data dir %s: %v", dataDir, err)
	}
	dbPath := filepath.Join(dataDir, "saas-api.db")

	// Timeout WAJIB. Tanpa ini, instance kedua yang tidak sengaja berjalan
	// menggantung SELAMANYA menunggu kunci berkas — proses hidup, port tidak
	// pernah dibuka, tidak ada log maupun error.
	boltDB, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		log.Fatalf(
			"failed to open saas-api database at %s: %v "+
				"(apakah ada instance lain yang masih memegang berkas ini?)",
			dbPath, err,
		)
	}
	log.Printf("Bolt data dir: %s", dataDir)
	defer boltDB.Close()

	// ============================================================
	// 2b. POSTGRES POOL
	// ============================================================
	ctx := context.Background()

	pgPool, err := db.Connect(ctx, postgresURL)
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pgPool.Close()

	if err := db.RunMigrations(postgresURL); err != nil {
		log.Fatalf("failed to run postgres migrations: %v", err)
	}

	// ============================================================
	// 2c. TEMPLATE ENGINE
	// ============================================================
	// Path template relatif terhadap working directory, bukan terhadap
	// binary, dan menunjuk ke struktur repo. Binary yang dideploy tanpa
	// folder itu membuat setiap halaman portal balas 500.
	templateDir := os.Getenv("TRAILSEAL_TEMPLATE_DIR")
	if templateDir == "" {
		templateDir = "./cmd/saas-api/templates"
	}
	engine := html.New(templateDir, ".html")

	// Reload membaca ulang berkas dari disk pada SETIAP render. Berguna di
	// dev, pemborosan di production.
	engine.Reload(os.Getenv("APP_ENV") != "production")

	// ============================================================
	// 3. BUILD APP
	// ============================================================
	app, billingService, err := BuildApp(boltDB, pgPool, signingKey, rootAPIKey, engine)
	if err != nil {
		log.Fatalf("failed to build app: %v", err)
	}

	// ============================================================
	// 3b. GRACE PERIOD SWEEPER — SATU-SATUNYA penegakan
	// invoice.payment_failed.
	//
	// middleware.RequirePaymentCurrent ada di grace_period_guard.go tapi
	// TIDAK dipasang di route mana pun (0 pemakaian di app.go). Jadi tidak
	// ada pemeriksaan real-time per-request: tenant yang gagal bayar dan
	// grace period-nya lewat tetap punya akses penuh sampai sweep berikutnya
	// jalan — jendela paling lama 24 jam. Itu keputusan sadar, bukan
	// kelalaian: memasangnya menambah satu pembacaan tenant store di setiap
	// request jalur panas. Pasang kalau jendela sehari itu pernah
	// disalahgunakan pelanggan nyata.
	// Jalan sekali per 24 jam, downgrade tenant yang grace period-nya
	// sudah lewat tapi belum sempat kena block middleware (mis. tenant
	// yang tidak ada trafik sama sekali selama grace period).
	// TIDAK DIPAKAI (per 2026-11-01): nol pemakaian di app.go. Lihat catatan di
	// main.go bagian 3b. Dipertahankan karena siap pasang, bukan karena terlupa.
	// ============================================================
	sweepStop := make(chan struct{})
	runGraceSweep := func() {
		n, err := billingService.SweepExpiredGracePeriods(context.Background())
		if err != nil {
			log.Printf("grace period sweep error: %v", err)
		}
		if n > 0 {
			log.Printf("grace period sweep: %d tenants downgraded", n)
		}
	}
	go func() {
		// Sekali saat start. Ticker 24 jam baru berdetak SETELAH 24 jam,
		// jadi proses yang di-restart lebih sering dari itu — deploy, crash,
		// reboot — tidak pernah menjalankan sweep sama sekali, dan tenant
		// yang gagal bayar tidak pernah turun ke Free.
		runGraceSweep()

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				runGraceSweep()
			case <-sweepStop:
				return
			}
		}
	}()

	// Rekonsiliasi Stripe harian — menangkap webhook yang tidak pernah
	// sampai. Dry-run kecuali STRIPE_RECONCILE_APPLY=true.
	stopReconcile, err := startStripeReconcileScheduler(billingService)
	if err != nil {
		log.Printf("stripe reconcile scheduler disabled: %v", err)
	} else {
		defer stopReconcile()
	}

	// Alert disk penuh. Bukan rotasi log: saas-api menulis ke stderr dan
	// platform yang menangkapnya. Sumber disk penuh yang nyata adalah
	// bbolt yang tumbuh dan tidak pernah mengecil, plus berkas backup.
	stopDisk, err := startDiskMonitor()
	if err != nil {
		log.Printf("disk monitor disabled: %v", err)
	} else {
		defer stopDisk()
	}

	// P5-1: retensi events + purge tenant kedaluwarsa (harian)
	stopRetention, err := startRetentionScheduler(boltDB, pgPool)
	if err != nil {
		log.Printf("retention scheduler disabled: %v", err)
	} else {
		defer stopRetention()
	}

	// P0-1: kirim antrean email notifikasi dari dalam proses ini (bbolt satu pemegang)
	if stopNotify, err := startNotifyScheduler(boltDB); err != nil {
		log.Printf("notify scheduler disabled: %v", err)
	} else {
		defer stopNotify()
	}

	// ============================================================
	// 4. START — dengan Graceful Shutdown
	// ============================================================
	port := os.Getenv("SAAS_API_PORT")
	if port == "" {
		port = "8090"
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("TrailSeal API running on http://localhost:%s", port)
		log.Printf("Bolt data stored in: %s", dbPath)
		log.Printf("Postgres: connected")
		if err := app.Listen(":" + port); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	<-quit
	log.Println("🛑 Shutting down gracefully...")
	close(sweepStop) // hentikan goroutine sweeper juga, bukan cuma server

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = shutdownCtx // dipakai kalau Shutdown butuh context; app.Shutdown() Fiber saat ini tidak menerima ctx

	if err := app.Shutdown(); err != nil {
		log.Fatalf("❌ Server shutdown failed: %v", err)
	}

	log.Println("✅ Server stopped successfully")
}
