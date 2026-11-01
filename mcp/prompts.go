package mcp

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ============================================================
// MCP PROMPTS
//
// Fondasi extensible: satu PromptRegistry untuk built-in + custom
// prompt (keputusan produk 2026-09-15, lihat prompt-bangun-
// prompts-feature (2).md / ROADMAP.md / API-CONTRACTS.md "Prompts
// (MCP)"). Built-in prompt di-seed ke registry yang sama saat
// startup, ditandai lewat field Source — supaya custom prompt
// tenant (Business tier, dibuka nanti) tidak perlu migrasi skema.
//
// ============================================================
// STATUS SESI INI — BACA SEBELUM LANJUTKAN
//
// SUDAH DIBANGUN (sesi ini):
//   - Skema MCPPromptDefinition final (TenantID/Template/Source/
//     CreatedAt tidak diekspos ke client MCP, sesuai spec).
//   - PromptRegistry (struct konkret, BUKAN interface baru — sesuai
//     larangan bug pattern #9) dengan builtin map, di-seed 3 prompt.
//   - Render Template -> messages di prompts/get dengan argumen yang
//     dikirim client.
//   - Custom prompt store (BoltCustomPromptStore, lihat
//     custom_prompt_store.go / custom_prompt_store_bbolt.go) — mengikuti
//     PERSIS pola key/isolasi BoltRoleStore ("<tenantID>:<name>").
//     PromptRegistry.custom disuntik lewat SetCustomStore (setter
//     opsional pasca-construct, pola SAMA dengan
//     mcpGateway.SetQuotaEnforcer di app.go) — nil-safe, jadi tidak
//     mengubah signature NewServer/NewPromptRegistry dan tidak
//     mematahkan 20+ call site `NewServer(gateway)` di test lama.
//   - ListForTenant/Get sekarang benar-benar menggabungkan built-in +
//     custom milik tenant tersebut.
//   - prompts/create (JSON-RPC, non-standar — lihat keputusan di
//     server.go) untuk membuat custom prompt, dengan gate Owner-only +
//     tier Business, via header X-Role-ID/X-Plan-Tier (lihat catatan
//     di server.go — KEPUTUSAN PRODUK yang perlu dikonfirmasi user).
//   - prompts/delete, prompts/update (ditambah sesi ini) — deleteCustom
//     dan updateCustom, gate SAMA PERSIS dengan createCustom (Owner-only
//     + Business tier, dicek di handler server.go, bukan di sini).
//     Keduanya mengecek existence dulu (custom.Get) sebelum
//     Delete/Put — supaya update terhadap nama yang belum ada TIDAK
//     diam-diam jadi create (Put di store adalah upsert polos, tidak
//     ngecek existence sendiri), dan delete terhadap nama yang belum
//     ada mengembalikan error "not found" yang jelas alih-alih no-op
//     diam-diam.
//
// MASIH SENGAJA DISEDERHANAKAN (bukan lupa — butuh keputusan/file
// tambahan sebelum ditutup penuh):
//   - Fine-grained capability tenant.{id}.prompts.read TIDAK
//     diimplementasikan sebagai gate terpisah di dalam dispatch
//     JSON-RPC. Endpoint /mcp/rpc masih digate satu capability blanket
//     "tenant.{tenant_id}.mcp.query" di app.go (levelnya HTTP route,
//     bukan per JSON-RPC method) — ini SUDAH mencakup Owner/Admin/
//     Member (ketiganya punya mcp.query di billing/service.go per
//     API-CONTRACTS.md), jadi requirement "semua role bisa read"
//     sudah terpenuhi SECARA EFEKTIF hari ini, tapi bukan lewat gate
//     bernama "prompts.read" yang berdiri sendiri. Gate sungguhan
//     butuh capability.Engine + RoleResolver + roleID pemanggil masuk
//     ke package mcp — belum dibangun (lihat server.go).
//   - prompts.write (create/delete/update) TIDAK dicek lewat RBAC
//     CapabilityGrants biasa (billing/service.go tidak diupload sesi
//     ini, jadi tidak bisa ditambah grant "prompts.write" ke role
//     manapun tanpa menebak isi file itu). Sebagai gantinya, write
//     digate langsung: HANYA roleID=="owner" DAN planTier=="business"
//     yang boleh (lihat handlePromptsCreate/Delete/Update, server.go).
//     Ini secara fungsional memenuhi DoD ("Owner-only", "Business tier
//     only"), tapi TIDAK terintegrasi ke sistem CapabilityGrants
//     generik yang dipakai endpoint lain — kalau nanti butuh Admin
//     juga bisa write (bukan cuma Owner), ini perlu direvisi bersama
//     billing/service.go.
//   - Tenant isolation untuk custom prompt DIVERIFIKASI lewat unit
//     test baru (lihat mcp/custom_prompt_test.go), mengikuti
//     gaya TestServer_ToolsCall_CannotEscapeTenant — TAPI belum
//     live-tested dari MCP Test Console sungguhan (sandbox ini tidak
//     punya server TrailSeal berjalan).
// ============================================================

