package main

import (
	"context"
	"log"
	"time"

	"trailseal/billing"
)

// Penjadwal rekonsiliasi Stripe harian.
//
// Mengikuti pola startRetentionScheduler (ctx + cancel + done + stop
// bertimeout), bukan pola channel di grace period sweeper — pola ini
// menunggu goroutine benar-benar berhenti saat shutdown, bukan sekadar
// menyuruhnya berhenti.
const (
	// Jalan pertama ditunda sebentar, bukan langsung saat start.
	// Alasannya: proses yang sering di-restart (deploy, crash, reboot)
	// akan memanggil Stripe sekali per restart kalau dijalankan seketika.
	// Beda dengan grace sweep yang memang harus jalan sekali saat start
	// karena ia bekerja dari jam dinding, bukan dari API luar.
	stripeReconcileStartupDelay = 2 * time.Minute
	stripeReconcileInterval     = 24 * time.Hour
	stripeReconcileTimeout      = 10 * time.Minute
)

func startStripeReconcileScheduler(svc *billing.Service) (func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	apply := billing.ReconcileApplyEnabled()
	if apply {
		log.Printf("stripe reconcile: MODE TERAPKAN (STRIPE_RECONCILE_APPLY=true) — plan tenant akan diubah otomatis")
	} else {
		log.Printf("stripe reconcile: mode dry-run — hanya melaporkan. Set STRIPE_RECONCILE_APPLY=true untuk menerapkan")
	}

	go func() {
		defer close(done)
		timer := time.NewTimer(stripeReconcileStartupDelay)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				runCtx, runCancel := context.WithTimeout(ctx, stripeReconcileTimeout)
				runStripeReconcile(runCtx, svc, apply)
				runCancel()
				timer.Reset(stripeReconcileInterval)
			}
		}
	}()

	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			log.Printf("stripe reconcile scheduler: stop timed out")
		}
	}
	return stop, nil
}

func runStripeReconcile(ctx context.Context, svc *billing.Service, apply bool) {
	report, err := svc.ReconcileStripe(ctx, apply)
	if err != nil {
		log.Printf("stripe reconcile error: %v", err)
		return
	}

	mode := "dry-run"
	if apply {
		mode = "diterapkan"
	}

	// Selalu dicatat, termasuk saat bersih. Baris "0 menyimpang" setiap
	// hari adalah bukti job ini benar-benar berjalan — keheningan tidak
	// bisa dibedakan dari job yang mati diam-diam.
	log.Printf(
		"stripe reconcile (%s): %d diperiksa, %d dilewati, %d menyimpang, %d diperbaiki, %d gagal",
		mode, report.Checked, report.Skipped, len(report.Drifted), report.Applied, len(report.Failed),
	)

	for _, d := range report.Drifted {
		log.Printf(
			"  MENYIMPANG tenant=%s sub=%s stripe=%s lokal=%s seharusnya=%s diterapkan=%v",
			d.TenantID, d.SubscriptionID, d.StripeStatus, d.LocalPlan, d.ExpectedPlan, d.Applied,
		)
	}
	for _, f := range report.Failed {
		log.Printf("  GAGAL tenant=%s sub=%s err=%v", f.TenantID, f.SubscriptionID, f.Err)
	}
}
