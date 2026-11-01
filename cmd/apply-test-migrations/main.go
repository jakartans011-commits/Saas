// cmd/apply-test-migrations/main.go
//
// Sekali pakai: jalankan db.RunMigrations (golang-migrate, sudah ada
// di db/migrate.go) terhadap TRAILSEAL_TEST_POSTGRES_URL. Dipakai
// supaya migrasi baru (mis. 000010_audit_verify_checkpoints) tercatat
// benar di schema_migrations, BUKAN dijalankan manual lewat psql yang
// tidak update version tracking golang-migrate.
//
// JANGAN PERNAH arahkan ini ke TRAILSEAL_POSTGRES_URL (produksi) tanpa
// sengaja — program menolak jalan kalau TRAILSEAL_TEST_POSTGRES_URL
// kosong.
//
// Cara pakai:
//
//	go run ./cmd/apply-test-migrations
package main

import (
	"log"
	"os"

	"trailseal/db"
)

func main() {
	dsn := os.Getenv("TRAILSEAL_TEST_POSTGRES_URL")
	if dsn == "" {
		log.Fatal("FATAL: TRAILSEAL_TEST_POSTGRES_URL kosong.\n" +
			"JANGAN isi TRAILSEAL_POSTGRES_URL — itu database PRODUKSI.")
	}

	if err := db.RunMigrations(dsn); err != nil {
		log.Fatalf("FATAL: migrasi gagal: %v", err)
	}
	log.Println("OK: migrasi diterapkan (termasuk 000010 kalau belum pernah jalan).")
}
