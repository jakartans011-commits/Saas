package audit

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// BoltAuditStore — persistent, bucket "audit_events".
//
// KEY STRATEGY: "<tenantID>:<reverseTimestamp>:<eventID>"
// Sama seperti usage store, urutan terbaru dulu.
// ============================================================

var auditEventsBucket = []byte("audit_events")

type BoltAuditStore struct {
	db *bolt.DB
}

func NewBoltAuditStore(db *bolt.DB) (*BoltAuditStore, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(auditEventsBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init audit_events bucket: %w", err)
	}
	return &BoltAuditStore{db: db}, nil
}

func reverseTimestampKeyAudit(tenantID string, ts time.Time, eventID string) []byte {
	reverse := math.MaxInt64 - ts.UnixNano()
	return []byte(fmt.Sprintf("%s:%020d:%s", tenantID, reverse, eventID))
}

func (s *BoltAuditStore) Save(event *AuditEvent) error {
	if event.TenantID == "" {
		return fmt.Errorf("audit event: tenant_id is required")
	}
	if event.ID == "" {
		return fmt.Errorf("audit event: id is required")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	key := reverseTimestampKeyAudit(event.TenantID, event.Timestamp, event.ID)

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(auditEventsBucket)
		if b == nil {
			return fmt.Errorf("audit_events bucket not found")
		}
		return b.Put(key, data)
	})
}

func (s *BoltAuditStore) ListForTenant(tenantID string, limit int) ([]*AuditEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	prefix := []byte(tenantID + ":")
	events := make([]*AuditEvent, 0, limit)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(auditEventsBucket)
		if b == nil {
			return nil
		}
		c := b.Cursor()

		for k, v := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, v = c.Next() {
			if len(events) >= limit {
				break
			}
			var event AuditEvent
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

// CleanupForTenant menghapus event yang lebih tua dari policy.RetentionDays,
// dan meng-anonymize (scrub PII, sisakan action/outcome/timestamp untuk
// jejak compliance) event yang lebih tua dari policy.AnonymizationDays
// tapi masih di dalam RetentionDays — untuk SATU tenant saja.
//
// Two-pass (View lalu Update) sengaja dipakai, bukan mutasi langsung di
// dalam cursor loop — supaya aman dari isu undefined behavior bbolt kalau
// insert/delete terjadi di tengah iterasi cursor pada bucket yang sama.
func (s *BoltAuditStore) CleanupForTenant(tenantID string, policy *RetentionPolicy) (deleted int, anonymized int, err error) {
	if tenantID == "" {
		return 0, 0, fmt.Errorf("tenant_id_required")
	}
	if policy == nil {
		return 0, 0, fmt.Errorf("policy_required")
	}

	now := time.Now()
	deleteCutoff := now.AddDate(0, 0, -policy.RetentionDays)
	anonymizeCutoff := now.AddDate(0, 0, -policy.AnonymizationDays)

	prefix := []byte(tenantID + ":")

	type change struct {
		key    []byte
		delete bool
		value  []byte // non-nil kalau ini update (anonymize), nil kalau delete
	}
	var changes []change

	err = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(auditEventsBucket)
		if b == nil {
			return nil
		}
		c := b.Cursor()

		for k, v := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, v = c.Next() {
			var event AuditEvent
			if err := json.Unmarshal(v, &event); err != nil {
				continue
			}

			keyCopy := append([]byte(nil), k...)

			if event.Timestamp.Before(deleteCutoff) {
				changes = append(changes, change{key: keyCopy, delete: true})
				continue
			}
			if event.Timestamp.Before(anonymizeCutoff) && !isAnonymizedEvent(&event) {
				anonymizeEvent(&event)
				data, err := json.Marshal(&event)
				if err != nil {
					continue
				}
				changes = append(changes, change{key: keyCopy, value: data})
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(auditEventsBucket)
		if b == nil {
			return fmt.Errorf("audit_events bucket not found")
		}
		for _, ch := range changes {
			if ch.delete {
				if err := b.Delete(ch.key); err != nil {
					return err
				}
				deleted++
			} else {
				if err := b.Put(ch.key, ch.value); err != nil {
					return err
				}
				anonymized++
			}
		}
		return nil
	})
	if err != nil {
		return deleted, anonymized, err
	}

	return deleted, anonymized, nil
}

// DeleteAllForTenant — dipanggil dari compliance.PurgeTenant saat GDPR
// purge. Menghapus SELURUH audit event tenant tanpa syarat retensi
// (berbeda dari CleanupForTenant yang hormat cutoff tanggal). Pola sama
// dengan ListForTenant/CleanupForTenant: prefix scan lalu delete di
// transaksi terpisah — key di-copy dulu karena cursor tidak valid lagi
// setelah bucket dimutasi dalam transaksi yang sama.
func (s *BoltAuditStore) DeleteAllForTenant(tenantID string) error {
	prefix := []byte(tenantID + ":")
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(auditEventsBucket)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		var keysToDelete [][]byte
		for k, _ := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, _ = c.Next() {
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			keysToDelete = append(keysToDelete, keyCopy)
		}
		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return fmt.Errorf("delete audit event key %s: %w", string(k), err)
			}
		}
		return nil
	})
}

// isAnonymizedEvent cek marker supaya event yang sudah dianonymize tidak
// diproses ulang setiap kali cleanup run berikutnya.
func isAnonymizedEvent(event *AuditEvent) bool {
	return event.Metadata != nil && event.Metadata["anonymized"] == "true"
}

// anonymizeEvent men-scrub field PII (UserID, ip, user_agent, path),
// tetap simpan method/status (tidak identifying) + marker "anonymized".
// action/resource/outcome/timestamp di level struct TIDAK disentuh —
// jejak "apa yang terjadi, kapan, hasilnya apa" tetap ada untuk audit
// trail, hanya "siapa" (identitas) yang dihapus.
func anonymizeEvent(event *AuditEvent) {
	event.UserID = "[anonymized]"

	scrubbed := map[string]string{"anonymized": "true"}
	if event.Metadata != nil {
		if method, ok := event.Metadata["method"]; ok {
			scrubbed["method"] = method
		}
		if status, ok := event.Metadata["status"]; ok {
			scrubbed["status"] = status
		}
		// ip, user_agent, path SENGAJA dibuang — itu PII.
	}
	event.Metadata = scrubbed
}