// sdk/storage.go
package sdk

// Storage adalah interface untuk penyimpanan data
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Storage interface {
	Save(key string, value interface{}) error
	Load(key string, value interface{}) error
	Delete(key string) error
	List(prefix string) ([]string, error)
	Exists(key string) bool
}

// Bucket adalah collection dalam storage
type Bucket interface {
	Save(key string, value interface{}) error
	Load(key string, value interface{}) error
	Delete(key string) error
	List() ([]string, error)
}