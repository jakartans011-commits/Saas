package billing

import (
	"context"
	"fmt"
	"time"

	"trailseal/audit"
	"trailseal/tenant"
)

// SweepExpiredGracePeriods dipanggil scheduler sekali per hari. Untuk tiap
// tenant yang grace period-nya sudah lewat dan masih "past_due", downgrade
// paksa ke Free dengan status akhir "canceled_nonpayment" (bukan "canceled")
// supaya churn karena gagal bayar bisa dibedakan dari churn sukarela.
//
// Satu tenant bermasalah tidak menghentikan tenant lain: error pertama
// dikembalikan, sisanya tetap diproses.
func (s *Service) SweepExpiredGracePeriods(ctx context.Context) (int, error) {
	lister, ok := s.tenantStore.(tenant.TenantPastDueLister)
	if !ok {
		return 0, fmt.Errorf("tenant_store_does_not_support_past_due_listing")
	}

	expired, err := lister.ListExpiredGracePeriods(time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to list expired grace periods: %w", err)
	}

	free := s.plans.Free()
	downgraded := 0
	var firstErr error

	for _, candidate := range expired {
		done, err := s.downgradeExpiredTenant(ctx, candidate.ID, free)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if done {
			downgraded++
		}
	}

	return downgraded, firstErr
}

// downgradeExpiredTenant menurunkan satu tenant di bawah lockTenant yang sama
// dengan yang dipakai handler webhook, sehingga sweep dan webhook tidak bisa
// saling menimpa.
//
// Mengembalikan (false, nil) bila tenant ternyata sudah tidak layak diturunkan
// saat lock diperoleh.
func (s *Service) downgradeExpiredTenant(
	ctx context.Context,
	tenantID string,
	free PlanConfig,
) (bool, error) {
	unlock := s.lockTenant(tenantID)
	defer unlock()

	t, err := s.tenantStore.Get(tenantID)
	if err != nil {
		return false, fmt.Errorf("tenant %s: %w", tenantID, err)
	}

	// Baca ulang DI DALAM lock. Antara ListExpiredGracePeriods dan lock ini,
	// webhook "subscription.updated" berstatus active bisa saja baru
	// menyelamatkan tenant yang akhirnya membayar. Tanpa pemeriksaan ulang,
	// sweep menurunkan pelanggan yang sudah bayar.
	if t.DeletedAt != nil || t.SubscriptionStatus != "past_due" {
		return false, nil
	}
	if t.GracePeriodDeadline == nil || !time.Now().After(*t.GracePeriodDeadline) {
		return false, nil
	}

	// Cabut role berbayar lebih dulu dan tanpa syarat. Tenant yang tidak
	// membayar tidak boleh menahan entitlement hanya karena langkah lain
	// tersendat. Operasi ini idempoten, jadi aman diulang sweep berikutnya.
	s.revokePaidRoles(tenantID)

	// Bila project store gagal, berhenti di sini TANPA menulis tenant:
	// tenant tetap "past_due" sehingga sweep berikutnya mengulang dan
	// keadaan bertemu. Menulis tenant sebagai Free lebih dulu akan
	// meninggalkan project yang selamanya memegang limit berbayar.
	if err := s.projectStore.ApplyPlanToTenant(ctx, tenantID, free.MonthlyEventLimit, free.RetentionDays); err != nil {
		return false, fmt.Errorf("tenant %s: failed to apply free plan to projects: %w", tenantID, err)
	}

	// Mutasi field hanya setelah semua langkah yang bisa gagal lolos:
	// InMemoryTenantStore.Get memulangkan pointer ke objek di dalam store,
	// jadi mengubahnya lebih awal akan terlihat walau Put tidak dipanggil.
	t.PlanTier = string(free.Tier)
	t.MonthlyEventLimit = free.MonthlyEventLimit
	t.SubscriptionStatus = "canceled_nonpayment"
	t.PaymentFailureCount = 0
	t.LastPaymentFailureAt = nil
	t.PaymentFailureReason = ""
	t.PastDueSince = nil
	t.GracePeriodDeadline = nil

	if err := s.tenantStore.Put(t); err != nil {
		return false, fmt.Errorf("tenant %s: failed to persist downgrade: %w", tenantID, err)
	}

	s.logBillingEvent(tenantID, "billing.downgraded_nonpayment", "tenant:"+tenantID, audit.OutcomeSuccess, map[string]string{
		"plan_tier": string(free.Tier),
	})

	return true, nil
}