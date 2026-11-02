package db

import (
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"trailseal/migrations"
)

// RunMigrations menerapkan seluruh migrasi yang belum pernah jalan.
//
// Berkas migrasinya TERTANAM di dalam binary, bukan dibaca dari disk.
// Versi sebelumnya memakai migrate.New("file://migrations", ...) — path
// relatif terhadap direktori kerja proses. Itu bekerja di mesin pengembang
// yang menjalankan `go run ./cmd/saas-api` dari akar repo, dan GAGAL di
// container yang hanya berisi binary hasil multi-stage build: server menolak
// menyala, dan pesan errornya tidak menunjuk penyebabnya.
//
// Dengan embed, binary-nya mandiri. Dijalankan dari direktori mana pun,
// dengan cara build apa pun, migrasinya selalu ikut.
func RunMigrations(postgresURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("failed to load embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, postgresURL)
	if err != nil {
		return fmt.Errorf("failed to initialize migrations: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to read migration version: %w", err)
	}

	if err == migrate.ErrNilVersion {
		fmt.Println("postgres migration state: no version")
	} else {
		// Ini keadaan SEBELUM m.Up() di bawah. Versi yang tercetak di sini
		// wajar tertinggal satu dari versi terakhir yang ada di repo.
		fmt.Printf(
			"postgres migration state before migrating: version=%d dirty=%t\n",
			version,
			dirty,
		)

		if dirty {
			if err := m.Force(int(version)); err != nil {
				return fmt.Errorf(
					"failed to clear dirty migration state at version %d: %w",
					version,
					err,
				)
			}

			fmt.Printf(
				"postgres migration state recovered: version=%d dirty=false\n",
				version,
			)
		}
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	if after, _, err := m.Version(); err == nil {
		fmt.Printf("postgres migration state after migrating: version=%d\n", after)
	}

	return nil
}
