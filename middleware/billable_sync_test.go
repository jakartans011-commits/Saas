package middleware

import (
	"testing"

	"trailseal/usage"
)

// TestBillableMethods_SyncedWithUsagePackage — jaga sinkronisasi dua
// definisi billable yang sengaja diduplikasi (usage tidak boleh
// mengimpor middleware, circular import — lihat komentar di
// usage/event.go dan mcp_usage_middleware.go). Test ini gagal duluan
// kalau billableMCPMethods berubah tanpa mengupdate billableEventTypes
// di usage/event.go, sebelum sum(series) diam-diam berhenti cocok
// dengan events_billed (ROADMAP §8, 3C).
func TestBillableMethods_SyncedWithUsagePackage(t *testing.T) {
	for method := range billableMCPMethods {
		if !usage.IsBillableEventType(usage.EventType(method)) {
			t.Errorf("method %q billable di middleware.billableMCPMethods tapi TIDAK di usage.IsBillableEventType — kedua definisi tidak sinkron", method)
		}
	}
}

// TestDiscoveryMethods_NotBillableInEitherPackage — method discovery
// (sengaja tidak ada di billableMCPMethods, lihat komentar di atas
// var-nya) harus konsisten non-billable di kedua package.
func TestDiscoveryMethods_NotBillableInEitherPackage(t *testing.T) {
	discoveryMethods := []string{
		"tools/list",
		"resources/list",
		"prompts/list",
		"server/discover",
		"notifications/initialized",
	}

	for _, method := range discoveryMethods {
		if billableMCPMethods[method] {
			t.Errorf("method %q seharusnya discovery (non-billable) tapi ada di billableMCPMethods", method)
		}
		if usage.IsBillableEventType(usage.EventType(method)) {
			t.Errorf("method %q seharusnya discovery (non-billable) tapi usage.IsBillableEventType true", method)
		}
	}
}
