package billing

import "testing"

func TestPlanAllowsTeamSeats(t *testing.T) {
	cases := []struct {
		tier string
		want bool
	}{
		{"", false},
		{string(PlanFree), false},
		{"plan-tidak-dikenal", false},
		{string(PlanPro), true},
		{string(PlanBusiness), true},
	}
	for _, c := range cases {
		if got := PlanAllowsTeamSeats(c.tier); got != c.want {
			t.Errorf("PlanAllowsTeamSeats(%q) = %v, want %v", c.tier, got, c.want)
		}
	}
}