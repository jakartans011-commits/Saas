package audit

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// AUDIT RETENTION — Policy, cleanup lifecycle
//
// PERBAIKAN (Phase 9): sebelumnya RetentionStore menyimpan 1 policy
// GLOBAL untuk seluruh sistem (key tetap "policy", tanpa tenantID sama
// sekali) — itu bug arsitektur, bukan keputusan desain. Retensi WAJIB
// per-tenant karena §1 pricing menjanjikan 14/180/365 hari beda per plan.
// Diperbaiki: key sekarang "<tenantID>:policy".
//
// KEPUTUSAN PRODUK (dikonfirmasi): RetentionDays SELALU mengikuti plan
// tenant (via planRetentionDays yang di-pass caller, sumbernya sama
// dengan project.PlanResolver yang sudah dipakai project routes) — bukan
// nilai independen yang bisa divergen dari apa yang dijanjikan pricing.
// Tenant boleh PERSINGKAT retention (turun dari plan ceiling), tidak
// boleh memperpanjang melebihi kapasitas yang dibayar.
// AnonymizationDays TETAP tenant-configurable penuh (tidak dijanjikan
// beda per tier di pricing, aman jadi satu-satunya knob self-service).
// ============================================================

const (
	// DefaultRetentionDays is the default retention period in days
	DefaultRetentionDays = 365

	// DefaultAnonymizationDays is the default anonymization period in days
	DefaultAnonymizationDays = 30

	// DefaultCleanupInterval is the default cleanup interval in hours
	DefaultCleanupInterval = 24 * time.Hour
)

// RetentionPolicy defines how long audit logs are kept
type RetentionPolicy struct {
	// RetentionDays is how long audit logs are kept before deletion
	RetentionDays int `json:"retention_days"`
	// AnonymizationDays is how long before PII is anonymized
	AnonymizationDays int `json:"anonymization_days"`
	// CleanupInterval is how often cleanup runs
	CleanupInterval time.Duration `json:"cleanup_interval"`
}

// DefaultRetentionPolicy returns default policy
func DefaultRetentionPolicy() *RetentionPolicy {
	return &RetentionPolicy{
		RetentionDays:     DefaultRetentionDays,
		AnonymizationDays: DefaultAnonymizationDays,
		CleanupInterval:   DefaultCleanupInterval,
	}
}

// RetentionStore manages retention policy storage, per tenant.
type RetentionStore struct {
	db *bolt.DB
}

var retentionBucket = []byte("audit_retention")

func retentionPolicyKey(tenantID string) []byte {
	return []byte(tenantID + ":policy")
}

// NewRetentionStore creates a new retention store
func NewRetentionStore(db *bolt.DB) (*RetentionStore, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(retentionBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init retention bucket: %w", err)
	}
	return &RetentionStore{db: db}, nil
}

// GetPolicy returns the effective retention policy for one tenant.
//
// planRetentionDays adalah ceiling dari plan tenant (Free=14/Pro=180/
// Business=365) — dipakai sebagai default RetentionDays kalau tenant
// belum pernah override, DAN sebagai clamp kalau override lama sekarang
// melebihi plan (mis. tenant downgrade setelah pernah set retention
// tinggi saat masih plan atas — jangan biarkan override lama "menyandera"
// retensi lebih lama dari yang dibayar sekarang).
func (s *RetentionStore) GetPolicy(tenantID string, planRetentionDays int) (*RetentionPolicy, error) {
	if planRetentionDays <= 0 {
		planRetentionDays = DefaultRetentionDays
	}

	var policy RetentionPolicy
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(retentionBucket)
		if b == nil {
			return nil
		}
		data := b.Get(retentionPolicyKey(tenantID))
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &policy)
	})
	if err != nil {
		return nil, err
	}

	if !found {
		return &RetentionPolicy{
			RetentionDays:     planRetentionDays,
			AnonymizationDays: DefaultAnonymizationDays,
			CleanupInterval:   DefaultCleanupInterval,
		}, nil
	}

	if policy.RetentionDays <= 0 || policy.RetentionDays > planRetentionDays {
		policy.RetentionDays = planRetentionDays
	}
	if policy.AnonymizationDays <= 0 {
		policy.AnonymizationDays = DefaultAnonymizationDays
	}
	if policy.CleanupInterval <= 0 {
		policy.CleanupInterval = DefaultCleanupInterval
	}
	return &policy, nil
}

// SetPolicy stores a tenant's override.
//
// RetentionDays wajib > 0 dan <= planRetentionDays (tenant boleh
// persingkat, tidak boleh melebihi kapasitas plan). AnonymizationDays
// wajib > 0 dan <= RetentionDays efektif.
func (s *RetentionStore) SetPolicy(tenantID string, policy *RetentionPolicy, planRetentionDays int) error {
	if tenantID == "" {
		return fmt.Errorf("tenant_id_required")
	}
	if planRetentionDays <= 0 {
		planRetentionDays = DefaultRetentionDays
	}
	if policy == nil {
		policy = &RetentionPolicy{
			RetentionDays:     planRetentionDays,
			AnonymizationDays: DefaultAnonymizationDays,
		}
	}

	if policy.RetentionDays <= 0 {
		policy.RetentionDays = planRetentionDays
	}
	if policy.RetentionDays > planRetentionDays {
		return fmt.Errorf("retention_days_exceeds_plan: plan mengizinkan maksimal %d hari", planRetentionDays)
	}
	if policy.AnonymizationDays <= 0 {
		policy.AnonymizationDays = DefaultAnonymizationDays
	}
	if policy.AnonymizationDays > policy.RetentionDays {
		return fmt.Errorf("anonymization_days_exceeds_retention_days")
	}
	policy.CleanupInterval = DefaultCleanupInterval

	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(retentionBucket)
		if b == nil {
			return fmt.Errorf("retention bucket not found")
		}
		return b.Put(retentionPolicyKey(tenantID), data)
	})
}
