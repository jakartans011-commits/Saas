package main

import (
	"testing"
	"time"

	"trailseal/usage"
)

// --- quotaLimitForPlan ---

func TestQuotaLimitForPlan_KnownTiers(t *testing.T) {
	cases := map[string]int{
		"":         5000,
		"free":     5000,
		"pro":      250000,
		"business": 1000000,
	}
	for tier, want := range cases {
		got := quotaLimitForPlan(tier)
		if got != want {
			t.Errorf("quotaLimitForPlan(%q) = %d, want %d", tier, got, want)
		}
	}
}

func TestQuotaLimitForPlan_UnknownTier_FallsBackToDefault(t *testing.T) {
	got := quotaLimitForPlan("enterprise-custom-unknown")
	want := 5000 // Default to Free
	if got != want {
		t.Fatalf("expected fallback to default limit %d, got %d", want, got)
	}
}

// --- buildDailySeries ---

func TestBuildDailySeries_EmptyEvents_AllZero(t *testing.T) {
	series := buildDailySeries(nil, 7)
	if len(series) != 7 {
		t.Fatalf("expected 7 days, got %d", len(series))
	}
	for _, d := range series {
		if d.Count != 0 || d.Percent != 0 {
			t.Fatalf("expected zero count/percent for empty events, got %+v", d)
		}
	}
}

func TestBuildDailySeries_CountsEventsPerDay(t *testing.T) {
	now := time.Now().UTC()
	events := []*usage.UsageEvent{
		{Timestamp: now},
		{Timestamp: now},
		{Timestamp: now.AddDate(0, 0, -1)},
	}

	series := buildDailySeries(events, 3)
	if len(series) != 3 {
		t.Fatalf("expected 3 days, got %d", len(series))
	}

	total := 0
	for _, d := range series {
		total += d.Count
	}
	if total != 3 {
		t.Fatalf("expected total count of 3 across the series, got %d", total)
	}

	// Last entry in the series must be "today" and have the 2 same-day events.
	last := series[len(series)-1]
	if last.Count != 2 {
		t.Fatalf("expected today's bucket to have 2 events, got %d", last.Count)
	}
	if last.Percent != 100 {
		t.Fatalf("expected today's bucket (the max) to be 100%%, got %d", last.Percent)
	}
}

func TestBuildDailySeries_EventsOutsideWindow_Ignored(t *testing.T) {
	now := time.Now().UTC()
	events := []*usage.UsageEvent{
		{Timestamp: now.AddDate(0, 0, -30)}, // way outside a 7-day window
	}

	series := buildDailySeries(events, 7)
	total := 0
	for _, d := range series {
		total += d.Count
	}
	if total != 0 {
		t.Fatalf("expected events outside the window to be ignored, got total %d", total)
	}
}

func TestBuildDailySeries_ZeroDays_ReturnsEmpty(t *testing.T) {
	series := buildDailySeries(nil, 0)
	if len(series) != 0 {
		t.Fatalf("expected empty series for 0 days, got %d", len(series))
	}
}

// --- buildEventTypeBreakdown ---

func TestBuildEventTypeBreakdown_ComputesPercentages(t *testing.T) {
	counts := map[string]int{
		"ingest": 80,
		"verify": 20,
	}
	breakdown := buildEventTypeBreakdown(counts, 100)

	if len(breakdown) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(breakdown))
	}
	// Sorted descending by count — "ingest" (80) must come first.
	if breakdown[0].EventType != "ingest" || breakdown[0].Percent != 80 {
		t.Fatalf("expected first entry to be ingest at 80%%, got %+v", breakdown[0])
	}
	if breakdown[1].EventType != "verify" || breakdown[1].Percent != 20 {
		t.Fatalf("expected second entry to be verify at 20%%, got %+v", breakdown[1])
	}
}

func TestBuildEventTypeBreakdown_ZeroTotal_ZeroPercent(t *testing.T) {
	counts := map[string]int{"ingest": 0}
	breakdown := buildEventTypeBreakdown(counts, 0)

	if len(breakdown) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(breakdown))
	}
	if breakdown[0].Percent != 0 {
		t.Fatalf("expected 0%% when total is 0, got %d", breakdown[0].Percent)
	}
}

func TestBuildEventTypeBreakdown_EmptyInput_ReturnsEmpty(t *testing.T) {
	breakdown := buildEventTypeBreakdown(map[string]int{}, 0)
	if len(breakdown) != 0 {
		t.Fatalf("expected empty breakdown, got %d entries", len(breakdown))
	}
}

func TestBuildEventTypeBreakdown_SortedDescendingByCount(t *testing.T) {
	counts := map[string]int{
		"a": 5,
		"b": 50,
		"c": 20,
	}
	breakdown := buildEventTypeBreakdown(counts, 75)

	for i := 1; i < len(breakdown); i++ {
		if breakdown[i-1].Count < breakdown[i].Count {
			t.Fatalf("expected descending order by count, got %+v", breakdown)
		}
	}
}
