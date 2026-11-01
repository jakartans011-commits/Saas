package mcp

import "strings"

// ============================================================
// ROOTS & SAMPLING — DEPRECATED (SEP-2577)
//
// Keputusan final bagian 2: kedua primitive ini resmi deprecated.
// TrailSeal TIDAK membangun full implementation — hanya pass-through
// sederhana selama window support 12 bulan untuk client lama yang
// masih mengirim method/notification terkait.
//
// CATATAN ARSITEKTUR (penting, beda dari Resources/Prompts):
// Pada spec MCP asli, Roots & Sampling adalah SERVER-INITIATED
// (server yang meminta root list / meminta client menjalankan
// sampling ke client). Endpoint /mcp/v1/rpc TrailSeal adalah HTTP
// single-shot request/response (lihat server.go: ServeHTTP dibungkus
// adaptor.HTTPHandlerFunc, tanpa transport persisten) — server TIDAK
// PERNAH bisa memulai request ke client. Karena itu TrailSeal tidak
// pernah, dan tidak akan, memanggil roots/list atau
// sampling/createMessage KE client manapun.
//
// Pass-through di sini HANYA menangani arah client -> server yang
// realistis terjadi dari client lama:
//   - notifications/roots/*, notifications/sampling/*: client
//     memberi tahu server sesuatu berubah. Server tidak pernah query
//     root/sampling, jadi cukup di-acknowledge tanpa efek apapun
//     (true no-op pass-through).
//   - roots/list, sampling/createMessage: kalau ada client lama yang
//     (secara arah protokol seharusnya tidak pernah) mengirim ini KE
//     server, dijawab graceful — bukan generic "method not found" —
//     tanpa berpura-pura mengimplementasi primitive yang deprecated.
//
// Tidak diiklankan di capabilities initialize (lihat handleInitialize
// di server.go) karena ini bukan dukungan penuh, cuma pass-through.
// ============================================================

// isRootsOrSamplingNotification mengenali notification client->server
// terkait roots/sampling yang perlu di-acknowledge sebagai no-op.
func isRootsOrSamplingNotification(method string) bool {
	return strings.HasPrefix(method, "notifications/roots/") ||
		strings.HasPrefix(method, "notifications/sampling/")
}

// handleRootsSamplingNotification meng-acknowledge notification
// roots/sampling dari client tanpa efek apapun (true pass-through,
// bukan implementasi).
func (s *Server) handleRootsSamplingNotification(
	id interface{},
) *JSONRPCResponse {
	return newJSONRPCResult(id, map[string]interface{}{})
}

// handleRootsList menangani roots/list. Secara arah protokol ini
// seharusnya tidak pernah dikirim client ke server, tapi kalau ada
// client lama yang salah kirim, dibalas dengan daftar kosong —
// TrailSeal tidak mengekspos root filesystem apapun — bukan error,
// supaya client lama tidak crash.
func (s *Server) handleRootsList(
	id interface{},
) *JSONRPCResponse {
	return newJSONRPCResult(id, map[string]interface{}{
		"roots": []interface{}{},
	})
}

// handleSamplingCreateMessage menangani sampling/createMessage.
// TrailSeal tidak punya LLM sampling untuk dipanggil balik — primitive
// ini deprecated (SEP-2577) — jadi ditolak eksplisit dengan pesan
// jelas, bukan generic "method not found" yang membingungkan client.
func (s *Server) handleSamplingCreateMessage(
	id interface{},
) *JSONRPCResponse {
	return newJSONRPCError(
		id,
		JSONRPCMethodNotFound,
		"sampling is deprecated (SEP-2577) and not supported by this server",
		nil,
	)
}
