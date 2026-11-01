// sdk/contract.go
package sdk

// ContractVerifier adalah interface untuk contract test gate
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type ContractVerifier interface {
	// VerifyPlugin memverifikasi plugin memenuhi kontrak
	VerifyPlugin(plugin Plugin) ContractTestResult

	// VerifyManifest memverifikasi manifest memenuhi kontrak
	VerifyManifest(manifest Manifest) ContractTestResult

	// VerifySignature memverifikasi signature manifest
	VerifySignature(manifest Manifest, publicKey string) ContractTestResult

	// VerifyAll menjalankan semua verifikasi
	VerifyAll(plugin Plugin, manifest Manifest, publicKey string) ContractTestResult

	// RequiresHighAssurance mengecek apakah plugin butuh high-assurance review
	RequiresHighAssurance(manifest Manifest) bool
}

// ContractTestResult adalah hasil contract test
type ContractTestResult struct {
	Passed      bool     `json:"passed"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	HighAssurance bool   `json:"high_assurance,omitempty"`
}

// HighAssuranceReview adalah hasil high-assurance review
type HighAssuranceReview struct {
	Required    bool     `json:"required"`
	Completed   bool     `json:"completed"`
	ApprovedBy  string   `json:"approved_by,omitempty"`
	Notes       []string `json:"notes,omitempty"`
}