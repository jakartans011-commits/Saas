package tenant

import (
	"encoding/json"

	bolt "go.etcd.io/bbolt"
)

// ListDeleted mengembalikan semua tenant yang sudah soft-deleted
// (DeletedAt != nil). Dipakai scheduler purge otomatis (P5-1).
//
// Record yang tidak bisa di-unmarshal dilewati, bukan menggagalkan
// seluruh listing: satu record rusak tidak boleh menghentikan purge
// tenant lain.
func (s *BoltTenantStore) ListDeleted() ([]*Tenant, error) {
	var out []*Tenant

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tenantsBucket)
		return b.ForEach(func(_, v []byte) error {
			var t Tenant
			if err := json.Unmarshal(v, &t); err != nil {
				return nil
			}
			if t.DeletedAt != nil {
				out = append(out, &t)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
