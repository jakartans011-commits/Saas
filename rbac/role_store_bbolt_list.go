package rbac

import (
	"bytes"
	"encoding/json"
	"sort"

	bolt "go.etcd.io/bbolt"
)

// ListRolesForTenant mengembalikan semua role milik satu tenant, terurut by ID.
// Sengaja BUKAN bagian interface RoleStore (memecahkan mock); dipakai lewat
// interface kecil di cmd/saas-api.
func (s *BoltRoleStore) ListRolesForTenant(tenantID string) ([]*Role, error) {
	var out []*Role
	prefix := []byte(tenantID + ":")

	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(rolesBucket).Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var role Role
			if err := json.Unmarshal(v, &role); err != nil {
				continue
			}
			if role.TenantID != tenantID {
				continue
			}
			r := role
			out = append(out, &r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}