// core/contract/verifier.go
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"trailseal/sdk"
)

// Verifier adalah implementasi ContractVerifier.
type Verifier struct {
	publicKey string
}

// NewVerifier membuat verifier baru.
func NewVerifier(publicKey string) *Verifier {
	return &Verifier{
		publicKey: publicKey,
	}
}

// VerifyPlugin memverifikasi plugin memenuhi kontrak.
//
// Keberadaan Init, Start, Stop, dan Health tidak diperiksa dengan
// perbandingan nil karena keempatnya merupakan method yang diwajibkan
// oleh interface sdk.Plugin. Jika nilai plugin berhasil diterima sebagai
// sdk.Plugin, method-method tersebut sudah dijamin oleh kontrak interface.
func (v *Verifier) VerifyPlugin(plugin sdk.Plugin) sdk.ContractTestResult {
	result := sdk.ContractTestResult{
		Passed: true,
		Errors: []string{},
	}

	if plugin == nil {
		result.Passed = false
		result.Errors = append(result.Errors, "plugin is nil")
		return result
	}

	if plugin.Name() == "" {
		result.Passed = false
		result.Errors = append(result.Errors, "plugin.Name() is empty")
	}

	if plugin.Version() == "" {
		result.Passed = false
		result.Errors = append(result.Errors, "plugin.Version() is empty")
	}

	manifest := plugin.Manifest()

	if manifest.Name == "" {
		result.Passed = false
		result.Errors = append(result.Errors, "manifest.Name is empty")
	}

	if manifest.Version == "" {
		result.Passed = false
		result.Errors = append(result.Errors, "manifest.Version is empty")
	}

	if manifest.SchemaVersion == "" {
		result.Passed = false
		result.Errors = append(result.Errors, "manifest.SchemaVersion is empty")
	}

	return result
}

// VerifyManifest memverifikasi manifest memenuhi kontrak.
//
// Hanya melakukan structural validation.
// Permission tidak divalidasi di Contract Test Gate;
// permission ditangani oleh Permission Engine.
func (v *Verifier) VerifyManifest(
	manifest sdk.Manifest,
) sdk.ContractTestResult {
	result := sdk.ContractTestResult{
		Passed: true,
		Errors: []string{},
	}

	// Required fields.
	if manifest.Name == "" {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			"manifest.Name is required",
		)
	}

	if manifest.Version == "" {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			"manifest.Version is required",
		)
	}

	if manifest.SchemaVersion == "" {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			"manifest.SchemaVersion is required",
		)
	}

	// Semantic version format.
	if manifest.Version != "" && !isValidSemver(manifest.Version) {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			fmt.Sprintf(
				"manifest.Version '%s' is not valid semver",
				manifest.Version,
			),
		)
	}

	if manifest.SchemaVersion != "" && !isValidSemver(manifest.SchemaVersion) {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			fmt.Sprintf(
				"manifest.SchemaVersion '%s' is not valid semver",
				manifest.SchemaVersion,
			),
		)
	}

	// Permission sengaja tidak divalidasi di sini.
	// Permission divalidasi oleh Permission Engine.

	// Capability format: minimal 3 segments.
	for _, capability := range manifest.Capabilities {
		segments := strings.Split(capability, ".")

		if len(segments) < 3 {
			result.Passed = false
			result.Errors = append(
				result.Errors,
				fmt.Sprintf(
					"capability '%s' must have at least 3 segments",
					capability,
				),
			)
		}

		if strings.Contains(capability, " ") {
			result.Passed = false
			result.Errors = append(
				result.Errors,
				fmt.Sprintf(
					"capability '%s' cannot contain spaces",
					capability,
				),
			)
		}

		for _, segment := range segments {
			if segment == "" {
				result.Passed = false
				result.Errors = append(
					result.Errors,
					fmt.Sprintf(
						"capability '%s' has empty segment",
						capability,
					),
				)
			}
		}
	}

	return result
}

// VerifySignature memverifikasi signature manifest.
func (v *Verifier) VerifySignature(
	manifest sdk.Manifest,
	publicKey string,
) sdk.ContractTestResult {
	result := sdk.ContractTestResult{
		Passed: true,
		Errors: []string{},
	}

	if publicKey == "" {
		publicKey = v.publicKey
	}

	if manifest.Signature == "" {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			"manifest.Signature is empty",
		)
		return result
	}

	expected := v.signManifest(manifest, publicKey)

	if manifest.Signature != expected {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			"manifest.Signature is invalid",
		)
		return result
	}

	return result
}

// VerifyAll menjalankan semua verifikasi.
func (v *Verifier) VerifyAll(
	plugin sdk.Plugin,
	manifest sdk.Manifest,
	publicKey string,
) sdk.ContractTestResult {
	result := sdk.ContractTestResult{
		Passed: true,
		Errors: []string{},
	}

	pluginResult := v.VerifyPlugin(plugin)

	if !pluginResult.Passed {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			pluginResult.Errors...,
		)
	}

	manifestResult := v.VerifyManifest(manifest)

	if !manifestResult.Passed {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			manifestResult.Errors...,
		)
	}

	sigResult := v.VerifySignature(
		manifest,
		publicKey,
	)

	if !sigResult.Passed {
		result.Passed = false
		result.Errors = append(
			result.Errors,
			sigResult.Errors...,
		)
	}

	if v.RequiresHighAssurance(manifest) {
		result.HighAssurance = true
	}

	return result
}

// RequiresHighAssurance mengecek apakah plugin membutuhkan
// high-assurance review.
func (v *Verifier) RequiresHighAssurance(
	manifest sdk.Manifest,
) bool {
	for _, capability := range manifest.Capabilities {
		if strings.HasPrefix(capability, "security.") {
			return true
		}
	}

	return false
}

// signManifest membuat signature deterministik untuk manifest.
func (v *Verifier) signManifest(
	manifest sdk.Manifest,
	secret string,
) string {
	data := fmt.Sprintf(
		"%s|%s|%s|%s|%v|%v",
		manifest.Name,
		manifest.Version,
		manifest.SchemaVersion,
		manifest.Publisher,
		manifest.Capabilities,
		manifest.Permissions,
	)

	hash := sha256.Sum256(
		[]byte(data + secret),
	)

	return hex.EncodeToString(hash[:])
}

// isValidSemver melakukan validasi sederhana terhadap format
// semantic version X.Y.Z.
//
// Implementasi ini sengaja mempertahankan kontrak yang digunakan
// verifier saat ini: tiga segmen numerik tanpa prefix/suffix.
func isValidSemver(version string) bool {
	if version == "" {
		return false
	}

	parts := strings.Split(version, ".")

	if len(parts) != 3 {
		return false
	}

	for _, part := range parts {
		if part == "" {
			return false
		}

		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}

	return true
}
