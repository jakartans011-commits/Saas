// core/storage.go
package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// ============================================================================
// STORAGE INTERFACE
// ============================================================================

// Storage - interface untuk persistent storage
type Storage interface {
	Save(bucket string, key string, value interface{}) error
	Load(bucket string, key string, value interface{}) error
	Delete(bucket string, key string) error
	List(bucket string) ([]string, error)
	Close() error
}

// ============================================================================
// BOLTDB STORAGE
// ============================================================================

type BoltStorage struct {
	db     *bbolt.DB
	mu     sync.RWMutex
	logger *slog.Logger
	path   string
}

// NewBoltStorage - membuat storage baru dengan path dari config
func NewBoltStorage(path string, logger *slog.Logger) (*BoltStorage, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Buat folder jika belum ada
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage dir: %w", err)
	}

	db, err := bbolt.Open(path, 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	storage := &BoltStorage{
		db:     db,
		logger: logger,
		path:   path,
	}

	// Inisialisasi bucket default
	if err := storage.initBuckets(); err != nil {
		return nil, err
	}

	logger.Info("Storage initialized", "path", path)
	return storage, nil
}

// initBuckets - inisialisasi bucket-bucket yang diperlukan
func (s *BoltStorage) initBuckets() error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		buckets := []string{
			"config",
			"state",
			"beacon",
			"collected",
			"commands",
			"modules",
		}
		for _, bucket := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", bucket, err)
			}
		}
		return nil
	})
}

// Save - simpan data ke bucket
func (s *BoltStorage) Save(bucket string, key string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucket)
		}
		return b.Put([]byte(key), data)
	})
}

// Load - ambil data dari bucket
func (s *BoltStorage) Load(bucket string, key string, value interface{}) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucket)
		}

		data := b.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("key %s not found in bucket %s", key, bucket)
		}

		return json.Unmarshal(data, value)
	})
}

// Delete - hapus data dari bucket
func (s *BoltStorage) Delete(bucket string, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucket)
		}
		return b.Delete([]byte(key))
	})
}

// List - list semua key dalam bucket
func (s *BoltStorage) List(bucket string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []string
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucket)
		}

		cursor := b.Cursor()
		for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
			keys = append(keys, string(k))
		}
		return nil
	})

	return keys, err
}

// Close - tutup database
func (s *BoltStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		s.logger.Info("Closing storage", "path", s.path)
		return s.db.Close()
	}
	return nil
}
