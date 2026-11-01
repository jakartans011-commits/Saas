// core/manifest/manifest.go
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"trailseal/sdk"
)

// ============================================================
// PROCESSOR — MENGINTEGRASIKAN VALIDATOR + NORMALIZER + ALIAS RESOLVER
// ============================================================

// Processor adalah facade untuk memproses manifest
type Processor struct {
	aliasResolver *AliasResolver
}

// NewProcessor membuat processor baru
func NewProcessor() *Processor {
	return &Processor{
		aliasResolver: NewAliasResolver(),
	}
}

// GetAliasResolver mengembalikan alias resolver (untuk konfigurasi)
func (p *Processor) GetAliasResolver() *AliasResolver {
	return p.aliasResolver
}

// ProcessManifest memproses manifest:
// 1. Validasi structural
// 2. Normalisasi
// 3. Alias resolution
func (p *Processor) ProcessManifest(raw sdk.Manifest) (sdk.Manifest, error) {
	// STEP 1: Validasi structural
	if err := ValidateManifest(raw); err != nil {
		return sdk.Manifest{}, fmt.Errorf("validation failed: %w", err)
	}

	// STEP 2: Normalisasi
	normalized := NormalizeManifest(raw)

	// STEP 3: Alias resolution
	resolved := p.aliasResolver.ResolveAll(normalized)

	return resolved, nil
}

// ============================================================
// SIGNATURE — SUPPLY CHAIN SECURITY
// ============================================================

// SignManifest membuat signature dari manifest
func SignManifest(manifest sdk.Manifest, secret string) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%v|%v",
		manifest.Name,
		manifest.Version,
		manifest.SchemaVersion,
		manifest.Publisher,
		manifest.Capabilities,
		manifest.Permissions,
	)
	hash := sha256.Sum256([]byte(data + secret))
	return hex.EncodeToString(hash[:])
}

// VerifyManifest memverifikasi signature manifest
func VerifyManifest(manifest sdk.Manifest, secret string) bool {
	if manifest.Signature == "" {
		return false // Manifest tanpa signature tidak valid untuk production
	}
	expected := SignManifest(manifest, secret)
	return manifest.Signature == expected
}