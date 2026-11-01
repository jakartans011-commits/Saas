package billing

import (
	"context"
	"fmt"
	"os"
	"strings"

	"trailseal/alerts"
	"trailseal/audit"
	"trailseal/tenant"
)

// ============================================================
// REKONSILIASI STRIPE HARIAN
//
// KENAPA INI ADA
//
// Stripe mencoba ulang webhook yang gagal selama 3 hari, lalu berhenti.
// Kalau yang gagal checkout.session.completed atau subscription.updated,
// pelanggan SUDAH MEMBAYAR tapi plan_tier-nya tetap Free. Ia tidak akan
// melapor — ia akan marah, lalu pergi.
//
// Lapis pertama (idempotensi + release klaim di webhook_event_release.go)
// menangani kegagalan sementara. Lapis ini menangani yang lolos dari situ:
// webhook yang tidak pernah sampai sama sekali, atau yang gagal melewati
// seluruh jendela retry Stripe.
//
// APA YANG DIBANDINGKAN
//
// Hanya PLAN TIER. Bukan subscription_status.
//
// Alasannya penting: grace period sweep menulis "canceled_nonpayment"
// untuk membedakan churn karena gagal bayar dari churn sukarela, sementara
// Stripe menyebutnya "canceled". Kalau rekonsiliasi memaksa status ikut
// sama, ia akan menghapus informasi bisnis itu setiap malam. Plan tier
// adalah yang menentukan apa yang pelanggan dapatkan dan apa yang Anda
// tagih — itu yang harus benar.
//
// DRY RUN LEBIH DULU
//
// Default-nya hanya melapor. Menyalakan perubahan otomatis butuh
// STRIPE_RECONCILE_APPLY=true. Job yang menulis ke database pelanggan
// setiap malam harus dibuktikan benar selama beberapa hari sebelum
// diberi izin menulis.
// ============================================================

// ReconciledSubscription — bentuk minimal yang dibutuhkan rekonsiliasi.
//
// Sengaja BUKAN *stripe.Subscription: dengan tipe kecil milik sendiri,
// seluruh logika di bawah bisa diuji dengan fake tanpa menyentuh jaringan
// maupun SDK Stripe. Pola yang sama dengan subscriptionPayload di
// service.go, yang juga menolak memakai struct SDK penuh.
type ReconciledSubscription struct {
	ID      string
	Status  string
	PriceID string
}

// SubscriptionFetcher mengambil keadaan subscription dari Stripe.
type SubscriptionFetcher interface {
	GetSubscription(ctx context.Context, subscriptionID string) (*ReconciledSubscription, error)
}

// SetSubscriptionFetcher — setter, mengikuti pola SetPortalReturnURL /
// SetAuditLogger / SetRoleStore, supaya pemanggilan NewService di app.go
// tidak perlu berubah.
func (s *Service) SetSubscriptionFetcher(f SubscriptionFetcher) {
	s.subscriptionFetcher = f
}

// ReconcileResult — satu baris laporan per tenant yang menyimpang.
type ReconcileResult struct {
	TenantID       string
	SubscriptionID string
	StripeStatus   string
	LocalPlan      string
	ExpectedPlan   string
	Applied        bool
	Err            error
}

// ReconcileReport — ringkasan satu kali jalan.
type ReconcileReport struct {
	Checked int
	Skipped int
	Drifted []ReconcileResult
	Failed  []ReconcileResult
	Applied int
	DryRun  bool
}

// ReconcileApplyEnabled membaca sakelar penerapan dari environment.
func ReconcileApplyEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("STRIPE_RECONCILE_APPLY")), "true")
}

// ReconcileStripe membandingkan plan tiap tenant dengan Stripe.
//
// Satu tenant bermasalah tidak menghentikan yang lain — pola yang sama
// dengan SweepExpiredGracePeriods.
func (s *Service) ReconcileStripe(ctx context.Context, apply bool) (ReconcileReport, error) {
	report := ReconcileReport{DryRun: !apply}

	if s.subscriptionFetcher == nil {
		return report, fmt.Errorf("subscription_fetcher_not_configured")
	}

	lister, ok := s.tenantStore.(tenant.TenantActiveLister)
	if !ok {
		return report, fmt.Errorf("tenant_store_does_not_support_active_listing")
	}

	tenants, err := lister.ListActive()
	if err != nil {
		return report, fmt.Errorf("failed to list active tenants: %w", err)
	}

	for _, t := range tenants {
		if t == nil || t.DeletedAt != nil {
			continue
		}

		// Tenant tanpa subscription ID tidak pernah berlangganan.
		// Free adalah keadaan yang benar; tidak ada yang direkonsiliasi.
		if strings.TrimSpace(t.StripeSubscriptionID) == "" {
			report.Skipped++
			continue
		}

		report.Checked++

		sub, err := s.subscriptionFetcher.GetSubscription(ctx, t.StripeSubscriptionID)
		if err != nil {
			report.Failed = append(report.Failed, ReconcileResult{
				TenantID:       t.ID,
				SubscriptionID: t.StripeSubscriptionID,
				Err:            err,
			})
			continue
		}

		expected, ok := s.expectedPlanFor(sub)
		if !ok {
			// incomplete / incomplete_expired: pembayaran pertama belum
			// selesai. handleSubscriptionUpdated sengaja TIDAK mengubah
			// plan dalam keadaan ini, jadi rekonsiliasi pun tidak boleh.
			report.Skipped++
			continue
		}

		localPlan := t.PlanTier
		if localPlan == "" {
			localPlan = string(PlanFree) // "" diperlakukan sama dengan free (plans.go)
		}

		if localPlan == string(expected.Tier) {
			continue
		}

		res := ReconcileResult{
			TenantID:       t.ID,
			SubscriptionID: sub.ID,
			StripeStatus:   sub.Status,
			LocalPlan:      localPlan,
			ExpectedPlan:   string(expected.Tier),
		}

		if apply {
			if err := s.applyReconciledPlan(ctx, t.ID, expected, sub); err != nil {
				res.Err = err
				report.Failed = append(report.Failed, res)
				continue
			}
			res.Applied = true
			report.Applied++
		}

		report.Drifted = append(report.Drifted, res)
		s.alertDrift(res, apply)
	}

	return report, nil
}

