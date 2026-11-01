package billing

import (
	"context"
	"errors"
	"testing"

	"trailseal/tenant"
)

// fakeSubscriptionFetcher — Stripe palsu. Seluruh logika rekonsiliasi
// diuji tanpa jaringan.
type fakeSubscriptionFetcher struct {
	subs map[string]*ReconciledSubscription
	errs map[string]error

	calls int
}

func (f *fakeSubscriptionFetcher) GetSubscription(
	_ context.Context, id string,
) (*ReconciledSubscription, error) {
	f.calls++
	if err, ok := f.errs[id]; ok {
		return nil, err
	}
	sub, ok := f.subs[id]
	if !ok {
		return nil, errors.New("no such subscription")
	}
	return sub, nil
}

// newReconcileService merakit Service dengan store in-memory.
func newReconcileService(t *testing.T, fetcher SubscriptionFetcher) (*Service, *tenant.InMemoryTenantStore) {
	t.Helper()

	store := tenant.NewInMemoryTenantStore()
	registry := NewRegistry("price_pro_test", "price_business_test")

	svc := NewService(
		store,
		&noopProjectPlanApplier{},
		nil, // webhookEventStore tidak dipakai rekonsiliasi
		registry,
		"", "", "",
	)
	svc.SetSubscriptionFetcher(fetcher)

	return svc, store
}

type noopProjectPlanApplier struct {
	calls int
	fail  bool
}

func (p *noopProjectPlanApplier) ApplyPlanToTenant(
	_ context.Context, _ string, _ int64, _ int,
) error {
	p.calls++
	if p.fail {
		return errors.New("project store down")
	}
	return nil
}

func putTenant(t *testing.T, store *tenant.InMemoryTenantStore, id, plan, subID string) {
	t.Helper()
	if err := store.Put(&tenant.Tenant{
		ID:                   id,
		PlanTier:             plan,
		StripeSubscriptionID: subID,
	}); err != nil {
		t.Fatalf("seed tenant %s: %v", id, err)
	}
}

// TestReconcile_PaidButStillFree — kasus yang paling mahal.
//
// Pelanggan membayar, webhook tidak pernah sampai, plan tetap Free.
// Ia tidak akan melapor; ia akan marah lalu pergi.
func TestReconcile_PaidButStillFree(t *testing.T) {
	fetcher := &fakeSubscriptionFetcher{
		subs: map[string]*ReconciledSubscription{
			"sub_1": {ID: "sub_1", Status: "active", PriceID: "price_pro_test"},
		},
	}
	svc, store := newReconcileService(t, fetcher)
	putTenant(t, store, "tenant-1", "free", "sub_1")

	// --- dry run: terdeteksi, TIDAK diubah ---
	report, err := svc.ReconcileStripe(context.Background(), false)
	if err != nil {
		t.Fatalf("ReconcileStripe: %v", err)
	}
	if len(report.Drifted) != 1 {
		t.Fatalf("harus 1 penyimpangan, got %d", len(report.Drifted))
	}
	if report.Applied != 0 {
		t.Fatalf("dry-run tidak boleh menerapkan apa pun, got %d", report.Applied)
	}
	after, _ := store.Get("tenant-1")
	if after.PlanTier != "free" {
		t.Fatalf("dry-run mengubah plan menjadi %q — seharusnya tidak menyentuh apa pun", after.PlanTier)
	}

	// --- apply: diperbaiki ---
	report, err = svc.ReconcileStripe(context.Background(), true)
	if err != nil {
		t.Fatalf("ReconcileStripe(apply): %v", err)
	}
	if report.Applied != 1 {
		t.Fatalf("harus menerapkan 1 perbaikan, got %d", report.Applied)
	}
	after, _ = store.Get("tenant-1")
	if after.PlanTier != "pro" {
		t.Fatalf("plan setelah perbaikan = %q, want pro", after.PlanTier)
	}
	if after.MonthlyEventLimit <= 0 {
		t.Fatalf("MonthlyEventLimit tidak ikut diperbarui: %d", after.MonthlyEventLimit)
	}
}

