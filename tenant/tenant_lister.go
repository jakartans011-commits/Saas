package tenant

import "time"

// TenantPastDueLister adalah interface terpisah untuk cron sweep —
// mencari semua tenant yang grace period-nya sudah lewat TAPI belum
// di-downgrade (masih SubscriptionStatus == "past_due").
type TenantPastDueLister interface {
	ListExpiredGracePeriods(now time.Time) ([]*Tenant, error)
}