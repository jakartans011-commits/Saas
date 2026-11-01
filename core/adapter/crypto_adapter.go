// core/adapter/crypto_adapter.go
package adapter

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"trailseal/sdk"
)

// CryptoAdapter adalah adapter untuk enkripsi/dekripsi
type CryptoAdapter struct {
	key []byte
}

// NewCryptoAdapter membuat adapter baru dengan key
func NewCryptoAdapter(key []byte) sdk.Crypto {
	if key == nil || len(key) == 0 {
		key = make([]byte, 32)
		rand.Read(key)
	}
	return &CryptoAdapter{
		key: key,
	}
}

// Encrypt mengenkripsi data dengan AES-GCM
func (a *CryptoAdapter) Encrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

// Decrypt mendekripsi data dengan AES-GCM
func (a *CryptoAdapter) Decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// Sign membuat signature dari data
func (a *CryptoAdapter) Sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return hash[:], nil
}

// Verify memverifikasi signature
func (a *CryptoAdapter) Verify(data []byte, signature []byte) bool {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]) == hex.EncodeToString(signature)
}

// Hash menghasilkan hash dari data
func (a *CryptoAdapter) Hash(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

// GenerateKey menghasilkan key baru
func (a *CryptoAdapter) GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}