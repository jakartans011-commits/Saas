// core/contract/high_assurance.go
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"trailseal/sdk"
)

// HighAssuranceManager mengelola high-assurance review
type HighAssuranceManager struct {
	mu          sync.RWMutex
	approved    map[string]bool      // pluginName -> approved
	reviewNotes map[string][]string  // pluginName -> notes
	publicKey   string
}

// NewHighAssuranceManager membuat high-assurance manager baru
func NewHighAssuranceManager(publicKey string) *HighAssuranceManager {
	return &HighAssuranceManager{
		approved:    make(map[string]bool),
		reviewNotes: make(map[string][]string),
		publicKey:   publicKey,
	}
}

// RequiresHighAssurance mengecek apakah plugin butuh high-assurance review
func (m *HighAssuranceManager) RequiresHighAssurance(manifest sdk.Manifest) bool {
	for _, cap := range manifest.Capabilities {
		if strings.HasPrefix(cap, "security.") {
			return true
		}
	}
	return false
}

// RequestReview meminta high-assurance review
func (m *HighAssuranceManager) RequestReview(pluginName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approved[pluginName] = false
}

// ApproveReview menyetujui high-assurance review
func (m *HighAssuranceManager) ApproveReview(pluginName, approver string, notes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.approved[pluginName]; !exists {
		return fmt.Errorf("plugin %s not in review queue", pluginName)
	}

	m.approved[pluginName] = true
	if notes != nil {
		m.reviewNotes[pluginName] = append(m.reviewNotes[pluginName], notes...)
	}
	m.reviewNotes[pluginName] = append(m.reviewNotes[pluginName],
		fmt.Sprintf("Approved by: %s at %v", approver, time.Now()),
	)

	return nil
}

// IsApproved mengecek apakah plugin sudah di-approve
func (m *HighAssuranceManager) IsApproved(pluginName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	approved, ok := m.approved[pluginName]
	if !ok {
		return false
	}
	return approved
}

// GetReviewNotes mengembalikan review notes
func (m *HighAssuranceManager) GetReviewNotes(pluginName string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reviewNotes[pluginName]
}

// VerifyHighAssuranceSignature memverifikasi high-assurance signature
// security.* capability WAJIB signature dengan key terpisah
func (m *HighAssuranceManager) VerifyHighAssuranceSignature(manifest sdk.Manifest) bool {
	if !m.RequiresHighAssurance(manifest) {
		return true // Tidak butuh high-assurance
	}

	// CEK: Signature tidak kosong
	if manifest.Signature == "" {
		return false
	}

	// CEK: Signature valid dengan high-assurance key
	expected := m.signHighAssurance(manifest)
	return manifest.Signature == expected
}

// signHighAssurance membuat high-assurance signature
func (m *HighAssuranceManager) signHighAssurance(manifest sdk.Manifest) string {
	// High-assurance signature menggunakan key yang berbeda
	data := fmt.Sprintf("HIGH|%s|%s|%s|%s|%v",
		manifest.Name,
		manifest.Version,
		manifest.SchemaVersion,
		manifest.Publisher,
		manifest.Capabilities,
	)
	hash := sha256.Sum256([]byte(data + m.publicKey))
	return hex.EncodeToString(hash[:])
}