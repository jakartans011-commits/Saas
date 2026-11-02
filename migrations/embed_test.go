package migrations

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// ============================================================
// BERKAS MIGRASI YANG TERTANAM
//
// Sejak migrasi dibaca dari embed.FS dan bukan dari disk, ada satu kelas
// kesalahan baru yang tidak akan ketahuan sampai deploy: berkas yang ada
// di repo tapi TIDAK ikut tertanam. `go build` tetap lulus, test yang
// memakai database tetap hijau di mesin yang kebetulan punya berkasnya,
// dan kegagalannya baru muncul di container yang bersih.
//
// Test ini membaca isi embed.FS itu sendiri — bukan direktori di disk —
// sehingga ia melihat persis apa yang akan ikut ke dalam binary.
// ============================================================

func TestEmbeddedMigrations_NotEmpty(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("gagal membaca embed.FS: %v", err)
	}

	var sqlCount int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			sqlCount++
		}
	}

	if sqlCount == 0 {
		t.Fatal(
			"tidak ada satu pun berkas .sql yang tertanam.\n" +
				"Binary akan menyala lalu gagal bermigrasi di container yang bersih, " +
				"dan `go build` tidak akan mengeluh sama sekali.",
		)
	}
}

func TestEmbeddedMigrations_EveryUpHasDown(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("gagal membaca embed.FS: %v", err)
	}

	present := map[string]bool{}
	for _, e := range entries {
		present[e.Name()] = true
	}

	for name := range present {
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}

		down := strings.TrimSuffix(name, ".up.sql") + ".down.sql"
		if !present[down] {
			t.Errorf(
				"%s tidak punya pasangan %s.\n"+
					"Tanpa berkas down, migrasi itu tidak bisa dibatalkan — dan "+
					"pembatalan baru dibutuhkan justru saat keadaan sedang buruk.",
				name, down,
			)
		}
	}
}

// TestEmbeddedMigrations_LoadableByMigrate memastikan golang-migrate benar-
// benar menerima isi embed.FS ini. Membaca direktorinya saja tidak cukup:
// penamaan yang tidak sesuai konvensi (nomor urut hilang, versi ganda)
// baru ditolak di titik ini, bukan saat membaca berkas.
func TestEmbeddedMigrations_LoadableByMigrate(t *testing.T) {
	src, err := iofs.New(FS, ".")
	if err != nil {
		t.Fatalf("golang-migrate menolak isi embed.FS: %v", err)
	}
	t.Cleanup(func() { _ = src.Close() })

	first, err := src.First()
	if err != nil {
		t.Fatalf("tidak ada migrasi pertama yang bisa dibaca: %v", err)
	}
	if first == 0 {
		t.Error("versi migrasi pertama adalah 0; penomoran dimulai dari 1")
	}
}