// TestReconcile_CanceledButStillPro — sisi sebaliknya: Anda memberi
// layanan berbayar kepada orang yang sudah berhenti membayar.
func TestReconcile_CanceledButStillPro(t *testing.T) {
	fetcher := &fakeSubscriptionFetcher{
		subs: map[string]*ReconciledSubscription{
			"sub_2": {ID: "sub_2", Status: "canceled", PriceID: "price_pro_test"},
		},
	}
	svc, store := newReconcileService(t, fetcher)
	putTenant(t, store, "tenant-2", "pro", "sub_2")

	report, err := svc.ReconcileStripe(context.Background(), true)
	if err != nil {
		t.Fatalf("ReconcileStripe: %v", err)
	}
	if report.Applied != 1 {
		t.Fatalf("harus menerapkan 1 perbaikan, got %d", report.Applied)
	}

	after, _ := store.Get("tenant-2")
	if after.PlanTier != "free" {
		t.Fatalf("plan setelah dibatalkan = %q, want free", after.PlanTier)
	}
}

// TestReconcile_InSyncIsUntouched — tenant yang sudah benar tidak boleh
// disentuh. Job yang menulis ulang tiap malam walau tidak ada perubahan
// akan membanjiri audit log dan menyembunyikan kejadian yang sebenarnya.
func TestReconcile_InSyncIsUntouched(t *testing.T) {
	fetcher := &fakeSubscriptionFetcher{
		subs: map[string]*ReconciledSubscription{
			"sub_3": {ID: "sub_3", Status: "active", PriceID: "price_business_test"},
		},
	}
	svc, store := newReconcileService(t, fetcher)
	putTenant(t, store, "tenant-3", "business", "sub_3")

	report, err := svc.ReconcileStripe(context.Background(), true)
	if err != nil {
		t.Fatalf("ReconcileStripe: %v", err)
	}
	if len(report.Drifted) != 0 {
		t.Fatalf("tenant yang sinkron tidak boleh dilaporkan menyimpang: %+v", report.Drifted)
	}
	if report.Applied != 0 {
		t.Fatalf("tidak boleh ada perubahan, got %d", report.Applied)
	}
	if report.Checked != 1 {
		t.Fatalf("harus memeriksa 1 tenant, got %d", report.Checked)
	}
}

// TestReconcile_IncompleteNeverGrantsPaidPlan — pembayaran pertama belum
// selesai. handleSubscriptionUpdated sengaja tidak mengubah plan dalam
// keadaan ini; rekonsiliasi harus meniru persis, kalau tidak keduanya
// akan saling membatalkan setiap malam.
func TestReconcile_IncompleteNeverGrantsPaidPlan(t *testing.T) {
	for _, status := range []string{"incomplete", "incomplete_expired"} {
		t.Run(status, func(t *testing.T) {
			fetcher := &fakeSubscriptionFetcher{
				subs: map[string]*ReconciledSubscription{
					"sub_x": {ID: "sub_x", Status: status, PriceID: "price_business_test"},
				},
			}
			svc, store := newReconcileService(t, fetcher)
			putTenant(t, store, "tenant-x", "free", "sub_x")

			report, err := svc.ReconcileStripe(context.Background(), true)
			if err != nil {
				t.Fatalf("ReconcileStripe: %v", err)
			}
			if report.Applied != 0 || len(report.Drifted) != 0 {
				t.Fatalf("status %s tidak boleh memberi plan berbayar: applied=%d drifted=%d",
					status, report.Applied, len(report.Drifted))
			}
			after, _ := store.Get("tenant-x")
			if after.PlanTier != "free" {
				t.Fatalf("plan berubah menjadi %q pada status %s", after.PlanTier, status)
			}
		})
	}
}

