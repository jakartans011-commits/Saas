package billing

import (
	"testing"
)

func TestRegistry_FreePlan(t *testing.T) {
	registry := NewRegistry(
		"price_pro_test",
		"price_business_test",
	)

	got, ok := registry.Get(PlanFree)
	if !ok {
		t.Fatal("expected free plan to exist")
	}

	if got.Tier != PlanFree {
		t.Fatalf("expected tier %q, got %q", PlanFree, got.Tier)
	}

	if got.MonthlyEventLimit != 5_000 {
		t.Fatalf(
			"expected free monthly event limit 5000, got %d",
			got.MonthlyEventLimit,
		)
	}

	if got.RetentionDays != 14 {
		t.Fatalf(
			"expected free retention 14 days, got %d",
			got.RetentionDays,
		)
	}

	if got.StripePriceID != "" {
		t.Fatalf(
			"free plan must not have Stripe price ID, got %q",
			got.StripePriceID,
		)
	}
}

func TestRegistry_ProPlan(t *testing.T) {
	registry := NewRegistry(
		"price_pro_test",
		"price_business_test",
	)

	got, ok := registry.Get(PlanPro)
	if !ok {
		t.Fatal("expected pro plan to exist")
	}

	if got.Tier != PlanPro {
		t.Fatalf("expected tier %q, got %q", PlanPro, got.Tier)
	}

	if got.MonthlyEventLimit != 250_000 {
		t.Fatalf(
			"expected pro monthly event limit 250000, got %d",
			got.MonthlyEventLimit,
		)
	}

	if got.RetentionDays != 180 {
		t.Fatalf(
			"expected pro retention 180 days, got %d",
			got.RetentionDays,
		)
	}

	if got.StripePriceID != "price_pro_test" {
		t.Fatalf(
			"expected pro Stripe price ID %q, got %q",
			"price_pro_test",
			got.StripePriceID,
		)
	}
}

func TestRegistry_BusinessPlan(t *testing.T) {
	registry := NewRegistry(
		"price_pro_test",
		"price_business_test",
	)

	got, ok := registry.Get(PlanBusiness)
	if !ok {
		t.Fatal("expected business plan to exist")
	}

	if got.Tier != PlanBusiness {
		t.Fatalf("expected tier %q, got %q", PlanBusiness, got.Tier)
	}

	if got.MonthlyEventLimit != 1_000_000 {
		t.Fatalf(
			"expected business monthly event limit 1000000, got %d",
			got.MonthlyEventLimit,
		)
	}

	if got.RetentionDays != 365 {
		t.Fatalf(
			"expected business retention 365 days, got %d",
			got.RetentionDays,
		)
	}

	if got.StripePriceID != "price_business_test" {
		t.Fatalf(
			"expected business Stripe price ID %q, got %q",
			"price_business_test",
			got.StripePriceID,
		)
	}
}

func TestRegistry_ResolveByPriceID(t *testing.T) {
	registry := NewRegistry(
		"price_pro_test",
		"price_business_test",
	)

	tests := []struct {
		name     string
		priceID  string
		wantTier PlanTier
	}{
		{
			name:     "pro",
			priceID:  "price_pro_test",
			wantTier: PlanPro,
		},
		{
			name:     "business",
			priceID:  "price_business_test",
			wantTier: PlanBusiness,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := registry.ResolveByPriceID(tt.priceID)
			if !ok {
				t.Fatalf(
					"expected price ID %q to resolve",
					tt.priceID,
				)
			}

			if got.Tier != tt.wantTier {
				t.Fatalf(
					"expected tier %q, got %q",
					tt.wantTier,
					got.Tier,
				)
			}
		})
	}
}

func TestRegistry_UnknownPriceIDFailsClosed(t *testing.T) {
	registry := NewRegistry(
		"price_pro_test",
		"price_business_test",
	)

	got, ok := registry.ResolveByPriceID("price_attacker_controlled")
	if ok {
		t.Fatal("unknown Stripe price ID must not resolve")
	}

	if got != (PlanConfig{}) {
		t.Fatalf(
			"expected zero PlanConfig for unknown price, got %+v",
			got,
		)
	}
}

func TestRegistry_FreeIsNotStripeBillable(t *testing.T) {
	registry := NewRegistry(
		"price_pro_test",
		"price_business_test",
	)

	free := registry.Free()

	if free.Tier != PlanFree {
		t.Fatalf("expected free tier, got %q", free.Tier)
	}

	if free.StripePriceID != "" {
		t.Fatalf(
			"free plan must have empty Stripe price ID, got %q",
			free.StripePriceID,
		)
	}
}

func TestRegistry_AllPaidPlansHaveStripePriceIDs(t *testing.T) {
	registry := NewRegistry(
		"price_pro_test",
		"price_business_test",
	)

	for _, tier := range []PlanTier{
		PlanPro,
		PlanBusiness,
	} {
		t.Run(string(tier), func(t *testing.T) {
			plan, ok := registry.Get(tier)
			if !ok {
				t.Fatalf("plan %q not found", tier)
			}

			if plan.StripePriceID == "" {
				t.Fatalf(
					"paid plan %q must have Stripe price ID",
					tier,
				)
			}
		})
	}
}

func TestRegistry_PlanDefinitionsAreIndependentCopies(t *testing.T) {
	registry := NewRegistry(
		"price_pro_test",
		"price_business_test",
	)

	pro, ok := registry.Get(PlanPro)
	if !ok {
		t.Fatal("pro plan not found")
	}

	pro.MonthlyEventLimit = 1

	again, ok := registry.Get(PlanPro)
	if !ok {
		t.Fatal("pro plan not found on second lookup")
	}

	if again.MonthlyEventLimit != 250_000 {
		t.Fatalf(
			"mutating returned PlanConfig must not mutate registry; got %d",
			again.MonthlyEventLimit,
		)
	}
}

func TestRegistry_EmptyPaidPriceIDsDoNotResolve(t *testing.T) {
	registry := NewRegistry("", "")

	if _, ok := registry.ResolveByPriceID(""); ok {
		t.Fatal("empty Stripe price ID must not resolve to a paid plan")
	}

	pro, ok := registry.Get(PlanPro)
	if !ok {
		t.Fatal("pro plan should still exist")
	}

	if pro.StripePriceID != "" {
		t.Fatalf(
			"expected empty pro Stripe price ID in test registry, got %q",
			pro.StripePriceID,
		)
	}
}
