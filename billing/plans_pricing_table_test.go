package billing

import "testing"

// Mengunci seluruh nilai tabel pricing di registry. Ubah angka di plans.go
// tanpa mengubah tabel pricing = test ini merah.
func TestPricingTable_FullLock(t *testing.T) {
	reg := NewRegistry("price_pro_test", "price_business_test")

	want := []struct {
		tier        PlanTier
		events      int64
		retention   int
		mcpPerMin   int
		exportMax   int
		maxAPIKeys  int
		customRoles bool
	}{
		{PlanFree, 5_000, 14, 60, 100, 1, false},
		{PlanPro, 250_000, 180, 300, 0, 0, false},
		{PlanBusiness, 1_000_000, 365, 1000, 0, 0, true},
	}

	for _, w := range want {
		p, ok := reg.Get(w.tier)
		if !ok {
			t.Fatalf("plan %q tidak ada di registry", w.tier)
		}
		if p.MonthlyEventLimit != w.events {
			t.Errorf("%s events: got %d want %d", w.tier, p.MonthlyEventLimit, w.events)
		}
		if p.RetentionDays != w.retention {
			t.Errorf("%s retention: got %d want %d", w.tier, p.RetentionDays, w.retention)
		}
		if p.MCPRequestsPerMinute != w.mcpPerMin {
			t.Errorf("%s mcp/min: got %d want %d", w.tier, p.MCPRequestsPerMinute, w.mcpPerMin)
		}
		if p.AuditExportMaxEvents != w.exportMax {
			t.Errorf("%s export cap: got %d want %d", w.tier, p.AuditExportMaxEvents, w.exportMax)
		}
		if p.MaxActiveAPIKeys != w.maxAPIKeys {
			t.Errorf("%s api keys: got %d want %d", w.tier, p.MaxActiveAPIKeys, w.maxAPIKeys)
		}
		if p.CustomRoles != w.customRoles {
			t.Errorf("%s custom roles: got %v want %v", w.tier, p.CustomRoles, w.customRoles)
		}
	}

	if reg.Free().MonthlyEventLimit != 5_000 {
		t.Error("Free() harus sama dengan plan Free")
	}
}
