package usage

import (
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	"encoding/json"
)

type HistoryPoint struct {
	Date   string `json:"date"`
	Billed int    `json:"billed"`
}

type HistoryBreakdownItem struct {
	Key    string
	Billed int
}

// UsageHistoryResult — data mentah hasil scan, belum dibentuk jadi JSON
// kontrak (itu tanggung jawab handler, 3B). ByAPIKey pakai key "" untuk
// event yang tidak punya metadata["api_key"] (jalur dashboard/JWT,
// metadata-nya "user_id" bukan "api_key" — lihat MCPUsageMeteringDashboard).
type UsageHistoryResult struct {
	From        string // YYYY-MM-DD UTC, awal window
	To          string // YYYY-MM-DD UTC, hari ini (parsial)
	Series      []HistoryPoint
	ByAPIKey    map[string]int
	ByEventType map[string]int
}

// UsageHistoryForTenant — satu kali scan, pola SAMA PERSIS seperti
// CountBillableSinceForTenant: prefix tenantID+":", bucket newest-first,
// berhenti begitu event lebih tua dari awal window. Hanya event yang
// lolos IsBillableEventType yang dihitung (tidak ada definisi billable
// kedua, sesuai instruksi 3A).
//
// days sudah diasumsikan valid (1-30) — clamp jadi tanggung jawab
// handler (3B poin 2), bukan fungsi ini.
func (s *BoltEventStore) UsageHistoryForTenant(tenantID string, days int) (*UsageHistoryResult, error) {
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := todayStart.AddDate(0, 0, -(days - 1))

	series := make(map[string]int, days)
	for i := 0; i < days; i++ {
		d := windowStart.AddDate(0, 0, i).Format("2006-01-02")
		series[d] = 0
	}
	byKey := map[string]int{}
	byType := map[string]int{}

	prefix := []byte(tenantID + ":")

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usageEventsBucket)
		if b == nil {
			return nil
		}
		c := b.Cursor()

		for k, v := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, v = c.Next() {
			var event UsageEvent
			if err := json.Unmarshal(v, &event); err != nil {
				continue
			}

			if event.Timestamp.Before(windowStart) {
				// Newest-first: semua event berikutnya juga lebih tua
				// dari window — aman berhenti di sini (pola sama
				// seperti CountBillableSinceForTenant).
				break
			}

			if !IsBillableEventType(event.EventType) {
				continue
			}

			day := event.Timestamp.UTC().Format("2006-01-02")
			if _, ok := series[day]; ok {
				series[day]++
			}

			apiKey := event.Metadata["api_key"] // "" kalau dari jalur dashboard
			byKey[apiKey]++
			byType[string(event.EventType)]++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	ordered := make([]HistoryPoint, 0, days)
	for i := 0; i < days; i++ {
		d := windowStart.AddDate(0, 0, i).Format("2006-01-02")
		ordered = append(ordered, HistoryPoint{Date: d, Billed: series[d]})
	}

	return &UsageHistoryResult{
		From:        windowStart.Format("2006-01-02"),
		To:          todayStart.Format("2006-01-02"),
		Series:      ordered,
		ByAPIKey:    byKey,
		ByEventType: byType,
	}, nil
}