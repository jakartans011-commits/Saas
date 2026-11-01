package usage

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// BoltEventStore — persistent, bucket "usage_events".
//
// KEY STRATEGY: "<tenantID>:<reverseTimestamp>:<eventID>"
//
// reverseTimestamp = MaxInt64 - event.Timestamp.UnixNano(), dipadding
// zero jadi 20 digit tetap. Ini membuat urutan BYTE ASCENDING pada
// key = urutan WAKTU DESCENDING (terbaru dulu) — bbolt Cursor selalu
// iterasi key dalam urutan byte ascending, jadi dengan trik ini kita
// dapat "terbaru dulu" TANPA perlu load semua event lalu sort di
// memory. Pola ini BARU di repo (belum ada store lain yang butuh
// urutan waktu), jadi didokumentasikan detail di sini.
//
// Prefix "<tenantID>:" dipakai Cursor.Seek untuk lompat langsung ke
// event pertama milik tenant tsb, lalu iterasi berhenti begitu
// prefix tidak lagi cocok (event milik tenant lain / habis).
// ============================================================

var usageEventsBucket = []byte("usage_events")

type BoltEventStore struct {
	db *bolt.DB
}

func NewBoltEventStore(db *bolt.DB) (*BoltEventStore, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(usageEventsBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init usage_events bucket: %w", err)
	}
	return &BoltEventStore{db: db}, nil
}

func reverseTimestampKey(tenantID string, ts time.Time, eventID string) []byte {
	reverse := math.MaxInt64 - ts.UnixNano()
	return []byte(fmt.Sprintf("%s:%020d:%s", tenantID, reverse, eventID))
}

func (s *BoltEventStore) Save(event *UsageEvent) error {
	if event.TenantID == "" {
		return fmt.Errorf("usage event: tenant_id is required")
	}
	if event.ID == "" {
		return fmt.Errorf("usage event: id is required")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	key := reverseTimestampKey(event.TenantID, event.Timestamp, event.ID)

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(usageEventsBucket)
		if b == nil {
			return fmt.Errorf("usage_events bucket not found")
		}
		return b.Put(key, data)
	})
}

func (s *BoltEventStore) ListForTenant(tenantID string, limit int) ([]*UsageEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	prefix := []byte(tenantID + ":")
	events := make([]*UsageEvent, 0, limit)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usageEventsBucket)
		if b == nil {
			return nil
		}
		c := b.Cursor()

		for k, v := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, v = c.Next() {
			if len(events) >= limit {
				break
			}
			var event UsageEvent
			if err := json.Unmarshal(v, &event); err != nil {
				continue
			}
			events = append(events, &event)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return events, nil
}

// CountBillableSinceForTenant menghitung event MCP billable
// (IsBillableEventType true) sejak waktu "since" (inclusive), TANPA
// batas jumlah — dipakai untuk "events billed this month".
//
// Memakai key ordering yang sama (terbaru dulu): iterasi berhenti SEGERA
// begitu ketemu event dengan Timestamp < since, karena semua event
// sesudahnya (lebih lama lagi) pasti juga < since — tidak perlu scan
// seluruh bucket tenant, cukup sampai batas bulan berjalan.
func (s *BoltEventStore) CountBillableSinceForTenant(tenantID string, since time.Time) (int, error) {
	prefix := []byte(tenantID + ":")
	count := 0

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
			if event.Timestamp.Before(since) {
				// Terbaru-dulu: semua event berikutnya juga sebelum
				// "since", aman berhenti di sini.
				break
			}
			if IsBillableEventType(event.EventType) {
				count++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	return count, nil
}