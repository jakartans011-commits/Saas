package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	bolt "go.etcd.io/bbolt"

	"trailseal/agent"
	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/auth"
	"trailseal/billing"
	"trailseal/compliance"
	"trailseal/events"
	"trailseal/project"
	"trailseal/tenant"
)

// ============================================================
// P5-1: penegakan retensi dan purge otomatis.
//
// Scheduler harian di proses saas-api (pola sama dengan sweep grace
// period di main.go). Tiap putaran:
//   1. events.RetentionJob.RunOnce  -> purge event lewat retensi project
//   2. compliance.SweepExpiredTenants -> hard delete tenant yang
//      soft-deleted dan grace period-nya (30/30/60 hari) sudah lewat
//
// Jalan pertama 2 menit setelah start (supaya restart harian tidak
// membuat sweep tidak pernah jalan), lalu tiap 24 jam.
//
// Catatan multi-instance: bila saas-api dijalankan lebih dari satu
// instance, sweep berjalan di tiap instance. Operasi bersifat
// idempotent, tapi hanya satu instance yang disarankan menjalankan
// scheduler.
// ============================================================

const (
	retentionSweepStartupDelay = 2 * time.Minute
	retentionSweepInterval     = 24 * time.Hour
	retentionSweepTimeout      = 30 * time.Minute
)

type retentionSweeper struct {
	tenantStore  *tenant.BoltTenantStore
	userStore    *auth.BoltUserStore
	agentStore   *agent.BoltAgentStore
	projectStore *project.PostgresStore
	apiKeyStore  *apikey.PostgresStore
	auditStore   audit.Store
	eventsJob    *events.RetentionJob

	retentionStore *audit.RetentionStore
	// planRetentionDays: ceiling retensi dari plan tier (kosong/tidak
	// dikenal = Free, fail closed). Sumber sama dengan resolver di app.go.
	planRetentionDays func(planTier string) int
}

func newRetentionSweeper(db *bolt.DB, pool *pgxpool.Pool) (*retentionSweeper, error) {
	tenantStore, err := tenant.NewBoltTenantStore(db)
	if err != nil {
		return nil, fmt.Errorf("tenant store: %w", err)
	}
	userStore, err := auth.NewBoltUserStore(db)
	if err != nil {
		return nil, fmt.Errorf("user store: %w", err)
	}
	agentStore, err := agent.NewBoltAgentStore(db)
	if err != nil {
		return nil, fmt.Errorf("agent store: %w", err)
	}
	auditStore, err := audit.NewPostgresAuditStore(pool)
	if err != nil {
		return nil, fmt.Errorf("audit store: %w", err)
	}

	retentionStore, err := audit.NewRetentionStore(db)
	if err != nil {
		return nil, fmt.Errorf("retention store: %w", err)
	}

	plansRegistry := billing.NewRegistry(
		os.Getenv("STRIPE_PRICE_PRO"),
		os.Getenv("STRIPE_PRICE_BUSINESS"),
	)
	planRetentionDays := func(planTier string) int {
		tier := billing.PlanFree
		if planTier != "" {
			tier = billing.PlanTier(planTier)
		}
		plan, ok := plansRegistry.Get(tier)
		if !ok {
			plan = plansRegistry.Free() // fail closed ke Free
		}
		return plan.RetentionDays
	}

	return &retentionSweeper{
		tenantStore:       tenantStore,
		userStore:         userStore,
		agentStore:        agentStore,
		projectStore:      project.NewPostgresStore(pool),
		apiKeyStore:       apikey.NewPostgresStore(pool),
		auditStore:        auditStore,
		eventsJob:         events.NewRetentionJob(pool, nil),
		retentionStore:    retentionStore,
		planRetentionDays: planRetentionDays,
	}, nil
}

// cleanupAudit menjalankan retensi audit untuk tiap tenant aktif:
// policy efektif dari RetentionStore.GetPolicy (sudah di-clamp ke
// plan), lalu CleanupForTenant. Kegagalan satu tenant hanya di-log.
func (r *retentionSweeper) cleanupAudit(ctx context.Context) {
	active, err := r.tenantStore.ListActive()
	if err != nil {
		log.Printf("retention sweep: failed to list active tenants: %v", err)
		return
	}

	for _, t := range active {
		if ctx.Err() != nil {
			return
		}

		policy, err := r.retentionStore.GetPolicy(t.ID, r.planRetentionDays(t.PlanTier))
		if err != nil {
			log.Printf("retention sweep: tenant %s: get policy failed: %v", t.ID, err)
			continue
		}

		deleted, anonymized, err := r.auditStore.CleanupForTenant(t.ID, policy)
		if err != nil {
			log.Printf("retention sweep: tenant %s: audit cleanup failed: %v", t.ID, err)
			continue
		}
		if deleted > 0 || anonymized > 0 {
			log.Printf("retention sweep: tenant %s: audit deleted=%d anonymized=%d", t.ID, deleted, anonymized)
		}
	}
}

// runOnce menjalankan satu putaran. Kegagalan satu langkah hanya
// di-log dan tidak menghentikan langkah berikutnya.
func (r *retentionSweeper) runOnce(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("retention sweep: recovered from panic: %v", rec)
		}
	}()

	if err := r.eventsJob.RunOnce(ctx); err != nil {
		log.Printf("retention sweep: events retention error: %v", err)
	}

	r.cleanupAudit(ctx)

	n, err := compliance.SweepExpiredTenants(
		ctx,
		r.tenantStore,
		r.userStore,
		r.agentStore,
		r.projectStore,
		r.apiKeyStore,
		r.auditStore,
	)
	if err != nil {
		log.Printf("retention sweep: tenant purge error: %v", err)
	}
	if n > 0 {
		log.Printf("retention sweep: %d expired tenants purged", n)
	}
}

// startRetentionScheduler memulai goroutine scheduler. Fungsi stop
// yang dikembalikan menghentikan scheduler dan menunggu putaran yang
// sedang berjalan selesai (maks 10 detik).
func startRetentionScheduler(db *bolt.DB, pool *pgxpool.Pool) (func(), error) {
	sweeper, err := newRetentionSweeper(db, pool)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		timer := time.NewTimer(retentionSweepStartupDelay)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				runCtx, runCancel := context.WithTimeout(ctx, retentionSweepTimeout)
				sweeper.runOnce(runCtx)
				runCancel()
				timer.Reset(retentionSweepInterval)
			}
		}
	}()

	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			log.Printf("retention scheduler: stop timed out")
		}
	}
	return stop, nil
}
