// ============================================================
// billing/plans.go
//
// Source of truth TUNGGAL untuk definisi tier — webhook/checkout
// TIDAK PERNAH hardcode angka limit/retensi, semua resolve dari sini.
// ============================================================
package billing

type PlanTier string

const (
	PlanFree     PlanTier = "free"
	PlanPro      PlanTier = "pro"
	PlanBusiness PlanTier = "business"
)

type PlanConfig struct {
	Tier              PlanTier
	MonthlyEventLimit int64
	RetentionDays     int
	StripePriceID     string // kosong untuk Free — tidak ada Stripe price

	// Gate fitur per plan, mengikuti tabel pricing. Semua tipe comparable
	// (service_test.go:175 membandingkan PlanConfig dengan nilai nol).
	MCPRequestsPerMinute int  // request MCP per menit per tenant
	AuditExportMaxEvents int  // 0 = export penuh
	MaxActiveAPIKeys     int  // 0 = tanpa batas
	CustomRoles          bool // hanya Business
 }

var baseDefinitions = map[PlanTier]PlanConfig{
	PlanFree: {
		Tier: PlanFree, MonthlyEventLimit: 5_000, RetentionDays: 14,
		MCPRequestsPerMinute: 60, AuditExportMaxEvents: 100, MaxActiveAPIKeys: 1, CustomRoles: false,
	},
	PlanPro: {
		Tier: PlanPro, MonthlyEventLimit: 250_000, RetentionDays: 180,
		MCPRequestsPerMinute: 300, AuditExportMaxEvents: 0, MaxActiveAPIKeys: 0, CustomRoles: false,
	},
	PlanBusiness: {
		Tier: PlanBusiness, MonthlyEventLimit: 1_000_000, RetentionDays: 365,
		MCPRequestsPerMinute: 1000, AuditExportMaxEvents: 0, MaxActiveAPIKeys: 0, CustomRoles: true,
   },
}

type Registry struct {
	byTier  map[PlanTier]PlanConfig
	byPrice map[string]PlanTier
	annual  map[PlanTier]string // price ID tahunan per tier (opsional)
}

// NewRegistry — priceID Pro/Business diisi dari env var saat startup
// (lihat main.go), supaya price ID sandbox vs live tidak hardcode di source.
func NewRegistry(proPriceID, businessPriceID string) *Registry {
	byTier := make(map[PlanTier]PlanConfig, len(baseDefinitions))
	for k, v := range baseDefinitions {
		byTier[k] = v
	}

	pro := byTier[PlanPro]
	pro.StripePriceID = proPriceID
	byTier[PlanPro] = pro

	biz := byTier[PlanBusiness]
	biz.StripePriceID = businessPriceID
	byTier[PlanBusiness] = biz

	byPrice := make(map[string]PlanTier, 2)

	// Never register an empty Stripe price ID.
	// An empty environment variable must fail closed,
	// not become a valid billing identifier.
	if proPriceID != "" {
		byPrice[proPriceID] = PlanPro
	}

	if businessPriceID != "" {
		byPrice[businessPriceID] = PlanBusiness
	}

	return &Registry{
		byTier:  byTier,
		byPrice: byPrice,
	}
}

func (r *Registry) Get(tier PlanTier) (PlanConfig, bool) {
	c, ok := r.byTier[tier]
	return c, ok
}

func (r *Registry) ResolveByPriceID(priceID string) (PlanConfig, bool) {
	tier, ok := r.byPrice[priceID]
	if !ok {
		return PlanConfig{}, false
	}
	return r.Get(tier)
}

func (r *Registry) Free() PlanConfig {
	c := r.byTier[PlanFree]
	return c
}

// GetMonthlyEventLimit — Helper untuk portal display supaya tidak hardcode.
// Jika tier tidak dikenal, return limit untuk Free.
func GetMonthlyEventLimit(tier PlanTier) int64 {
	config, ok := baseDefinitions[tier]
	if !ok {
		return baseDefinitions[PlanFree].MonthlyEventLimit
	}
	return config.MonthlyEventLimit
}

// ============================================================
// PATCH: tenant/tenant.go
//
// Tambahkan field ini ke struct Tenant yang sudah ada. Karena bbolt
// serialize lewat JSON, INI TIDAK BUTUH MIGRATION — record tenant
// lama otomatis decode dengan field ini kosong (diperlakukan sebagai
// plan Free di seluruh kode billing).
// ============================================================

/*
type Tenant struct {
	ID        string
	Name      string
	Status    TenantStatus
	CreatedAt time.Time
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	// Billing (Stripe) — P1.
	StripeCustomerID     string `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID string `json:"stripe_subscription_id,omitempty"`
	PlanTier             string `json:"plan_tier,omitempty"`             // "", "free", "pro", "business" — "" diperlakukan sama dengan "free"
	SubscriptionStatus   string `json:"subscription_status,omitempty"`   // status Stripe: active, past_due, canceled, dst
}
*/

// ============================================================
// PATCH: project/store_postgres.go
//
// Tambahkan method ini ke PostgresStore — dipakai webhook untuk
// bulk-update semua project 1 tenant sekaligus.
// ============================================================

/*
func (s *PostgresStore) ApplyPlanToTenant(ctx context.Context, tenantID string, monthlyEventLimit int64, retentionDays int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE projects
		SET monthly_event_limit = $2, retention_days = $3
		WHERE tenant_id = $1 AND deleted_at IS NULL
	`, tenantID, monthlyEventLimit, retentionDays)
	if err != nil {
		return fmt.Errorf("failed to apply plan to tenant projects: %w", err)
	}
	return nil
}
*/

// ============================================================
// PATCH: cmd/saas-api/app.go
//
// 1. Tambahkan import "trailseal/billing" dan "os"
// 2. Di dalam BuildApp(), setelah projectStore dibuat, tambahkan:
// ============================================================

/*
	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")

	plansRegistry := billing.NewRegistry(
		os.Getenv("STRIPE_PRICE_PRO"),
		os.Getenv("STRIPE_PRICE_BUSINESS"),
	)

	billingService := billing.NewService(
		tenantStore,
		projectStore,
		plansRegistry,
		os.Getenv("STRIPE_WEBHOOK_SECRET"),
		os.Getenv("BILLING_SUCCESS_URL"), // mis. https://app.trailseal.dev/billing/success
		os.Getenv("BILLING_CANCEL_URL"),
	)
*/

// 3. Tambahkan routes ini (di dekat blok PROJECT ROUTES):
/*
	app.Post(
		"/api/tenants/:id/billing/checkout",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.billing.write",
		),
		billingService.CreateCheckoutHandler(),
	)

	// TIDAK pakai requireAuth — Stripe yang memanggil endpoint ini,
	// bukan user browser. Autentikasi lewat signature verification
	// di dalam WebhookHandler() sendiri.
	app.Post(
		"/api/billing/webhook",
		billingService.WebhookHandler(),
	)
*/

// 4. Seed capability baru "tenant.{tenant_id}.billing.write" ke role
//    "owner" di tempat kamu seed capability lain (rbac seed data) —
//    kalau tidak, owner tenant akan selalu 403 saat checkout.
