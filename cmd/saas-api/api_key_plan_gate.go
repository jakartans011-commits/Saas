package main

import (
	"context"

	"trailseal/apikey"
	"trailseal/middleware"
)

type oldestKeyChecker interface {
	IsAmongOldestActive(ctx context.Context, tenantID, keyID string, limit int) (bool, error)
}

// newAPIKeyPlanGate menegakkan batas key aktif per plan saat key dipakai.
// Plan dengan batas (Free = 1): hanya key aktif paling lama yang berlaku;
// key lain ditolak tetapi tidak dihapus, dan pulih otomatis saat upgrade.
// Batas <= 0 (Pro, Business) = tanpa batas, tanpa query tambahan.
func newAPIKeyPlanGate(checker oldestKeyChecker, resolve apikey.KeyLimitResolver) middleware.APIKeyGate {
	return func(ctx context.Context, key *apikey.APIKey) (bool, error) {
		if resolve == nil {
			return true, nil
		}
		limit := resolve(key.TenantID)
		if limit <= 0 {
			return true, nil
		}
		return checker.IsAmongOldestActive(ctx, key.TenantID, key.ID, limit)
	}
}
