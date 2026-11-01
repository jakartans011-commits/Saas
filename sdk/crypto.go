// sdk/crypto.go
package sdk

// Crypto adalah interface untuk enkripsi/dekripsi
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Crypto interface {
	Encrypt(data []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
	Sign(data []byte) ([]byte, error)
	Verify(data []byte, signature []byte) bool
	Hash(data []byte) []byte
	GenerateKey() ([]byte, error)
}
