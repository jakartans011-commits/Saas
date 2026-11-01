package tenant

import (
	"errors"
	"sync"
	"time"
)

// ============================================================
// TENANT
// ============================================================

type TenantStatus string

const (
	TenantStatusActive        TenantStatus = "active"
	TenantStatusSuspended     TenantStatus = "suspended"
	TenantStatusDeprovisioned TenantStatus = "deprovisioned"
)

// QuotaPeriod — panjang satu periode kuota (rolling window sejak
// PeriodResetAt terakhir di-set), konsisten dengan pola
// invoiceFailureGracePeriod di billing/service.go.
const QuotaPeriod = 30 * 24 * time.Hour

// DefaultMonthlyEventLimit — dipakai HANYA saat Tenant.MonthlyEventLimit
// belum pernah diisi (tenant baru / belum checkout).
//
// PENTING: nilai ini SENGAJA disamakan manual dengan billing.PlanFree
// (lihat billing/plans.go). Package tenant TIDAK boleh import billing
// (circular import — billing sudah import tenant), jadi kalau limit
// Free tier di plans.go diubah, nilai ini WAJIB diupdate manual juga.
const DefaultMonthlyEventLimit int64 = 5_000

var ErrTenantNotFoundForQuota = errors.New("tenant_not_found_for_quota")

