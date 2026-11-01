// sdk/capability.go
package sdk

// CapabilityMatcher adalah interface untuk mencocokkan capability
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type CapabilityMatcher interface {
	// Match mengecek apakah capability cocok dengan pattern
	// Contoh: pattern "hardware.camera.*" cocok dengan "hardware.camera.read"
	Match(capability, pattern string) bool

	// MatchAny mengecek apakah ada pattern yang cocok dengan capability
	MatchAny(capability string, patterns []string) bool

	// Filter mengembalikan capability yang cocok dengan pattern
	Filter(capabilities []string, pattern string) []string
}