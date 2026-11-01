package billing

import "testing"

func TestPlanAllowsCustomRoles(t *testing.T) {
	cases := []struct {
		tier string
		want bool
	}{
		{"", false},
		{string(PlanFree), false},
		{string(PlanPro), false},
		{"plan-tidak-dikenal", false},
		{string(PlanBusiness), true},
	}
	for _, c := range cases {
		if got := PlanAllowsCustomRoles(c.tier); got != c.want {
			t.Errorf("PlanAllowsCustomRoles(%q) = %v, want %v", c.tier, got, c.want)
		}
	}
}