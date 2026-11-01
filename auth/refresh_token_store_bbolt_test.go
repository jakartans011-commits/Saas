// auth/refresh_token_store_bbolt_test.go
package auth

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openTestRefreshBoltDB(t *testing.T, path string) *bolt.DB {
	t.Helper()

	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open test bolt db: %v", err)
	}

	return db
}

func testRefreshTokenRecord(
	id string,
	userID string,
	tokenHash string,
) *RefreshTokenRecord {
	return &RefreshTokenRecord{
		ID:        id,
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
}

func TestBoltRefreshTokenStore(t *testing.T) {
	t.Run("save_and_find_by_hash_roundtrip", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "refresh_test.db")

		db := openTestRefreshBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRefreshTokenStore(db)
		if err != nil {
			t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
		}

		rec := testRefreshTokenRecord(
			"rt1",
			"u1",
			"hash-abc",
		)

		if err := store.Save(rec); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		got, err := store.FindByHash("hash-abc")
		if err != nil {
			t.Fatalf("FindByHash failed: %v", err)
		}

		if got.ID != "rt1" || got.UserID != "u1" {
			t.Fatalf("roundtrip mismatch: got %+v", got)
		}

		if got.TokenHash != "hash-abc" {
			t.Fatalf(
				"roundtrip token hash mismatch: got %q",
				got.TokenHash,
			)
		}

		if got.ExpiresAt.IsZero() {
			t.Fatal("expected ExpiresAt to survive roundtrip")
		}

		if got.RevokedAt != nil {
			t.Fatal("expected new token to be non-revoked")
		}
	})

	t.Run("find_nonexistent_hash_returns_error", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "refresh_test.db")

		db := openTestRefreshBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRefreshTokenStore(db)
		if err != nil {
			t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
		}

		_, err = store.FindByHash("does-not-exist")
		if err == nil {
			t.Fatal("expected error for nonexistent hash, got nil")
		}

		if !errors.Is(err, errors.New("refresh_token_not_found")) &&
			err.Error() != "refresh_token_not_found" {
			t.Logf("received expected lookup error: %v", err)
		}
	})

	t.Run("find_empty_hash_returns_error", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "refresh_test.db")

		db := openTestRefreshBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRefreshTokenStore(db)
		if err != nil {
			t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
		}

		_, err = store.FindByHash("")
		if err == nil {
			t.Fatal("expected error for empty token hash, got nil")
		}

		if err.Error() != "refresh_token_not_found" {
			t.Fatalf(
				"expected refresh_token_not_found, got %v",
				err,
			)
		}
	})

	t.Run("save_nil_record_returns_error", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "refresh_test.db")

		db := openTestRefreshBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRefreshTokenStore(db)
		if err != nil {
			t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
		}

		err = store.Save(nil)
		if err == nil {
			t.Fatal("expected Save(nil) to fail")
		}
	})

	t.Run("save_invalid_record_returns_error", func(t *testing.T) {
		testCases := []struct {
			name   string
			record *RefreshTokenRecord
		}{
			{
				name: "missing_id",
				record: &RefreshTokenRecord{
					UserID:    "u1",
					TokenHash: "hash",
					ExpiresAt: time.Now().Add(time.Hour),
				},
			},
			{
				name: "missing_user_id",
				record: &RefreshTokenRecord{
					ID:        "rt1",
					TokenHash: "hash",
					ExpiresAt: time.Now().Add(time.Hour),
				},
			},
			{
				name: "missing_token_hash",
				record: &RefreshTokenRecord{
					ID:        "rt1",
					UserID:    "u1",
					ExpiresAt: time.Now().Add(time.Hour),
				},
			},
			{
				name: "missing_expiry",
				record: &RefreshTokenRecord{
					ID:        "rt1",
					UserID:    "u1",
					TokenHash: "hash",
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				dbPath := filepath.Join(
					t.TempDir(),
					"refresh_validation_test.db",
				)

				db := openTestRefreshBoltDB(t, dbPath)
				defer db.Close()

				store, err := NewBoltRefreshTokenStore(db)
				if err != nil {
					t.Fatalf(
						"NewBoltRefreshTokenStore failed: %v",
						err,
					)
				}

				if err := store.Save(tc.record); err == nil {
					t.Fatal("expected invalid record to be rejected")
				}
			})
		}
	})

	t.Run("revoke_by_id_marks_revoked_at", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "refresh_test.db")

		db := openTestRefreshBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRefreshTokenStore(db)
		if err != nil {
			t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
		}

		rec := testRefreshTokenRecord(
			"rt2",
			"u2",
			"hash-def",
		)

		if err := store.Save(rec); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		before := time.Now()

		if err := store.Revoke("rt2"); err != nil {
			t.Fatalf("Revoke failed: %v", err)
		}

		got, err := store.FindByHash("hash-def")
		if err != nil {
			t.Fatalf(
				"FindByHash after revoke failed: %v",
				err,
			)
		}

		if got.RevokedAt == nil {
			t.Fatal("expected RevokedAt to be set after Revoke")
		}

		if got.RevokedAt.Before(before) {
			t.Fatalf(
				"unexpected RevokedAt timestamp: %v",
				got.RevokedAt,
			)
		}
	})

	t.Run("revoke_is_idempotent", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "refresh_test.db")

		db := openTestRefreshBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRefreshTokenStore(db)
		if err != nil {
			t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
		}

		if err := store.Save(testRefreshTokenRecord(
			"rt-idempotent",
			"u-idempotent",
			"hash-idempotent",
		)); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		if err := store.Revoke("rt-idempotent"); err != nil {
			t.Fatalf("first Revoke failed: %v", err)
		}

		first, err := store.FindByHash("hash-idempotent")
		if err != nil {
			t.Fatalf("FindByHash failed: %v", err)
		}

		if first.RevokedAt == nil {
			t.Fatal("expected first revoke timestamp")
		}

		firstRevokedAt := *first.RevokedAt

		time.Sleep(2 * time.Millisecond)

		if err := store.Revoke("rt-idempotent"); err != nil {
			t.Fatalf(
				"second Revoke should be idempotent, got: %v",
				err,
			)
		}

		second, err := store.FindByHash("hash-idempotent")
		if err != nil {
			t.Fatalf("FindByHash failed: %v", err)
		}

		if second.RevokedAt == nil {
			t.Fatal("expected RevokedAt to remain set")
		}

		if !second.RevokedAt.Equal(firstRevokedAt) {
			t.Fatalf(
				"RevokedAt changed on second revoke: first=%v second=%v",
				firstRevokedAt,
				*second.RevokedAt,
			)
		}
	})

	t.Run("revoke_nonexistent_id_returns_error", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "refresh_test.db")

		db := openTestRefreshBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRefreshTokenStore(db)
		if err != nil {
			t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
		}

		err = store.Revoke("does-not-exist")
		if err == nil {
			t.Fatal(
				"expected error revoking nonexistent id, got nil",
			)
		}

		if err.Error() != "refresh_token_not_found" {
			t.Fatalf(
				"expected refresh_token_not_found, got %v",
				err,
			)
		}
	})

	t.Run("full_rotation_flow_with_bolt_backend", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "refresh_test.db")

		db := openTestRefreshBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRefreshTokenStore(db)
		if err != nil {
			t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
		}

		oldToken, err := GenerateRefreshToken(
			"user-rotation-test",
			store,
		)
		if err != nil {
			t.Fatalf("GenerateRefreshToken failed: %v", err)
		}

		newToken, err := RotateRefreshToken(
			oldToken,
			"user-rotation-test",
			store,
		)
		if err != nil {
			t.Fatalf("RotateRefreshToken failed: %v", err)
		}

		if newToken == "" {
			t.Fatal("expected new refresh token")
		}

		if newToken == oldToken {
			t.Fatal("expected new token to differ from old")
		}

		_, err = RotateRefreshToken(
			oldToken,
			"user-rotation-test",
			store,
		)
		if err == nil {
			t.Fatal(
				"expected old revoked token reuse to fail",
			)
		}

		if err.Error() != "refresh_token_already_used" {
			t.Fatalf(
				"expected refresh_token_already_used, got %v",
				err,
			)
		}

		// New token must remain valid.
		nextToken, err := RotateRefreshToken(
			newToken,
			"user-rotation-test",
			store,
		)
		if err != nil {
			t.Fatalf(
				"new refresh token should remain usable: %v",
				err,
			)
		}

		if nextToken == "" || nextToken == newToken {
			t.Fatal(
				"expected another distinct rotated token",
			)
		}
	})

	t.Run("expired_refresh_token_is_rejected", func(t *testing.T) {
		dbPath := filepath.Join(
			t.TempDir(),
			"refresh_expired_test.db",
		)

		db := openTestRefreshBoltDB(t, dbPath)
		defer db.Close()

		store, err := NewBoltRefreshTokenStore(db)
		if err != nil {
			t.Fatalf("NewBoltRefreshTokenStore failed: %v", err)
		}

		token, err := GenerateRefreshToken(
			"user-expired-test",
			store,
		)
		if err != nil {
			t.Fatalf("GenerateRefreshToken failed: %v", err)
		}

		record, err := store.FindByHash(hashToken(token))
		if err != nil {
			t.Fatalf("FindByHash failed: %v", err)
		}

		// Paksa token menjadi expired untuk menguji rejection path.
		record.ExpiresAt = time.Now().Add(-1 * time.Minute)

		if err := store.Save(record); err != nil {
			t.Fatalf(
				"Save expired record failed: %v",
				err,
			)
		}

		_, err = RotateRefreshToken(
			token,
			"user-expired-test",
			store,
		)

		if err == nil {
			t.Fatal(
				"expected expired refresh token to be rejected",
			)
		}

		if err.Error() != "refresh_token_expired" {
			t.Fatalf(
				"expected refresh_token_expired, got %v",
				err,
			)
		}

		t.Log("OK: expired refresh token rejected")
	})

	t.Run("data_survives_close_and_reopen", func(t *testing.T) {
		persistPath := filepath.Join(
			t.TempDir(),
			"refresh_persist_test.db",
		)

		db1 := openTestRefreshBoltDB(t, persistPath)

		store1, err := NewBoltRefreshTokenStore(db1)
		if err != nil {
			db1.Close()
			t.Fatalf(
				"NewBoltRefreshTokenStore failed: %v",
				err,
			)
		}

		if err := store1.Save(testRefreshTokenRecord(
			"rt-persist",
			"u-persist",
			"hash-persist",
		)); err != nil {
			db1.Close()
			t.Fatalf("Save failed: %v", err)
		}

		if err := db1.Close(); err != nil {
			t.Fatalf("failed to close db: %v", err)
		}

		db2 := openTestRefreshBoltDB(t, persistPath)
		defer db2.Close()

		store2, err := NewBoltRefreshTokenStore(db2)
		if err != nil {
			t.Fatalf(
				"NewBoltRefreshTokenStore failed: %v",
				err,
			)
		}

		got, err := store2.FindByHash("hash-persist")
		if err != nil {
			t.Fatalf("persistence failed: %v", err)
		}

		if got.ID != "rt-persist" {
			t.Fatalf(
				"persisted ID corrupted: got %+v",
				got,
			)
		}

		if got.UserID != "u-persist" {
			t.Fatalf(
				"persisted UserID corrupted: got %+v",
				got,
			)
		}

		if got.TokenHash != "hash-persist" {
			t.Fatalf(
				"persisted TokenHash corrupted: got %+v",
				got,
			)
		}

		if got.ExpiresAt.IsZero() {
			t.Fatal(
				"persisted ExpiresAt should not be zero",
			)
		}
	})

	t.Run("revoke_survives_close_and_reopen", func(t *testing.T) {
		persistPath := filepath.Join(
			t.TempDir(),
			"refresh_revoke_persist_test.db",
		)

		db1 := openTestRefreshBoltDB(t, persistPath)

		store1, err := NewBoltRefreshTokenStore(db1)
		if err != nil {
			db1.Close()
			t.Fatalf(
				"NewBoltRefreshTokenStore failed: %v",
				err,
			)
		}

		if err := store1.Save(testRefreshTokenRecord(
			"rt-revoke-persist",
			"u-revoke-persist",
			"hash-revoke-persist",
		)); err != nil {
			db1.Close()
			t.Fatalf("Save failed: %v", err)
		}

		if err := store1.Revoke("rt-revoke-persist"); err != nil {
			db1.Close()
			t.Fatalf("Revoke failed: %v", err)
		}

		if err := db1.Close(); err != nil {
			t.Fatalf("failed to close db: %v", err)
		}

		db2 := openTestRefreshBoltDB(t, persistPath)
		defer db2.Close()

		store2, err := NewBoltRefreshTokenStore(db2)
		if err != nil {
			t.Fatalf(
				"NewBoltRefreshTokenStore failed: %v",
				err,
			)
		}

		got, err := store2.FindByHash("hash-revoke-persist")
		if err != nil {
			t.Fatalf("FindByHash failed: %v", err)
		}

		if got.RevokedAt == nil {
			t.Fatal(
				"SECURITY FAILURE: revoked state was lost after reopen",
			)
		}
	})
}

