package main

import (
	"testing"
	"time"

	"trailseal/compliance"
)

// Mengunci baris "recovery" tabel pricing: Free 30 / Pro 30 / Business 60 hari.
func TestRecoveryWindow_MatchesPricingTable(t *testing.T) {
	if compliance.DefaultGracePeriod != 30*24*time.Hour {
		t.Errorf("DefaultGracePeriod = %v, want 720h (30 hari)", compliance.DefaultGracePeriod)
	}
	if compliance.BusinessGracePeriod != 60*24*time.Hour {
		t.Errorf("BusinessGracePeriod = %v, want 1440h (60 hari)", compliance.BusinessGracePeriod)
	}

	cases := []struct {
		tier string
		want int
	}{
		{"", 30},
		{"free", 30},
		{"pro", 30},
		{"business", 60},
	}
	for _, c := range cases {
		if got := gracePeriodDaysFor(c.tier); got != c.want {
			t.Errorf("gracePeriodDaysFor(%q) = %d, want %d", c.tier, got, c.want)
		}
	}
}
