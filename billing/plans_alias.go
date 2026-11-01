package billing

import "fmt"

// ============================================================
// P3-1: alias price ID dan opsi tahunan.
//
// NewRegistry(pro, business) TIDAK diubah (dipakai banyak test).
// Price ID tambahan didaftarkan sesudahnya, saat startup:
//   - legacy (mis. Business $499 lama): tetap ter-resolve ke tier
//     Business supaya webhook pelanggan lama tidak gagal
//     unknown_price_id
//   - tahunan: ter-resolve ke tier yang sama dengan bulanannya
//
// Method di sini hanya dipanggil saat startup (sebelum request
// pertama), jadi tidak butuh mutex.
// ============================================================

// BillingInterval adalah periode tagihan pada checkout.
type BillingInterval string

const (
	IntervalMonthly BillingInterval = "monthly"
	IntervalAnnual  BillingInterval = "annual"
)

// ParseBillingInterval: string kosong dianggap monthly (default).
func ParseBillingInterval(s string) (BillingInterval, bool) {
	switch BillingInterval(s) {
	case "", IntervalMonthly:
		return IntervalMonthly, true
	case IntervalAnnual:
		return IntervalAnnual, true
	}
	return "", false
}

// AddPriceAlias mendaftarkan price ID tambahan ke sebuah tier berbayar.
// Price ID kosong diabaikan (env belum diisi = fitur belum aktif,
// fail closed). Price ID yang sudah terdaftar ke tier LAIN ditolak.
func (r *Registry) AddPriceAlias(priceID string, tier PlanTier) error {
	if priceID == "" {
		return nil
	}
	if tier == PlanFree {
		return fmt.Errorf("cannot alias a price id to the free tier")
	}
	if _, ok := r.byTier[tier]; !ok {
		return fmt.Errorf("unknown tier %q", tier)
	}
	if existing, ok := r.byPrice[priceID]; ok && existing != tier {
		return fmt.Errorf("price id %s is already registered to tier %s", priceID, existing)
	}
	r.byPrice[priceID] = tier
	return nil
}

// SetAnnualPriceID mendaftarkan price ID tahunan untuk tier berbayar:
// ter-resolve ke tier yang sama (webhook) dan dipakai checkout
// interval annual. Price ID kosong diabaikan.
func (r *Registry) SetAnnualPriceID(tier PlanTier, priceID string) error {
	if priceID == "" {
		return nil
	}
	if err := r.AddPriceAlias(priceID, tier); err != nil {
		return err
	}
	if r.annual == nil {
		r.annual = make(map[PlanTier]string, 2)
	}
	r.annual[tier] = priceID
	return nil
}

// PriceIDFor mengembalikan price ID Stripe untuk tier dan interval.
// ok=false bila belum dikonfigurasi (mis. tahunan belum diisi env),
// sehingga checkout bisa menolak dengan jelas, bukan memakai price
// yang salah.
func (r *Registry) PriceIDFor(tier PlanTier, interval BillingInterval) (string, bool) {
	cfg, ok := r.byTier[tier]
	if !ok || tier == PlanFree {
		return "", false
	}
	if interval == IntervalAnnual {
		id := r.annual[tier]
		return id, id != ""
	}
	return cfg.StripePriceID, cfg.StripePriceID != ""
}
