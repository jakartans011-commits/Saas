package compliance

import (
	"context"
	"fmt"
	"time"

	"trailseal/agent"
	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/auth"
	"trailseal/project"
	"trailseal/tenant"
)

// SweepExpiredTenants menghapus permanen (PurgeTenant) setiap tenant
// yang sudah soft-deleted DAN grace period-nya lewat
// (30 hari Free/Pro, 60 hari Business, lewat gracePeriodFor).
//
// Tenant yang grace period-nya belum lewat dilewati tanpa error.
// Restore tetap bisa selama tenant belum dipurge (K9: default aman).
//
// Satu tenant gagal tidak menghentikan tenant lain. Purge idempotent:
// record tenant dihapus paling akhir, jadi tenant yang gagal di tengah
// jalan akan dicoba lagi pada sweep berikutnya.
//
// Return: jumlah tenant yang berhasil dipurge dan error pertama
// (bila ada).
func SweepExpiredTenants(
	ctx context.Context,
	tenantStore *tenant.BoltTenantStore,
	userStore *auth.BoltUserStore,
	agentStore *agent.BoltAgentStore,
	projectStore *project.PostgresStore,
	apiKeyStore *apikey.PostgresStore,
	auditStore audit.Store,
) (int, error) {
	deleted, err := tenantStore.ListDeleted()
	if err != nil {
		return 0, fmt.Errorf("failed to list soft-deleted tenants: %w", err)
	}

	purged := 0
	var firstErr error

	for _, t := range deleted {
		if err := ctx.Err(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		if t.DeletedAt == nil {
			continue
		}
		if time.Now().Before(t.DeletedAt.Add(gracePeriodFor(t.PlanTier))) {
			continue
		}

		if err := PurgeTenant(
			ctx,
			tenantStore,
			userStore,
			agentStore,
			projectStore,
			apiKeyStore,
			auditStore,
			t.ID,
			nil,
		); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("tenant %s: purge failed: %w", t.ID, err)
			}
			continue
		}
		purged++
	}

	return purged, firstErr
}