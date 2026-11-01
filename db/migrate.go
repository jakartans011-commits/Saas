package db

import (
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func RunMigrations(postgresURL string) error {
	m, err := migrate.New(
		"file://migrations",
		postgresURL,
	)
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
		fmt.Printf(
			"postgres migration state: version=%d dirty=%t\n",
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

	return nil
}
