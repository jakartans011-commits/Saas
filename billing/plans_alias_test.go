package billing

import "testing"

func TestAddPriceAlias_LegacyResolvesToBusiness(t *testing.T) {
	reg := NewRegistry("price_pro", "price_business")

	if err := reg.AddPriceAlias("price_business_legacy_499", PlanBusiness); err != nil {
		t.Fatalf("AddPriceAlias failed: %v", err)
	}

	cfg, ok := reg.ResolveByPriceID("price_business_legacy_499")
	if !ok || cfg.Tier != PlanBusiness {
		t.Fatalf("legacy price must resolve to business, got ok=%v tier=%v", ok, cfg.Tier)
	}
	// price asli tetap berfungsi
	if cfg, ok := reg.ResolveByPriceID("price_business"); !ok || cfg.Tier != PlanBusiness {
		t.Fatalf("original business price must still resolve")
	}
}

func TestAddPriceAlias_EmptyIgnoredAndNeverResolves(t *testing.T) {
	reg := NewRegistry("price_pro", "price_business")

	if err := reg.AddPriceAlias("", PlanBusiness); err != nil {
		t.Fatalf("empty alias must be ignored, got error: %v", err)
	}
	if _, ok := reg.ResolveByPriceID(""); ok {
		t.Fatalf("empty price id must never resolve")
	}
}

func TestAddPriceAlias_RejectsFreeUnknownAndConflict(t *testing.T) {
	reg := NewRegistry("price_pro", "price_business")

	if err := reg.AddPriceAlias("price_x", PlanFree); err == nil {
		t.Errorf("alias to free tier must be rejected")
	}
	if err := reg.AddPriceAlias("price_x", PlanTier("enterprise")); err == nil {
		t.Errorf("alias to unknown tier must be rejected")
	}
	if err := reg.AddPriceAlias("price_pro", PlanBusiness); err == nil {
		t.Errorf("price already registered to pro must not be re-aliased to business")
	}
}

func TestSetAnnualPriceID_ResolvesAndPriceIDFor(t *testing.T) {
	reg := NewRegistry("price_pro", "price_business")

	if err := reg.SetAnnualPriceID(PlanPro, "price_pro_annual"); err != nil {
		t.Fatalf("SetAnnualPriceID failed: %v", err)
	}

	if cfg, ok := reg.ResolveByPriceID("price_pro_annual"); !ok || cfg.Tier != PlanPro {
		t.Fatalf("annual price must resolve to pro")
	}

	if id, ok := reg.PriceIDFor(PlanPro, IntervalMonthly); !ok || id != "price_pro" {
		t.Errorf("pro monthly: got %q ok=%v", id, ok)
	}
	if id, ok := reg.PriceIDFor(PlanPro, IntervalAnnual); !ok || id != "price_pro_annual" {
		t.Errorf("pro annual: got %q ok=%v", id, ok)
	}
	// business tahunan belum dikonfigurasi: harus ditolak, bukan jatuh ke bulanan
	if id, ok := reg.PriceIDFor(PlanBusiness, IntervalAnnual); ok {
		t.Errorf("business annual not configured must be rejected, got %q", id)
	}
	if _, ok := reg.PriceIDFor(PlanFree, IntervalMonthly); ok {
		t.Errorf("free has no price id")
	}
}

func TestParseBillingInterval(t *testing.T) {
	for in, want := range map[string]BillingInterval{
		"": IntervalMonthly, "monthly": IntervalMonthly, "annual": IntervalAnnual,
	} {
		got, ok := ParseBillingInterval(in)
		if !ok || got != want {
			t.Errorf("ParseBillingInterval(%q) = %q ok=%v, want %q", in, got, ok, want)
		}
	}
	if _, ok := ParseBillingInterval("weekly"); ok {
		t.Errorf("weekly must be rejected")
	}
}