type Tenant struct {
	ID        string
	Name      string
	Status    TenantStatus
	CreatedAt time.Time
	DeletedAt *time.Time `json:"deleted_at,omitempty"` // GDPR soft delete

	// Billing (Stripe) — P1.
	StripeCustomerID     string `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID string `json:"stripe_subscription_id,omitempty"`
	PlanTier             string `json:"plan_tier,omitempty"`
	SubscriptionStatus   string `json:"subscription_status,omitempty"`
	NotificationEmail    string `json:"notification_email,omitempty"` // tujuan notifikasi customer (Chain Integrity, quota) — kosong = fallback ke email Owner

	PaymentFailureCount  int        `json:"payment_failure_count,omitempty"`
	LastPaymentFailureAt *time.Time `json:"last_payment_failure_at,omitempty"`
	PaymentFailureReason string     `json:"payment_failure_reason,omitempty"`
	PastDueSince         *time.Time `json:"past_due_since,omitempty"`
	GracePeriodDeadline  *time.Time `json:"grace_period_deadline,omitempty"`

	// ============================================================
	// QUOTA — POOLED DI LEVEL TENANT (keputusan bisnis: tenant =
	// unit billing, project cuma metadata/label, TIDAK ada kuota
	// sendiri). Semua MCP call dari SEMUA project milik tenant ini
	// mengurangi counter yang SAMA.
	// ============================================================

	// MonthlyEventLimit — 0 berarti "belum diisi", fallback ke
	// DefaultMonthlyEventLimit. Diisi/diupdate oleh billing service
	// saat webhook subscription.updated/deleted.
	MonthlyEventLimit int64 `json:"monthly_event_limit,omitempty"`

	// CurrentEventCount — counter berjalan dalam periode aktif.
	// HANYA boleh diubah lewat TryConsumeQuota (atomic), JANGAN
	// pernah di-set manual di tempat lain.
	CurrentEventCount int64 `json:"current_event_count,omitempty"`

	// PeriodResetAt — kapan CurrentEventCount direset ke 0. nil
	// berarti belum pernah mulai periode (akan diinisialisasi otomatis
	// oleh TryConsumeQuota panggilan pertama).
	PeriodResetAt *time.Time `json:"period_reset_at,omitempty"`

	// OnboardingCompletedAt — diisi SEKALI saat tenant berhasil kirim
	// event/MCP call pertama (Phase 2B.4 "first value moment"). nil
	// berarti belum ada aktivitas produk sama sekali. HANYA diisi
	// lewat MarkOnboardingCompleteIfUnset (tenant/onboarding.go),
	// JANGAN di-set manual di tempat lain.
	OnboardingCompletedAt *time.Time `json:"onboarding_completed_at,omitempty"`
}

// QuotaEnforcer — dipanggil MCP gateway SEBELUM eksekusi tool.
// Implementasi WAJIB atomic (cek + tambah counter dalam satu operasi),
// supaya tahan terhadap request paralel (race condition).
type QuotaEnforcer interface {
	// TryConsumeQuota mencoba menambah counter sebesar amount.
	// allowed=false berarti request HARUS ditolak (limit tercapai),
	// counter TIDAK bertambah pada kasus ini.
	// remaining = sisa kuota setelah operasi (>=0).
	TryConsumeQuota(tenantID string, amount int64) (allowed bool, remaining int64, err error)
}

// TenantStore — interface yang dipakai provisioning.go dan
// implementasi lain (InMemoryTenantStore, BoltTenantStore).
// JANGAN dihapus/diedit manual — kalau perlu method tambahan,
// buat interface terpisah di file lain (pola sama seperti
// events.EventStore/usage.EventStore).
type TenantStore interface {
	Get(tenantID string) (*Tenant, error)
	Put(t *Tenant) error
}

// InMemoryTenantStore — pola sama seperti store lain di Fase 1,
// gampang diganti ke bbolt/SQL nanti.
type InMemoryTenantStore struct {
	mu      sync.RWMutex
	tenants map[string]*Tenant
}

func NewInMemoryTenantStore() *InMemoryTenantStore {
	return &InMemoryTenantStore{tenants: make(map[string]*Tenant)}
}

func (s *InMemoryTenantStore) Put(t *Tenant) error {
	if t.ID == "" {
		return errors.New("tenant id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants[t.ID] = t
	return nil
}

func (s *InMemoryTenantStore) Get(tenantID string) (*Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantID]
	if !ok {
		return nil, errors.New("tenant_not_found")
	}

	// Soft-deleted tenants are not accessible via Get()
	if t.DeletedAt != nil {
		return nil, errors.New("tenant_not_found")
	}
	return t, nil
}

func (s *InMemoryTenantStore) ListExpiredGracePeriods(now time.Time) ([]*Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Tenant
	for _, t := range s.tenants {
		if t.DeletedAt != nil || t.SubscriptionStatus != "past_due" {
			continue
		}
		if t.GracePeriodDeadline != nil && now.After(*t.GracePeriodDeadline) {
			result = append(result, t)
		}
	}
	return result, nil
}

// FindByStripeCustomerID mencari tenant berdasarkan Stripe Customer ID.
// Tenant yang sudah soft-deleted tidak dikembalikan.
// Memenuhi interface TenantByStripeLookup (lihat tenant_lookup.go).
func (s *InMemoryTenantStore) FindByStripeCustomerID(customerID string) (*Tenant, error) {
	if customerID == "" {
		return nil, errors.New("customer_id is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, t := range s.tenants {
		if t.StripeCustomerID == customerID && t.DeletedAt == nil {
			return t, nil
		}
	}

	return nil, errors.New("tenant not found by stripe customer id")
}

// TryConsumeQuota — implementasi in-memory. Aman dari race karena
// s.mu.Lock() memegang exclusive lock selama seluruh read-modify-write,
// setara dengan single bbolt writable transaction di BoltTenantStore.
func (s *InMemoryTenantStore) TryConsumeQuota(tenantID string, amount int64) (bool, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tenants[tenantID]
	if !ok || t.DeletedAt != nil {
		return false, 0, ErrTenantNotFoundForQuota
	}

	now := time.Now()
	if t.PeriodResetAt == nil || now.After(*t.PeriodResetAt) {
		t.CurrentEventCount = 0
		next := now.Add(QuotaPeriod)
		t.PeriodResetAt = &next
	}

	limit := t.MonthlyEventLimit
	if limit <= 0 {
		limit = DefaultMonthlyEventLimit
	}

	if t.CurrentEventCount+amount > limit {
		remaining := limit - t.CurrentEventCount
		if remaining < 0 {
			remaining = 0
		}
		return false, remaining, nil
	}

	t.CurrentEventCount += amount
	return true, limit - t.CurrentEventCount, nil
}
