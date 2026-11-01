// core/adapter/storage_adapter.go
package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"trailseal/sdk"
)

// StorageAdapter adalah adapter untuk penyimpanan data
// Implementasi nyata dengan file-based storage
type StorageAdapter struct {
	mu   sync.RWMutex
	dir  string
	data map[string]map[string]interface{}
}

// NewStorageAdapter membuat adapter baru dengan direktori storage
func NewStorageAdapter(storageDir string) sdk.Storage {
	if storageDir == "" {
		storageDir = "data"
	}
	os.MkdirAll(storageDir, 0755)

	return &StorageAdapter{
		dir:  storageDir,
		data: make(map[string]map[string]interface{}),
	}
}

// Save menyimpan data ke storage
func (a *StorageAdapter) Save(key string, value interface{}) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Simpan ke memory
	if a.data[key] == nil {
		a.data[key] = make(map[string]interface{})
	}
	a.data[key]["value"] = value

	// Simpan ke file
	return a.saveToFile(key, value)
}

// Load mengambil data dari storage
func (a *StorageAdapter) Load(key string, value interface{}) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Cek memory dulu
	if val, ok := a.data[key]; ok {
		if v, ok := val["value"]; ok {
			data, err := json.Marshal(v)
			if err != nil {
				return err
			}
			return json.Unmarshal(data, value)
		}
	}

	// Cek file
	return a.loadFromFile(key, value)
}

// Delete menghapus data dari storage
func (a *StorageAdapter) Delete(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.data, key)

	// Hapus file
	filePath := filepath.Join(a.dir, key+".json")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List mengembalikan semua key yang tersimpan
func (a *StorageAdapter) List(prefix string) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	keys := make([]string, 0)
	for k := range a.data {
		if prefix == "" || len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Exists mengecek apakah key ada
func (a *StorageAdapter) Exists(key string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.data[key]
	return ok
}

func (a *StorageAdapter) saveToFile(key string, value interface{}) error {
	filePath := filepath.Join(a.dir, key+".json")
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func (a *StorageAdapter) loadFromFile(key string, value interface{}) error {
	filePath := filepath.Join(a.dir, key+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
