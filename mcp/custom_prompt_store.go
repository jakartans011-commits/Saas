package mcp

// ============================================================
// CUSTOM PROMPT STORE
//
// Interface BARU, purpose-built untuk custom prompt (Business tier).
// TIDAK memperluas RoleStore atau interface lain yang sudah ada —
// menambah method ke interface existing mematahkan semua mock yang
// mengimplementasikannya (bug pattern #9, lihat API-CONTRACTS.md).
// Karena interface ini baru, tidak ada mock lama yang bisa dipatahkan.
//
// BoltCustomPromptStore (custom_prompt_store_bbolt.go) adalah satu-
// satunya implementasi produksi. Interface tetap dipisah dari struct
// supaya test bisa memakai fake in-memory tanpa bbolt kalau perlu.
// ============================================================

type CustomPromptStore interface {
	// Put menyimpan/menimpa satu custom prompt milik prompt.TenantID.
	// prompt.TenantID WAJIB diisi — sama seperti BoltRoleStore.PutRole
	// menolak role tanpa TenantID, mencegah "global prompt" tidak
	// sengaja tersimpan tanpa scope tenant.
	Put(prompt MCPPromptDefinition) error

	// Get mengambil satu custom prompt HANYA jika tersimpan untuk
	// tenantID tersebut. Prompt custom milik tenant lain harus
	// dianggap "tidak ada" di sini, bukan ditemukan lalu ditolak di
	// caller — pola ini sama persis dengan BoltRoleStore.GetRole
	// (mencegah role/prompt ID collision bug, lihat rbac/role.go).
	Get(tenantID, name string) (MCPPromptDefinition, error)

	// ListForTenant mengembalikan semua custom prompt milik tenantID.
	// Tenant tanpa custom prompt (Free/Pro, atau Business yang belum
	// pernah create) mendapat slice kosong, bukan error.
	ListForTenant(tenantID string) []MCPPromptDefinition

	// DeleteAllForTenant menghapus semua custom prompt milik tenantID.
	// Dipakai jalur tenant purge/deletion (compliance_handlers.go) —
	// belum disambungkan di task ini (di luar scope), tapi disediakan
	// dari awal mengikuti pola BoltRoleStore.DeleteAllForTenant supaya
	// tidak perlu breaking change ke interface ini nanti.
	DeleteAllForTenant(tenantID string) int

	// Delete menghapus satu custom prompt milik tenantID (ditambahkan
	// untuk prompts/delete, prompts/update). Tidak error kalau key
	// tidak ditemukan (no-op aman) — pengecekan "harus ada dulu
	// sebelum dihapus/diupdate" dilakukan di PromptRegistry
	// (deleteCustom/updateCustom, prompts.go), bukan di sini, supaya
	// interface ini tetap primitif murni tanpa business logic,
	// konsisten dengan Put/Get/ListForTenant/DeleteAllForTenant di
	// atas.
	Delete(tenantID, name string) error
}