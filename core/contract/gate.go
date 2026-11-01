// core/contract/gate.go
package contract

import (
	"trailseal/sdk"
)

// ContractGate adalah gerbang contract test
type ContractGate struct {
	verifier      *Verifier
	highAssurance *HighAssuranceManager
}

// NewContractGate membuat contract gate baru
func NewContractGate(publicKey string) *ContractGate {
	return &ContractGate{
		verifier:      NewVerifier(publicKey),
		highAssurance: NewHighAssuranceManager(publicKey),
	}
}

// VerifyPlugin memverifikasi plugin melalui contract gate
func (g *ContractGate) VerifyPlugin(plugin sdk.Plugin) sdk.ContractTestResult {
	manifest := plugin.Manifest()

	// 1. Verifikasi kontrak
	result := g.verifier.VerifyAll(plugin, manifest, "")

	// 2. Jika gagal, return langsung
	if !result.Passed {
		return result
	}

	// 3. High-Assurance Review Gate
	if g.verifier.RequiresHighAssurance(manifest) {
		result.HighAssurance = true

		// CEK: Signature high-assurance
		if !g.highAssurance.VerifyHighAssuranceSignature(manifest) {
			result.Passed = false
			result.Errors = append(result.Errors,
				"High-assurance signature invalid for security.* capability")
			return result
		}

		// CEK: Review approval
		if !g.highAssurance.IsApproved(manifest.Name) {
			result.Passed = false
			result.Errors = append(result.Errors,
				"High-assurance review required for security.* capability")
			return result
		}
	}

	return result
}

// GetVerifier mengembalikan verifier
func (g *ContractGate) GetVerifier() *Verifier {
	return g.verifier
}

// GetHighAssuranceManager mengembalikan high-assurance manager
func (g *ContractGate) GetHighAssuranceManager() *HighAssuranceManager {
	return g.highAssurance
}
