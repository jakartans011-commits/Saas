// One-shot backfill: sets EmailVerifiedAt for every existing user whose
// account predates the email-verification-at-login enforcement, so they
// aren't locked out by the new check in /api/auth/login.
//
// Safe to run multiple times (idempotent — skips users that already have
// EmailVerifiedAt set). Does NOT touch DeletedAt or Status.
//
// Usage:
//   go run ./cmd/migrate-verify-existing-users -db path/to/data.db
//   go run ./cmd/migrate-verify-existing-users -db path/to/data.db -dry-run
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	bolt "go.etcd.io/bbolt"
)

var usersBucket = []byte("users")

// Minimal local copy of the fields we touch — avoids importing the auth
// package just for this, and avoids any risk of silently dropping fields
// this tool doesn't know about (we round-trip the full raw JSON below).
type userPatch struct {
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
}

func main() {
	dbPath := flag.String("db", "", "path to the bbolt .db file (required)")
	dryRun := flag.Bool("dry-run", false, "print what would change, write nothing")
	flag.Parse()

	if *dbPath == "" {
		log.Fatal("missing -db flag, e.g. -db D:/TrailSeal/data/agent.db")
	}

	db, err := bolt.Open(*dbPath, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	now := time.Now()
	var scanned, patched int

	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(usersBucket)
		if b == nil {
			return fmt.Errorf("bucket %q not found — wrong db file?", usersBucket)
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			scanned++

			// Decode into a generic map so we preserve every existing
			// field verbatim and only touch email_verified_at.
			var raw map[string]any
			if err := json.Unmarshal(v, &raw); err != nil {
				log.Printf("WARN: skip key %s: unmarshal failed: %v", k, err)
				continue
			}

			if existing, ok := raw["email_verified_at"]; ok && existing != nil {
				continue // already verified, idempotent skip
			}

			raw["email_verified_at"] = now.Format(time.RFC3339Nano)
			patched++

			if *dryRun {
				log.Printf("[dry-run] would patch user id=%s email=%v", k, raw["Email"])
				continue
			}

			newData, err := json.Marshal(raw)
			if err != nil {
				return fmt.Errorf("marshal user %s: %w", k, err)
			}
			if err := b.Put(k, newData); err != nil {
				return fmt.Errorf("put user %s: %w", k, err)
			}
		}
		return nil
	})
	if err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	mode := "APPLIED"
	if *dryRun {
		mode = "DRY RUN — no changes written"
	}
	fmt.Printf("%s: scanned %d users, %d patched (already-verified users skipped)\n", mode, scanned, patched)
}