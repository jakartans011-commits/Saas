package tenant

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// BoltTenantStore — persistent, menggantikan InMemoryTenantStore.
// Memenuhi TenantStore interface — bisa dipasang di
// ProvisionTenant/DeprovisionTenant tanpa mengubah kode di sana.
// ============================================================

var tenantsBucket = []byte("tenants")

type BoltTenantStore struct {
	db *bolt.DB
}

func NewBoltTenantStore(db *bolt.DB) (*BoltTenantStore, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(tenantsBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init tenants bucket: %w", err)
	}
	return &BoltTenantStore{db: db}, nil
}

func (s *BoltTenantStore) Put(t *Tenant) error {
	if t.ID == "" {
		return errors.New("tenant id is required")
	}
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tenantsBucket)
		return b.Put([]byte(t.ID), data)
	})
}

// Get returns tenant only if not soft-deleted
func (s *BoltTenantStore) Get(tenantID string) (*Tenant, error) {
	t, err := s.GetIncludingDeleted(tenantID)
	if err != nil {
		return nil, err
	}
	if t.DeletedAt != nil {
		return nil, errors.New("tenant_not_found")
	}
	return t, nil
}

// FindByStripeCustomerID mencari tenant berdasarkan Stripe Customer ID.
// Tenant yang sudah soft-deleted tidak dikembalikan.
func (s *BoltTenantStore) FindByStripeCustomerID(customerID string) (*Tenant, error) {
	if customerID == "" {
		return nil, errors.New("customer_id is required")
	}

	var foundTenant *Tenant

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tenantsBucket)
		if b == nil {
			return errors.New("tenants bucket not found")
		}

		return b.ForEach(func(_, data []byte) error {
			if data == nil {
				return nil
			}

			var t Tenant
			if err := json.Unmarshal(data, &t); err != nil {
				return fmt.Errorf("failed to decode tenant: %w", err)
			}

			if t.StripeCustomerID == customerID && t.DeletedAt == nil {
				// Copy ke variable baru supaya pointer tidak mengacu
				// ke data yang berubah selama iterasi.
				foundTenant = &t
				return nil
			}

			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	if foundTenant == nil {
		return nil, errors.New("tenant not found by stripe customer id")
	}

	return foundTenant, nil
}

// GetIncludingDeleted returns tenant even if soft-deleted
// Used for restore and purge operations
func (s *BoltTenantStore) GetIncludingDeleted(tenantID string) (*Tenant, error) {
	var t Tenant
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tenantsBucket)
		data := b.Get([]byte(tenantID))
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &t)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("tenant_not_found")
	}
	return &t, nil
}

// Delete hard-deletes a tenant (used by Purge)
func (s *BoltTenantStore) Delete(tenantID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tenantsBucket)
		return b.Delete([]byte(tenantID))
	})
}

// PutIfNotExists: check-dan-write dalam SATU db.Update transaction.
// bbolt menyerialisasi semua writer (hanya 1 writable tx berjalan
// bersamaan), jadi ini atomic tanpa mutex tambahan.
func (s *BoltTenantStore) PutIfNotExists(t *Tenant) error {
	if t.ID == "" {
		return errors.New("tenant id is required")
	}
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tenantsBucket)
		if existing := b.Get([]byte(t.ID)); existing != nil {
			var e Tenant
			if err := json.Unmarshal(existing, &e); err != nil {
				return err
			}
			if e.DeletedAt == nil {
				return ErrTenantAlreadyExists
			}
		}
		return b.Put([]byte(t.ID), data)
	})
}

func (s *BoltTenantStore) ListExpiredGracePeriods(now time.Time) ([]*Tenant, error) {
	var result []*Tenant
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tenantsBucket)
		return b.ForEach(func(_, data []byte) error {
			var t Tenant
			if err := json.Unmarshal(data, &t); err != nil {
				return err
			}
			if t.DeletedAt != nil || t.SubscriptionStatus != "past_due" {
				return nil
			}
			if t.GracePeriodDeadline != nil && now.After(*t.GracePeriodDeadline) {
				tCopy := t
				result = append(result, &tCopy)
			}
			return nil
		})
	})
	return result, err
}

// TryConsumeQuota — atomic check-and-increment.
//
// Aman dari race condition MURNI karena mekanisme bbolt: hanya ADA
// SATU writable transaction (db.Update) yang boleh berjalan dalam
// satu waktu — bbolt menyerialisasi semua writer secara otomatis.
// Read (Get) + compare (limit check) + write (Put) yang terjadi DI
// DALAM satu closure db.Update ini setara dengan "SELECT ... FOR
// UPDATE" di Postgres — tidak ada request lain yang bisa menyelinap
// di antara cek dan tulis.
func (s *BoltTenantStore) TryConsumeQuota(tenantID string, amount int64) (bool, int64, error) {
	var allowed bool
	var remaining int64

	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tenantsBucket)

		data := b.Get([]byte(tenantID))
		if data == nil {
			return ErrTenantNotFoundForQuota
		}

		var t Tenant
		if err := json.Unmarshal(data, &t); err != nil {
			return err
		}
		if t.DeletedAt != nil {
			return ErrTenantNotFoundForQuota
		}

		now := time.Now()
		if t.PeriodResetAt == nil || now.After(*t.PeriodResetAt) {
			t.CurrentEventCount = 0
			next := now.Add(QuotaPeriod)
			t.PeriodResetAt = &next
		}

		limit := t.MonthlyEventLimit
		if limit <= 0 {
			limit = DefaultMonthlyEventLimit
		}

		if t.CurrentEventCount+amount > limit {
			allowed = false
			remaining = limit - t.CurrentEventCount
			if remaining < 0 {
				remaining = 0
			}
			// Tetap simpan (untuk persist reset periode kalau baru
			// direset di atas), TAPI counter TIDAK bertambah.
			newData, err := json.Marshal(&t)
			if err != nil {
				return err
			}
			return b.Put([]byte(tenantID), newData)
		}

		t.CurrentEventCount += amount
		allowed = true
		remaining = limit - t.CurrentEventCount

		newData, err := json.Marshal(&t)
		if err != nil {
			return err
		}
		return b.Put([]byte(tenantID), newData)
	})
	if err != nil {
		return false, 0, err
	}
	return allowed, remaining, nil
}
