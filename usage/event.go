// usage/event.go
package usage

import "time"

// ============================================================
// USAGE EVENT — raw event log (bukan counter agregat), sesuai
// keputusan: lebih detail, bisa di-query ulang untuk kebutuhan
// Quota/Plans/Billing di fase berikutnya.
// ============================================================

type EventType string

const (
	// EventTypeAPIRequest dicatat otomatis oleh
	// middleware.MeterAPIRequest untuk setiap request yang lolos
	// RequireTenantScope (lihat middleware/usage_middleware.go).
	EventTypeAPIRequest EventType = "api_request"

	// EventTypeModuleExecution BELUM TERHUBUNG ke pemanggil manapun
	// di fase ini — belum ada endpoint module execution di
	// cmd/saas-api (Fase 5 "Modul produksi nyata" belum dikerjakan).
	// Konstanta dan RecordEvent() sudah siap dipakai supaya saat
	// module endpoint dibangun nanti, tidak perlu desain ulang
	// skema usage event dari nol.
	EventTypeModuleExecution EventType = "module_execution"
)

// UsageEvent — satu baris kejadian usage.
type UsageEvent struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	EventType EventType         `json:"event_type"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// billableEventTypes — method MCP yang dihitung sebagai "event billed",
// SAMA PERSIS dengan billableMCPMethods di middleware/mcp_usage_middleware.go
// (tools/call, resources/read, prompts/get). Duplikasi sengaja: package usage
// tidak boleh import middleware (circular), jadi daftar ini didefinisikan
// ulang di sini. Kalau billableMCPMethods di middleware berubah, update juga
// di sini — lihat API-CONTRACTS.md "Usage" untuk catatan sinkronisasi ini.
//
// Event-event ini SUDAH DIJAMIN sukses saat masuk store: MCPUsageMetering
// hanya memanggil RecordEvent untuk request yang lolos tanpa JSON-RPC error
// (lihat middleware/mcp_usage_middleware.go). Jadi "billable" dan "berhasil"
// di sini adalah hal yang sama — tidak perlu field outcome terpisah seperti
// di package audit.
var billableEventTypes = map[EventType]bool{
	EventType("tools/call"):     true,
	EventType("resources/read"): true,
	EventType("prompts/get"):    true,
}

// IsBillableEventType — dipakai handler untuk filter count. Event bertipe
// api_request (dicatat UsageMetering, dashboard) atau module_execution
// TIDAK dihitung sebagai "billed" — hanya MCP Gateway usage yang ditagih,
// sesuai keputusan pricing-and-mcp-decisions-v2.md §4.1.
func IsBillableEventType(t EventType) bool {
	return billableEventTypes[t]
}

// EventStore — interface minimal, pola sama seperti TenantStore/
// UserStore: kecil dulu, method tambahan (mis. admin/global list)
// ditambah lewat interface terpisah di file lain kalau dibutuhkan,
// BUKAN mengubah interface ini setelah ada implementasi yang lolos
// test.
type EventStore interface {
	// Save menyimpan satu event. TIDAK melakukan validasi bisnis
	// (mis. apakah tenant benar-benar ada) — itu tanggung jawab
	// caller (middleware/handler), konsisten dengan pola store lain
	// di repo ini (mis. BoltUserStore.Put tidak cek tenant exists).
	Save(event *UsageEvent) error

	// ListForTenant mengembalikan event milik satu tenant, URUTAN
	// TERBARU DULU (descending by Timestamp), dibatasi limit.
	// limit <= 0 diperlakukan sebagai "gunakan default caller".
	ListForTenant(tenantID string, limit int) ([]*UsageEvent, error)

	// CountBillableSinceForTenant menghitung event MCP billable
	// (IsBillableEventType true) milik tenant sejak waktu "since"
	// (inclusive), TANPA batas limit — dipakai untuk "events billed
	// this month" (PRD §2 poin 2). Event non-billable (api_request dkk)
	// tidak ikut dihitung. Ditambahkan 2026-09-14, lihat
	// API-CONTRACTS.md "Usage" untuk konteks.
	CountBillableSinceForTenant(tenantID string, since time.Time) (int, error)
}
