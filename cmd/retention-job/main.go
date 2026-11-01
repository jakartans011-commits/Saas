package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"trailseal/db"
	"trailseal/events"
)

func main() {
	logger := slog.New(
		slog.NewJSONHandler(os.Stdout, nil),
	)

	// Gunakan environment variable yang sama dengan saas-api.
	postgresURL := os.Getenv("TRAILSEAL_POSTGRES_URL")

	if postgresURL == "" {
		logger.Error(
			"TRAILSEAL_POSTGRES_URL is required",
		)
		os.Exit(1)
	}

	// Retention job bukan service long-running.
	// Satu eksekusi dibatasi maksimal 10 menit.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Minute,
	)
	defer cancel()

	pool, err := db.Connect(ctx, postgresURL)
	if err != nil {
		logger.Error(
			"failed to connect to postgres",
			"error", err,
		)
		os.Exit(1)
	}
	defer pool.Close()

	job := events.NewRetentionJob(
		pool,
		logger,
	)

	if err := job.RunOnce(ctx); err != nil {
		logger.Error(
			"retention job failed",
			"error", err,
		)
		os.Exit(1)
	}

	logger.Info(
		"retention job completed successfully",
	)
}