// Source menandai asal sebuah prompt di registry.
const (
	PromptSourceBuiltin = "builtin"
	PromptSourceCustom  = "custom"
)

// MCPPromptDefinition adalah representasi prompt pada prompts/list.
//
// Hanya Name/Description/Arguments yang diekspos ke client MCP
// (json tag polos). Field lain (TenantID/Template/Source/CreatedAt)
// sengaja `json:"-"` — dipakai internal (RBAC, rendering, UI admin),
// bukan bagian spec MCP standar.
type MCPPromptDefinition struct {
	Name        string              `json:"name"`
	TenantID    string              `json:"-"` // "" = built-in (semua tenant); tenant_id = custom
	Description string              `json:"description"`
	Arguments   []MCPPromptArgument `json:"arguments"`
	Template    string              `json:"-"`
	Source      string              `json:"-"`
	CreatedAt   time.Time           `json:"-"`
}

// MCPPromptArgument adalah argumen yang diterima sebuah prompt.
type MCPPromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// MCPPromptsListParams adalah params prompts/list.
type MCPPromptsListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// MCPPromptsListResult adalah result prompts/list.
type MCPPromptsListResult struct {
	Prompts    []MCPPromptDefinition `json:"prompts"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

// MCPPromptsGetParams adalah params prompts/get.
type MCPPromptsGetParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// MCPPromptMessage adalah satu message dalam hasil prompts/get.
type MCPPromptMessage struct {
	Role    string          `json:"role"`
	Content MCPContentBlock `json:"content"`
}

// MCPPromptsGetResult adalah result prompts/get. Bentuk TIDAK diubah
// dari yang sudah ada sebelumnya (per constraint task).
type MCPPromptsGetResult struct {
	Description string             `json:"description,omitempty"`
	Messages    []MCPPromptMessage `json:"messages"`
}

// MCPPromptsCreateParams adalah params method BARU "prompts/create"
// (non-standar MCP — lihat komentar keputusan di server.go). Dipakai
// membuat custom prompt tenant Business.
type MCPPromptsCreateParams struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Arguments   []MCPPromptArgument `json:"arguments,omitempty"`
	Template    string              `json:"template"`
}

// MCPPromptsCreateResult adalah result "prompts/create" — bentuk
// SAMA dengan satu entry di prompts/list (name/description/arguments
// saja, tanpa template/tenant_id/source — tetap tidak mengekspos
// field internal ke client MCP).
type MCPPromptsCreateResult struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Arguments   []MCPPromptArgument `json:"arguments"`
}

// MCPPromptsDeleteParams adalah params method BARU "prompts/delete"
// (non-standar MCP, sama sifatnya dengan prompts/create). Cuma butuh
// nama prompt yang mau dihapus.
type MCPPromptsDeleteParams struct {
	Name string `json:"name"`
}

// MCPPromptsDeleteResult adalah result "prompts/delete". Minimal —
// cuma konfirmasi nama yang berhasil dihapus.
type MCPPromptsDeleteResult struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// MCPPromptsUpdateParams adalah params method BARU "prompts/update"
// (non-standar MCP). Shape SAMA dengan MCPPromptsCreateParams —
// Name menentukan prompt mana yang diupdate, field lain adalah nilai
// baru penuh (bukan partial patch — update selalu mengganti
// description/arguments/template sekaligus, konsisten dengan Put
// yang bersifat upsert-penuh di store layer).
type MCPPromptsUpdateParams struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Arguments   []MCPPromptArgument `json:"arguments,omitempty"`
	Template    string              `json:"template"`
}

// MCPPromptsUpdateResult adalah result "prompts/update" — bentuk SAMA
// dengan MCPPromptsCreateResult.
type MCPPromptsUpdateResult struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Arguments   []MCPPromptArgument `json:"arguments"`
}

// ============================================================
// PROMPT REGISTRY
//
// Struct konkret dengan method spesifik — BUKAN interface generik
// baru yang disuntik ke banyak tempat (menghindari bug pattern #9:
// menambah method ke interface Go mematahkan semua mock).
//
// Field `custom` disuntik lewat SetCustomStore SETELAH construct
// (pola sama dengan mcpGateway.SetQuotaEnforcer di app.go) — bukan
// lewat parameter NewPromptRegistry/NewServer, supaya tidak mengubah
// signature keduanya (NewServer(gateway) dipakai di 20+ test lama).
// nil-safe: kalau tidak pernah di-set, ListForTenant/Get berlaku
// seperti sebelumnya (built-in saja) — tidak ada regresi test lama.
// ============================================================

type PromptRegistry struct {
	mu      sync.RWMutex
	builtin map[string]MCPPromptDefinition
	custom  CustomPromptStore
}

// SetCustomStore menyuntik custom prompt store (Business tier).
// Aman dipanggil nol kali (custom == nil, ListForTenant/Get fallback
// ke built-in saja) — lihat komentar di atas.
func (r *PromptRegistry) SetCustomStore(store CustomPromptStore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.custom = store
}

// NewPromptRegistry membuat registry dan men-seed built-in prompt.
func NewPromptRegistry() *PromptRegistry {
	r := &PromptRegistry{
		builtin: make(map[string]MCPPromptDefinition),
	}

	for _, p := range seedBuiltinPrompts() {
		r.builtin[p.Name] = p
	}

	return r
}

// ListForTenant mengembalikan semua prompt yang terlihat oleh tenant
// tersebut: built-in (sama untuk semua tenant/tier — pembeda tier ada
// di UI label "evaluation" untuk Free, bukan di sini, lihat
// pricing-doc §1.1) DIGABUNG dengan custom prompt milik tenant itu
// (kalau ada — hanya tenant Business yang bisa punya, ditegakkan di
// sisi create, bukan di sini; tenant Free/Pro otomatis dapat slice
// custom kosong karena memang tidak pernah bisa create).
func (r *PromptRegistry) ListForTenant(tenantID string) []MCPPromptDefinition {
	r.mu.RLock()
	custom := r.custom
	builtinCount := len(r.builtin)
	prompts := make([]MCPPromptDefinition, 0, builtinCount)
	for _, p := range r.builtin {
		prompts = append(prompts, p)
	}
	r.mu.RUnlock()

	if custom != nil {
		prompts = append(prompts, custom.ListForTenant(tenantID)...)
	}

	// Urutan deterministik supaya prompts/list stabil antar panggilan
	// (map Go tidak menjamin urutan iterasi, dan urutan custom dari
	// bbolt cursor juga tidak dijamin stabil terhadap builtin).
	for i := 1; i < len(prompts); i++ {
		for j := i; j > 0 && prompts[j-1].Name > prompts[j].Name; j-- {
			prompts[j-1], prompts[j] = prompts[j], prompts[j-1]
		}
	}

	return prompts
}

// Get mencari satu prompt berdasarkan nama: built-in dulu (built-in
// terlihat sama oleh semua tenant, tidak ada isolasi), lalu custom
// MILIK tenantID tersebut. Custom prompt tenant lain harus dianggap
// "tidak ada" di sini — isolasi ditegakkan oleh
// CustomPromptStore.Get sendiri (lihat custom_prompt_store_bbolt.go),
// bukan oleh filter tambahan di layer ini.
func (r *PromptRegistry) Get(tenantID, name string) (MCPPromptDefinition, error) {
	r.mu.RLock()
	p, ok := r.builtin[name]
	custom := r.custom
	r.mu.RUnlock()

	if ok {
		return p, nil
	}

	if custom != nil {
		if cp, err := custom.Get(tenantID, name); err == nil {
			return cp, nil
		}
	}

	return MCPPromptDefinition{}, fmt.Errorf("prompt not found: %s", name)
}

// isBuiltinName mengecek apakah sebuah nama sudah dipakai built-in
// prompt — dipakai handlePromptsCreate/Delete/Update (server.go)
// untuk menolak operasi custom prompt yang menyentuh nama built-in,
// supaya tidak ambigu prompt mana yang sebenarnya dijalankan.
func (r *PromptRegistry) isBuiltinName(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.builtin[name]
	return ok
}

// createCustom menyimpan custom prompt baru lewat store yang sudah
// disuntik SetCustomStore. Return error kalau store belum di-set
// (harus tidak pernah terjadi di production setelah app.go diupdate,
// tapi fail closed, jangan panic, kalau suatu saat lupa disambung).
func (r *PromptRegistry) createCustom(def MCPPromptDefinition) error {
	r.mu.RLock()
	custom := r.custom
	r.mu.RUnlock()

	if custom == nil {
		return fmt.Errorf("custom prompt store not configured")
	}
	return custom.Put(def)
}

// deleteCustom menghapus custom prompt milik tenant tersebut.
// Mengecek existence dulu lewat custom.Get SEBELUM Delete — supaya
// menghapus nama yang belum pernah ada mengembalikan error "not
// found" yang jelas ke caller, bukan sukses diam-diam (bolt Delete
// pada key yang tidak ada adalah no-op aman di layer store, jadi
// pengecekan existence ini SENGAJA diletakkan di sini, bukan di
// custom_prompt_store_bbolt.go — konsisten dengan pembagian tanggung
// jawab store-primitif vs registry-business-logic yang sudah ada).
func (r *PromptRegistry) deleteCustom(tenantID, name string) error {
	r.mu.RLock()
	custom := r.custom
	r.mu.RUnlock()

	if custom == nil {
		return fmt.Errorf("custom prompt store not configured")
	}

	if _, err := custom.Get(tenantID, name); err != nil {
		return fmt.Errorf("prompt not found: %s", name)
	}

	return custom.Delete(tenantID, name)
}

// updateCustom mengganti custom prompt yang SUDAH ADA milik tenant
// tersebut. Mengecek existence dulu lewat custom.Get SEBELUM Put —
// supaya update terhadap nama yang belum ada mengembalikan error
// "not found", bukan diam-diam berperilaku seperti create (Put di
// store adalah upsert polos, tidak ngecek existence sendiri).
//
// CreatedAt SENGAJA dipertahankan dari record lama (bukan diisi
// time.Now() lagi) — update mengganti isi prompt, bukan membuat
// ulang identitasnya; CreatedAt tetap mencerminkan kapan prompt itu
// pertama kali dibuat.
func (r *PromptRegistry) updateCustom(def MCPPromptDefinition) error {
	r.mu.RLock()
	custom := r.custom
	r.mu.RUnlock()

	if custom == nil {
		return fmt.Errorf("custom prompt store not configured")
	}

	existing, err := custom.Get(def.TenantID, def.Name)
	if err != nil {
		return fmt.Errorf("prompt not found: %s", def.Name)
	}

	def.CreatedAt = existing.CreatedAt
	def.Source = PromptSourceCustom

	return custom.Put(def)
}

// seedBuiltinPrompts mendefinisikan 3 built-in prompt awal, align ke
// domain produk (LLM agent audit + MCP Gateway). Tambah entry baru di
// sini kalau perlu prompt lain — tidak perlu ubah kode di handler
// manapun.
func seedBuiltinPrompts() []MCPPromptDefinition {
	now := time.Now()

	return []MCPPromptDefinition{
		{
			Name:        "audit-investigate",
			Description: "Tarik dan analisis audit event terkait sebuah resource tertentu.",
			Arguments: []MCPPromptArgument{
				{
					Name:        "resource",
					Description: "Resource yang ingin diinvestigasi (mis. tool ID, agent ID, atau nama resource audit).",
					Required:    true,
				},
				{
					Name:        "date_range",
					Description: "Rentang tanggal investigasi, mis. \"7 hari terakhir\". Kosongkan untuk seluruh retensi yang tersedia.",
					Required:    false,
				},
			},
			Template: "Tarik audit event yang terkait dengan resource \"{resource}\" untuk rentang waktu {date_range}. " +
				"Analisis pola akses, identifikasi aktor yang terlibat, dan tandai aktivitas yang tampak anomali " +
				"(mis. akses di luar jam kerja, lonjakan frekuensi, atau kegagalan berulang).",
			Source:    PromptSourceBuiltin,
			CreatedAt: now,
		},
		{
			Name:        "agent-health-report",
			Description: "Rangkum status, capability, dan aktivitas terakhir sebuah agent.",
			Arguments: []MCPPromptArgument{
				{
					Name:        "agent_id",
					Description: "ID agent yang ingin dirangkum statusnya.",
					Required:    true,
				},
			},
			Template: "Rangkum status kesehatan agent \"{agent_id}\": daftar capability yang terdaftar, tools yang bisa " +
				"diaksesnya, dan aktivitas (tools/call) dalam periode terakhir. Tandai kalau ada tool yang gagal " +
				"berulang atau capability yang tidak pernah dipakai.",
			Source:    PromptSourceBuiltin,
			CreatedAt: now,
		},
		{
			Name:        "tool-failure-triage",
			Description: "Investigasi pola kegagalan berulang pada sebuah tool.",
			Arguments: []MCPPromptArgument{
				{
					Name:        "tool_id",
					Description: "ID tool yang mengalami kegagalan.",
					Required:    true,
				},
				{
					Name:        "since",
					Description: "Titik waktu mulai investigasi, mis. \"24 jam terakhir\". Kosongkan untuk seluruh retensi yang tersedia.",
					Required:    false,
				},
			},
			Template: "Investigasi kegagalan tool \"{tool_id}\" sejak {since}. Kelompokkan kegagalan berdasarkan jenis " +
				"error, identifikasi apakah kegagalan terkonsentrasi pada tenant/user tertentu, dan sarankan langkah " +
				"triase berikutnya.",
			Source:    PromptSourceBuiltin,
			CreatedAt: now,
		},
	}
}

// renderPromptMessages merender Template sebuah prompt dengan argumen
// yang dikirim client di prompts/get. Placeholder di Template berupa
// {nama_argumen}, diganti apa adanya dengan nilai string dari params.
//
// Argumen optional yang tidak dikirim diisi placeholder penjelas
// (bukan dikosongkan begitu saja) supaya hasil render tetap masuk
// akal dibaca LLM. Validasi argumen required dilakukan di caller
// (handlePromptsGet, server.go) SEBELUM fungsi ini dipanggil.
func renderPromptMessages(def MCPPromptDefinition, args map[string]string) []MCPPromptMessage {
	text := def.Template

	for _, a := range def.Arguments {
		val := args[a.Name]
		if strings.TrimSpace(val) == "" {
			val = fmt.Sprintf("(tidak dibatasi — %s tidak diberikan)", a.Name)
		}
		text = strings.ReplaceAll(text, "{"+a.Name+"}", val)
	}

	return []MCPPromptMessage{
		{
			Role: "user",
			Content: MCPContentBlock{
				Type: "text",
				Text: text,
			},
		},
	}
}

// missingRequiredArgument mengecek argumen required yang tidak
// dikirim client, dipakai handlePromptsGet (server.go) untuk validasi
// sebelum render. Mengembalikan nama argumen pertama yang hilang,
// atau "" kalau semua required argumen sudah ada.
func missingRequiredArgument(def MCPPromptDefinition, args map[string]string) string {
	for _, a := range def.Arguments {
		if !a.Required {
			continue
		}
		if strings.TrimSpace(args[a.Name]) == "" {
			return a.Name
		}
	}
	return ""
}

// validatePromptsCreateParams mengecek field wajib untuk
// prompts/create. Mengembalikan pesan error (non-kosong) kalau
// invalid, "" kalau valid.
func validatePromptsCreateParams(p MCPPromptsCreateParams) string {
	if strings.TrimSpace(p.Name) == "" {
		return "name is required"
	}
	if strings.TrimSpace(p.Template) == "" {
		return "template is required"
	}
	for _, a := range p.Arguments {
		if strings.TrimSpace(a.Name) == "" {
			return "each argument must have a non-empty name"
		}
	}
	return ""
}

// validatePromptsUpdateParams mengecek field wajib untuk
// prompts/update. Sama persis dengan validatePromptsCreateParams
// (shape params identik) — dibuat fungsi terpisah, bukan reuse
// langsung, supaya validasi update boleh berkembang beda dari create
// nanti tanpa mengubah perilaku create yang sudah live-tested.
func validatePromptsUpdateParams(p MCPPromptsUpdateParams) string {
	if strings.TrimSpace(p.Name) == "" {
		return "name is required"
	}
	if strings.TrimSpace(p.Template) == "" {
		return "template is required"
	}
	for _, a := range p.Arguments {
		if strings.TrimSpace(a.Name) == "" {
			return "each argument must have a non-empty name"
		}
	}
	return ""
}

// validatePromptsDeleteParams mengecek field wajib untuk
// prompts/delete.
func validatePromptsDeleteParams(p MCPPromptsDeleteParams) string {
	if strings.TrimSpace(p.Name) == "" {
		return "name is required"
	}
	return ""
}