// TestReconcile_UnknownPriceIsSkipped — price ID yang belum terdaftar di
// env tidak boleh membuat pelanggan diturunkan. Menebak di sini akan
// menghukum orang yang membayar lewat price baru.
func TestReconcile_UnknownPriceIsSkipped(t *testing.T) {
	fetcher := &fakeSubscriptionFetcher{
		subs: map[string]*ReconciledSubscription{
			"sub_4": {ID: "sub_4", Status: "active", PriceID: "price_yang_belum_didaftarkan"},
		},
	}
	svc, store := newReconcileService(t, fetcher)
	putTenant(t, store, "tenant-4", "pro", "sub_4")

	report, err := svc.ReconcileStripe(context.Background(), true)
	if err != nil {
		t.Fatalf("ReconcileStripe: %v", err)
	}
	if report.Applied != 0 {
		t.Fatalf("price tidak dikenal tidak boleh mengubah plan, applied=%d", report.Applied)
	}
	after, _ := store.Get("tenant-4")
	if after.PlanTier != "pro" {
		t.Fatalf("plan diturunkan menjadi %q karena price tidak dikenal", after.PlanTier)
	}
}

// TestReconcile_NoSubscriptionIdIsSkipped — tenant yang tidak pernah
// berlangganan. Free memang keadaan yang benar; memanggil Stripe untuk
// mereka hanya membuang kuota API.
func TestReconcile_NoSubscriptionIdIsSkipped(t *testing.T) {
	fetcher := &fakeSubscriptionFetcher{subs: map[string]*ReconciledSubscription{}}
	svc, store := newReconcileService(t, fetcher)
	putTenant(t, store, "tenant-5", "free", "")

	report, err := svc.ReconcileStripe(context.Background(), true)
	if err != nil {
		t.Fatalf("ReconcileStripe: %v", err)
	}
	if report.Checked != 0 {
		t.Fatalf("tenant tanpa subscription tidak boleh diperiksa, checked=%d", report.Checked)
	}
	if fetcher.calls != 0 {
		t.Fatalf("Stripe dipanggil %d kali untuk tenant tanpa subscription", fetcher.calls)
	}
}

// TestReconcile_OneFailureDoesNotStopTheRest — satu tenant bermasalah
// tidak boleh menghentikan sisanya, pola sama dengan
// SweepExpiredGracePeriods.
func TestReconcile_OneFailureDoesNotStopTheRest(t *testing.T) {
	fetcher := &fakeSubscriptionFetcher{
		subs: map[string]*ReconciledSubscription{
			"sub_ok": {ID: "sub_ok", Status: "active", PriceID: "price_pro_test"},
		},
		errs: map[string]error{
			"sub_bad": errors.New("stripe timeout"),
		},
	}
	svc, store := newReconcileService(t, fetcher)
	putTenant(t, store, "tenant-bad", "free", "sub_bad")
	putTenant(t, store, "tenant-ok", "free", "sub_ok")

	report, err := svc.ReconcileStripe(context.Background(), true)
	if err != nil {
		t.Fatalf("satu kegagalan tidak boleh menggagalkan seluruh jalan: %v", err)
	}
	if len(report.Failed) != 1 {
		t.Fatalf("harus 1 kegagalan tercatat, got %d", len(report.Failed))
	}
	if report.Applied != 1 {
		t.Fatalf("tenant yang sehat tetap harus diperbaiki, applied=%d", report.Applied)
	}

	ok, _ := store.Get("tenant-ok")
	if ok.PlanTier != "pro" {
		t.Fatalf("tenant-ok tidak diperbaiki: %q", ok.PlanTier)
	}
}

// TestReconcile_NoFetcherIsAnError — rekonsiliasi tanpa sumber kebenaran
// harus berteriak, bukan diam melaporkan "0 menyimpang". Laporan bersih
// palsu lebih berbahaya daripada error.
func TestReconcile_NoFetcherIsAnError(t *testing.T) {
	store := tenant.NewInMemoryTenantStore()
	svc := NewService(store, &noopProjectPlanApplier{}, nil,
		NewRegistry("price_pro_test", "price_business_test"), "", "", "")

	if _, err := svc.ReconcileStripe(context.Background(), false); err == nil {
		t.Fatal("tanpa SubscriptionFetcher harus error, bukan laporan bersih")
	}
}