// expectedPlanFor menerjemahkan keadaan Stripe menjadi plan yang seharusnya.
//
// Aturannya MENIRU handleSubscriptionUpdated dan handleSubscriptionDeleted
// persis. Satu perbedaan kecil saja akan membuat job ini menurunkan
// pelanggan tiap malam lalu webhook menaikkannya lagi — plan berkedip, dan
// Anda mengejar hantu berminggu-minggu.
//
// ok=false berarti "jangan sentuh": keadaan yang memang sengaja diabaikan.
func (s *Service) expectedPlanFor(sub *ReconciledSubscription) (PlanConfig, bool) {
	switch sub.Status {
	case "incomplete", "incomplete_expired":
		// Sama dengan service.go: pembayaran pertama belum selesai,
		// plan tidak diubah.
		return PlanConfig{}, false

	case "canceled":
		// Sama dengan handleSubscriptionDeleted.
		return s.plans.Free(), true
	}

	if sub.PriceID == "" {
		return PlanConfig{}, false
	}

	plan, ok := s.plans.ResolveByPriceID(sub.PriceID)
	if !ok {
		// Price ID tidak dikenal. Menebak di sini berbahaya: menurunkan
		// ke Free akan menghukum pelanggan yang membayar lewat price baru
		// yang belum didaftarkan di env. Lewati, biarkan alert berbunyi.
		return PlanConfig{}, false
	}

	return plan, true
}

// applyReconciledPlan menulis plan yang benar, meniru jalur webhook.
func (s *Service) applyReconciledPlan(
	ctx context.Context,
	tenantID string,
	plan PlanConfig,
	sub *ReconciledSubscription,
) error {
	unlock := s.lockTenant(tenantID)
	defer unlock()

	// Baca ulang DI DALAM lock. Antara ListActive dan lock ini, webhook
	// yang terlambat bisa saja sudah memperbaiki keadaannya sendiri.
	t, err := s.tenantStore.Get(tenantID)
	if err != nil {
		return fmt.Errorf("tenant %s: %w", tenantID, err)
	}
	if t.DeletedAt != nil {
		return nil
	}

	localPlan := t.PlanTier
	if localPlan == "" {
		localPlan = string(PlanFree)
	}
	if localPlan == string(plan.Tier) {
		return nil // sudah benar, tidak ada yang perlu ditulis
	}

	isDowngradeToFree := plan.Tier == PlanFree

	// Urutan mengikuti grace_period_sweep.go: cabut dulu yang harus
	// dicabut, lalu langkah yang bisa gagal, baru mutasi + Put.
	if isDowngradeToFree {
		s.revokePaidRoles(tenantID)
	} else {
		s.provisionRolesForPlan(tenantID, plan.Tier)
	}

	// Kalau project store gagal, berhenti TANPA menulis tenant: keadaan
	// tetap menyimpang dan jalan berikutnya mengulang. Menulis tenant
	// lebih dulu akan meninggalkan project memegang limit yang salah.
	if err := s.projectStore.ApplyPlanToTenant(
		ctx, tenantID, plan.MonthlyEventLimit, plan.RetentionDays,
	); err != nil {
		return fmt.Errorf("tenant %s: failed to apply plan to projects: %w", tenantID, err)
	}

	t.PlanTier = string(plan.Tier)
	t.MonthlyEventLimit = plan.MonthlyEventLimit
	t.StripeSubscriptionID = sub.ID

	// subscription_status SENGAJA tidak disentuh. Lihat catatan di kepala
	// berkas: "canceled_nonpayment" dari grace sweep membawa informasi
	// bisnis yang tidak boleh ditimpa oleh "canceled" versi Stripe.

	if err := s.tenantStore.Put(t); err != nil {
		return fmt.Errorf("tenant %s: failed to persist reconciled plan: %w", tenantID, err)
	}

	s.logBillingEvent(
		tenantID,
		"billing.reconciled",
		"subscription:"+sub.ID,
		audit.OutcomeSuccess,
		map[string]string{
			"from_plan":     localPlan,
			"to_plan":       string(plan.Tier),
			"stripe_status": sub.Status,
		},
	)

	return nil
}

// alertDrift memberi tahu operator. Penyimpangan apa pun berarti ada
// webhook yang hilang — dan itu tetap perlu diselidiki walaupun sudah
// diperbaiki otomatis, karena penyebabnya belum tentu hilang.
func (s *Service) alertDrift(res ReconcileResult, applied bool) {
	action := "TERDETEKSI (dry-run, tidak diubah)"
	if applied {
		action = "DIPERBAIKI otomatis"
	}

	alerts.NotifyCritical(
		"stripe-drift-"+res.TenantID,
		"Plan tenant tidak cocok dengan Stripe",
		"Webhook Stripe kemungkinan tidak pernah sampai. "+action+".",
		map[string]string{
			"tenant_id":     res.TenantID,
			"subscription":  res.SubscriptionID,
			"stripe_status": res.StripeStatus,
			"plan_lokal":    res.LocalPlan,
			"plan_stripe":   res.ExpectedPlan,
		},
	)
}
