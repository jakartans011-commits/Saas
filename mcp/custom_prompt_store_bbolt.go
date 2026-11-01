package mcp

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ============================================================
// BoltCustomPromptStore — persistent store untuk custom prompt
// tenant (Business tier). Struktur dan key format SENGAJA mengikuti
// rbac/role_store_bbolt.go (BoltRoleStore) persis:
//
//   - Key = "<tenantID>:<name>", bukan hanya "<name>" — supaya makna
//     "prompt tidak ditemukan DI TENANT INI" (bukan "prompt itu tidak
//     pernah ada sama sekali") tetap terjaga. Ini pola yang sama yang
//     mencegah role_id collision bug di rbac/role.go.
//   - Kumpulkan key dulu sebelum delete saat iterasi cursor
//     (DeleteAllForTenant) — hapus-sambil-iterate di bbolt cursor
//     adalah undefined behavior, persis catatan di BoltRoleStore.
// ============================================================

var customPromptsBucket = []byte("custom_prompts")

// storedCustomPrompt adalah bentuk penyimpanan (bukan wire format MCP).
// MCPPromptDefinition sengaja punya json:"-" di TenantID/Template/
// Source/CreatedAt karena field itu tidak boleh terekspos ke client
// MCP (prompts/list, prompts/get) — tapi field itu justru WAJIB
// disimpan. Struct terpisah ini mencegah kebutuhan wire-format dan
// storage-format saling membatasi satu sama lain di masa depan.
type storedCustomPrompt struct {
	Name        string              `json:"name"`
	TenantID    string              `json:"tenant_id"`
	Description string              `json:"description"`
	Arguments   []MCPPromptArgument `json:"arguments"`
	Template    string              `json:"template"`
	CreatedAt   time.Time           `json:"created_at"`
}

func toStoredCustomPrompt(p MCPPromptDefinition) storedCustomPrompt {
	return storedCustomPrompt{
		Name:        p.Name,
		TenantID:    p.TenantID,
		Description: p.Description,
		Arguments:   p.Arguments,
		Template:    p.Template,
		CreatedAt:   p.CreatedAt,
	}
}

func (s storedCustomPrompt) toDefinition() MCPPromptDefinition {
	return MCPPromptDefinition{
		Name:        s.Name,
		TenantID:    s.TenantID,
		Description: s.Description,
		Arguments:   s.Arguments,
		Template:    s.Template,
		Source:      PromptSourceCustom,
		CreatedAt:   s.CreatedAt,
	}
}

func customPromptKey(tenantID, name string) string {
	return tenantID + ":" + name
}

type BoltCustomPromptStore struct {
	db *bolt.DB
}

// NewBoltCustomPromptStore membuka/membuat bucket "custom_prompts".
func NewBoltCustomPromptStore(db *bolt.DB) (*BoltCustomPromptStore, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(customPromptsBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init custom_prompts bucket: %w", err)
	}
	return &BoltCustomPromptStore{db: db}, nil
}

func (s *BoltCustomPromptStore) Put(prompt MCPPromptDefinition) error {
	if prompt.TenantID == "" {
		return fmt.Errorf(
			"custom prompt %s: tenant_id is required, cannot store global custom prompt",
			prompt.Name,
		)
	}
	data, err := json.Marshal(toStoredCustomPrompt(prompt))
	if err != nil {
		return err
	}
	key := []byte(customPromptKey(prompt.TenantID, prompt.Name))
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(customPromptsBucket)
		return b.Put(key, data)
	})
}

func (s *BoltCustomPromptStore) Get(tenantID, name string) (MCPPromptDefinition, error) {
	var stored storedCustomPrompt
	found := false

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(customPromptsBucket)
		data := b.Get([]byte(customPromptKey(tenantID, name)))
		if data == nil {
			return nil // found tetap false — TIDAK fallback ke tenant lain
		}
		found = true
		return json.Unmarshal(data, &stored)
	})
	if err != nil {
		return MCPPromptDefinition{}, err
	}
	if !found {
		return MCPPromptDefinition{}, fmt.Errorf("prompt not found: %s", name)
	}
	return stored.toDefinition(), nil
}

func (s *BoltCustomPromptStore) ListForTenant(tenantID string) []MCPPromptDefinition {
	var results []MCPPromptDefinition

	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(customPromptsBucket)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var stored storedCustomPrompt
			if err := json.Unmarshal(v, &stored); err != nil {
				continue
			}
			if stored.TenantID == tenantID {
				results = append(results, stored.toDefinition())
			}
		}
		return nil
	})

	return results
}

func (s *BoltCustomPromptStore) DeleteAllForTenant(tenantID string) int {
	deleted := 0

	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(customPromptsBucket)
		c := b.Cursor()

		// Kumpulkan key dulu, hapus setelah iterasi selesai — sama
		// seperti BoltRoleStore.DeleteAllForTenant.
		var keysToDelete [][]byte
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var stored storedCustomPrompt
			if err := json.Unmarshal(v, &stored); err != nil {
				continue
			}
			if stored.TenantID == tenantID {
				keyCopy := make([]byte, len(k))
				copy(keyCopy, k)
				keysToDelete = append(keysToDelete, keyCopy)
			}
		}

		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
			deleted++
		}
		return nil
	})

	return deleted
}

// Delete menghapus satu custom prompt milik tenant tertentu.
// Tidak error kalau key tidak ditemukan (bolt Delete pada key yang
// tidak ada adalah no-op aman) — pengecekan "harus ada dulu sebelum
// dihapus" sengaja diletakkan di layer registry (PromptRegistry),
// bukan di sini, supaya store layer ini tetap primitif murni tanpa
// business logic, konsisten dengan Put.
func (s *BoltCustomPromptStore) Delete(tenantID, name string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(customPromptsBucket)
		return b.Delete([]byte(customPromptKey(tenantID, name)))
	})
}
