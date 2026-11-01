package tenant

import "time"

// MarkOnboardingCompleteIfUnset menandai tenant sudah menyelesaikan
// "first value moment" (2B.4) — dipanggil SETELAH aksi produk
// pertama (event ingest) sukses.
//
// Idempotent tapi BUKAN atomic compare-and-swap (beda dari
// TryConsumeQuota) — kalau dua request pertama datang nyaris
// bersamaan, paling banter Put() jalan dua kali dengan timestamp
// mirip, bukan data yang salah. Diterima demi kesederhanaan karena
// ini bukan angka kuota yang butuh akurasi ketat.
func MarkOnboardingCompleteIfUnset(store TenantStore, tenantID string) error {
	t, err := store.Get(tenantID)
	if err != nil {
		return err
	}
	if t.OnboardingCompletedAt != nil {
		return nil
	}
	now := time.Now()
	t.OnboardingCompletedAt = &now
	return store.Put(t)
}