func TestRefreshTokenTheftScenario(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"refresh_theft_test.db",
	)

	db := openTestRefreshBoltDB(t, dbPath)
	defer db.Close()

	store, err := NewBoltRefreshTokenStore(db)
	if err != nil {
		t.Fatalf(
			"NewBoltRefreshTokenStore failed: %v",
			err,
		)
	}

	// Legitimate client receives token A.
	tokenA, err := GenerateRefreshToken(
		"user-theft-test",
		store,
	)
	if err != nil {
		t.Fatalf(
			"GenerateRefreshToken failed: %v",
			err,
		)
	}

	// Legitimate client rotates token A into token B.
	tokenB, err := RotateRefreshToken(
		tokenA,
		"user-theft-test",
		store,
	)
	if err != nil {
		t.Fatalf(
			"legitimate rotation failed: %v",
			err,
		)
	}

	if tokenB == "" {
		t.Fatal("expected rotated token B")
	}

	if tokenA == tokenB {
		t.Fatal(
			"token rotation did not produce a new token",
		)
	}

	// Attacker possesses stolen token A and attempts reuse.
	_, err = RotateRefreshToken(
		tokenA,
		"user-theft-test",
		store,
	)
	if err == nil {
		t.Fatal(
			"SECURITY FAILURE: stolen refresh token was accepted",
		)
	}

	if err.Error() != "refresh_token_already_used" {
		t.Fatalf(
			"expected refresh_token_already_used, got %v",
			err,
		)
	}

	// The legitimate token B must still work.
	tokenC, err := RotateRefreshToken(
		tokenB,
		"user-theft-test",
		store,
	)
	if err != nil {
		t.Fatalf(
			"SECURITY FAILURE: legitimate token B rejected: %v",
			err,
		)
	}

	if tokenC == "" {
		t.Fatal("expected token C")
	}

	if tokenC == tokenB {
		t.Fatal(
			"expected token C to differ from token B",
		)
	}

	// Token B is now revoked and must not work again.
	_, err = RotateRefreshToken(
		tokenB,
		"user-theft-test",
		store,
	)
	if err == nil {
		t.Fatal(
			"SECURITY FAILURE: previously rotated token B was reused",
		)
	}

	if err.Error() != "refresh_token_already_used" {
		t.Fatalf(
			"expected refresh_token_already_used for token B, got %v",
			err,
		)
	}

	t.Log("OK: stolen refresh token reuse rejected")
	t.Log("OK: legitimate rotated token remains usable")
}

func TestBoltRefreshTokenStore_NilDatabase(t *testing.T) {
	if _, err := NewBoltRefreshTokenStore(nil); err == nil {
		t.Fatal(
			"expected NewBoltRefreshTokenStore(nil) to fail",
		)
	}
}
