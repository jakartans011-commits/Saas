package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ============================================================
// POSTGRES CONNECTION POOL
//
// Dipanggil SEKALI di BuildApp/main(), pool-nya di-share ke semua
// store (project, apikey, events) — pola yang sama dengan *bolt.DB
// yang sudah dipakai untuk tenant/auth/rbac.
// ============================================================

func Connect(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	if connString == "" {
		return nil, fmt.Errorf("postgres connection string is required")
	}

	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres connection string: %w", err)
	}

	// Batas wajar untuk API service tunggal. Bisa dinaikkan lewat
	// env var kalau nanti perlu tuning, tidak hardcode ekstrem.
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return pool, nil
}
