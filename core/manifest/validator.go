// core/manifest/validator.go
package manifest

import (
	"fmt"
	"strings"

	"trailseal/sdk"
)

// ============================================================
// VALIDATOR — STRUCTURAL-ONLY
// ============================================================

// ValidateCapability melakukan validasi structural capability
func ValidateCapability(capability string) error {
	if capability == "" {
		return fmt.Errorf("capability cannot be empty")
	}

	if strings.Contains(capability, " ") {
		return fmt.Errorf("capability cannot contain spaces: %s", capability)
	}

	segments := strings.Split(capability, ".")
	if len(segments) < 3 {
		return fmt.Errorf("capability must have at least 3 segments (domain.resource.action): %s", capability)
	}

	for _, seg := range segments {
		if seg == "" {
			return fmt.Errorf("capability has empty segment: %s", capability)
		}
		for _, ch := range seg {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
				(ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
				return fmt.Errorf("capability segment contains invalid character: %s (char: %c)", capability, ch)
			}
		}
	}

	return nil
}

// ValidateManifest melakukan validasi structural manifest
// 🔥 PERUBAHAN: Hanya structural, TIDAK validasi permission!
func ValidateManifest(manifest sdk.Manifest) error {
	// Required fields
	if manifest.Name == "" {
		return fmt.Errorf("manifest name is required")
	}
	if manifest.Version == "" {
		return fmt.Errorf("manifest version is required")
	}
	if manifest.SchemaVersion == "" {
		return fmt.Errorf("manifest schema_version is required")
	}

	// Version format (semver)
	if !isValidSemver(manifest.Version) {
		return fmt.Errorf("invalid version format: %s (expected semver)", manifest.Version)
	}
	if !isValidSemver(manifest.SchemaVersion) {
		return fmt.Errorf("invalid schema_version format: %s (expected semver)", manifest.SchemaVersion)
	}

	// Validate capabilities (structural only)
	for _, cap := range manifest.Capabilities {
		if err := ValidateCapability(cap); err != nil {
			return fmt.Errorf("invalid capability %s: %w", cap, err)
		}
	}

	// 🔥 HAPUS VALIDASI PERMISSION DI SINI!
	// Permission divalidasi oleh Permission Engine, BUKAN di sini

	return nil
}

// ============================================================
// NORMALIZER — CASING HANDLING
// ============================================================

// NormalizeCapability menyeragamkan format capability
func NormalizeCapability(capability string) string {
	return strings.ToLower(strings.TrimSpace(capability))
}

// NormalizeManifest menyeragamkan semua field di manifest
func NormalizeManifest(manifest sdk.Manifest) sdk.Manifest {
	normalized := manifest

	for i, cap := range normalized.Capabilities {
		normalized.Capabilities[i] = NormalizeCapability(cap)
	}

	for i, req := range normalized.Requirements {
		normalized.Requirements[i] = strings.ToLower(strings.TrimSpace(req))
	}

	for i, perm := range normalized.Permissions {
		normalized.Permissions[i] = strings.ToLower(strings.TrimSpace(perm))
	}

	return normalized
}

// ============================================================
// ALIAS RESOLVER
// ============================================================

type AliasResolver struct {
	aliases map[string]string
}

func NewAliasResolver() *AliasResolver {
	return &AliasResolver{
		aliases: make(map[string]string),
	}
}

func (r *AliasResolver) RegisterAlias(old, new string) error {
	if err := ValidateCapability(old); err != nil {
		return fmt.Errorf("invalid old capability: %w", err)
	}
	if err := ValidateCapability(new); err != nil {
		return fmt.Errorf("invalid new capability: %w", err)
	}
	r.aliases[old] = new
	return nil
}

func (r *AliasResolver) Resolve(capability string) string {
	if resolved, ok := r.aliases[capability]; ok {
		return resolved
	}
	return capability
}

func (r *AliasResolver) ResolveAll(manifest sdk.Manifest) sdk.Manifest {
	resolved := manifest
	for i, cap := range resolved.Capabilities {
		resolved.Capabilities[i] = r.Resolve(cap)
	}
	return resolved
}

// ============================================================
// HELPER FUNCTIONS
// ============================================================

func isValidSemver(version string) bool {
	if version == "" {
		return false
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}
