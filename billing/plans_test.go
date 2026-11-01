package billing

import (
	"testing"
	"trailseal/tenant"
)

// Mengunci registry plan terhadap tabel pricing yang berlaku.
func TestPlanRegistry_MatchesPricingTable(t *testing.T) {
	reg := NewRegistry("price_pro_test", "price_business_test")

	want := map[PlanTier]PlanConfig{
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

	for tier, w := range want {
		got, ok := reg.Get(tier)
		if !ok {
			t.Fatalf("tier %q tidak ada di registry", tier)
		}
		// StripePriceID berasal dari env, bukan bagian tabel pricing.
		got.StripePriceID = ""
		if got != w {
			t.Errorf("tier %q: got %+v, want %+v", tier, got, w)
		}
	}
}

func TestPlanRegistry_FreeMatchesGet(t *testing.T) {
	reg := NewRegistry("price_pro_test", "price_business_test")
	got, ok := reg.Get(PlanFree)
	if !ok {
		t.Fatal("tier free tidak ada")
	}
	if reg.Free() != got {
		t.Errorf("Free() = %+v, Get(PlanFree) = %+v", reg.Free(), got)
	}
}

func TestGetMonthlyEventLimit_UnknownTierFallsBackToFree(t *testing.T) {
	if got := GetMonthlyEventLimit(PlanTier("tidak-dikenal")); got != 5_000 {
		t.Errorf("got %d, want 5000", got)
	}
}

// tenant.DefaultMonthlyEventLimit disalin manual (larangan import melingkar).
// Test ini gagal bila angkanya berbeda dari plans.go.
func TestFreePlanMatchesTenantDefault(t *testing.T) {
	if got := baseDefinitions[PlanFree].MonthlyEventLimit; got != tenant.DefaultMonthlyEventLimit {
		t.Errorf("plans.go Free=%d, tenant.DefaultMonthlyEventLimit=%d", got, tenant.DefaultMonthlyEventLimit)
	}
}
