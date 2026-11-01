// core/capability/matcher.go
package capability

import (
	"strings"

	"trailseal/sdk"
)

// WildcardMatcher adalah implementasi CapabilityMatcher dengan wildcard
// Mendukung:
// - "*" untuk match semua
// - "domain.*" untuk match semua di domain
// - "domain.resource.*" untuk match semua action di resource
// - "domain.resource.action" untuk match exact
type WildcardMatcher struct{}

// NewWildcardMatcher membuat wildcard matcher baru
func NewWildcardMatcher() sdk.CapabilityMatcher {
	return &WildcardMatcher{}
}

// Match mengecek apakah capability cocok dengan pattern
func (m *WildcardMatcher) Match(capability, pattern string) bool {
	// Normalize dulu (lowercase)
	capability = strings.ToLower(strings.TrimSpace(capability))
	pattern = strings.ToLower(strings.TrimSpace(pattern))

	// Empty pattern selalu match (tidak ada batasan)
	if pattern == "" {
		return true
	}

	// Empty capability tidak pernah match
	if capability == "" {
		return false
	}

	// Exact match
	if capability == pattern {
		return true
	}

	// Pattern "*" match semua
	if pattern == "*" {
		return true
	}

	// Wildcard matching
	capSegments := strings.Split(capability, ".")
	patSegments := strings.Split(pattern, ".")

	// Jika pattern memiliki lebih banyak segments dari capability, false
	if len(patSegments) > len(capSegments) {
		return false
	}

	// Cek setiap segment
	for i, patSeg := range patSegments {
		if patSeg == "*" {
			// * match semua sisa segments
			return true
		}
		if i >= len(capSegments) {
			return false
		}
		if patSeg != capSegments[i] {
			return false
		}
	}

	// Jika pattern lebih pendek atau sama, dan semua segmen match
	return true
}

// MatchAny mengecek apakah ada pattern yang cocok dengan capability
func (m *WildcardMatcher) MatchAny(capability string, patterns []string) bool {
	for _, pattern := range patterns {
		if m.Match(capability, pattern) {
			return true
		}
	}
	return false
}

// Filter mengembalikan capability yang cocok dengan pattern
func (m *WildcardMatcher) Filter(capabilities []string, pattern string) []string {
	result := []string{}
	for _, cap := range capabilities {
		if m.Match(cap, pattern) {
			result = append(result, cap)
		}
	}
	return result
}
