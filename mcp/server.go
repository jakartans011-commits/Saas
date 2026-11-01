package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ============================================================
// MCP JSON-RPC SERVER
// ============================================================

// Server adalah HTTP-facing MCP JSON-RPC server.
//
// Authentication/tenant resolution tetap menjadi tanggung jawab
// HTTP/application layer yang sudah ada.
//
// Server menerima tenantID dan userID authoritative dari caller.
// Nilai tersebut TIDAK berasal dari MCP params/context.
type Server struct {
	gateway        *Gateway
	prompts        *PromptRegistry
	sessions       *SessionStore
	strictSessions bool
}

// NewServer membuat MCP JSON-RPC server.
//
// PromptRegistry di-construct dan di-seed built-in prompt di sini â€”
// tidak menambah parameter ke NewServer supaya call site existing
// (app.go) tidak perlu diubah untuk task built-in-prompt ini.
func NewServer(gateway *Gateway) *Server {
	return &Server{
		gateway: gateway,
		prompts: NewPromptRegistry(),
		sessions: NewSessionStore(defaultSessionTTL),
	}
}

// SetCustomPromptStore menyuntik custom prompt store (Business tier)
// SETELAH construct â€” pola sama dengan
// mcpGateway.SetQuotaEnforcer(tenantStore) di app.go. nil-safe: kalau
// tidak dipanggil, ListForTenant/Get PromptRegistry berlaku seperti
// sebelumnya (built-in saja), jadi tidak mengubah/mematahkan 20+ call
// site `NewServer(gateway)` di test lama.
func (s *Server) SetCustomPromptStore(store CustomPromptStore) {
	if s == nil || s.prompts == nil {
		return
	}
	s.prompts.SetCustomStore(store)
}

// ServeHTTP menangani MCP JSON-RPC requests.
//
// Supported methods:
//   - initialize
//   - tools/list
//   - tools/call
//   - resources/list
//   - resources/read
//   - prompts/list
//   - prompts/get
//   - prompts/create, prompts/delete, prompts/update (Owner-only,
//     Business tier â€” lihat handlePromptsCreate/Delete/Update)
//
// Pass-through minimal (deprecated, SEP-2577 â€” lihat roots_sampling.go):
//   - roots/list, sampling/createMessage
//   - notifications/roots/*, notifications/sampling/*
func (s *Server) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
		// Streamable HTTP transport (MCP spec): tidak ada server-initiated
	// SSE stream, jadi GET WAJIB dibalas 405. DELETE dipakai client
	// untuk terminate session.
	if r.Method == http.MethodGet {
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	tenantID := r.Header.Get("X-Tenant-ID")
	userID := r.Header.Get("X-User-ID")

	// roleID/planTier: OPSIONAL, TIDAK seperti tenantID/userID di
	// bawah (yang wajib, request ditolak kalau kosong). Kosong berarti
	// "tidak diketahui/tidak ada elevated permission" â€” dipakai HANYA
	// oleh prompts/create, prompts/delete, prompts/update untuk gate
	// Owner-only + tier Business (lihat handlePromptsCreate/Delete/
	// Update). Semua method LAIN (tools/call, resources/*,
	// prompts/list, prompts/get) tidak butuh dua header ini sama sekali,
	// jadi caller yang tidak mengirimnya (mis. semua test lama, native
	// endpoint /mcp/v1/rpc via API key) tidak terdampak.
	//
	// KEPUTUSAN PRODUK, PERLU DIKONFIRMASI: caller lewat API key (native
	// endpoint /mcp/v1/rpc) TIDAK memiliki konsep "role" (API key hanya
	// punya scope ingest/read/admin, bukan Owner/Admin/Member) â€” middleware
	// InjectMCPIdentityFromAPIKey TIDAK diubah untuk mengisi X-Role-ID,
	// jadi roleID selalu "" untuk jalur itu, dan prompts/create/delete/
	// update SELALU ditolak untuk API key, terlepas dari tier. Custom
	// prompt write praktis jadi aksi dashboard-only (lewat
	// /api/tenants/:id/mcp/rpc, JWT). Kalau ini bukan yang diinginkan
	// produk, perlu didiskusikan ulang sebelum go-live fitur write.
	roleID := r.Header.Get("X-Role-ID")
	planTier := r.Header.Get("X-Plan-Tier")

	// --------------------------------------------------------
	// CRITICAL AUTHENTICATED IDENTITY BOUNDARY
	//
	// Tenant dan user hanya berasal dari authenticated HTTP
	// layer/header. MCP params tidak boleh mengganti nilai ini.
	// --------------------------------------------------------
	if tenantID == "" {
		writeJSONRPCError(
			w,
			nil,
			JSONRPCInvalidRequest,
			"tenant_id is required",
			nil,
		)
		return
	}

	if userID == "" {
		writeJSONRPCError(
			w,
			nil,
			JSONRPCInvalidRequest,
			"user_id is required",
			nil,
		)
		return
	}

		// DELETE = terminate session. Sesi harus milik tenant+user ini,
	// sehingga ID yang bocor tidak bisa dipakai menghapus sesi orang lain.
	if r.Method == http.MethodDelete {
		sessionID := r.Header.Get(SessionHeader)
		if sessionID == "" {
			http.Error(w, "session id required", http.StatusBadRequest)
			return
		}
		if s.sessions == nil || !s.sessions.Validate(sessionID, tenantID, userID) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		s.sessions.Delete(sessionID)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var request JSONRPCRequest

	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&request); err != nil {
		writeJSONRPCError(
			w,
			nil,
			JSONRPCParseError,
			"parse error",
			nil,
		)
		return
	}

	if err := validateJSONRPCRequest(&request); err != nil {
		writeJSONRPCError(
			w,
			request.ID,
			JSONRPCInvalidRequest,
			err.Error(),
			nil,
		)
		return
	}

		// Session management (Streamable HTTP). Lenient secara default:
	// header divalidasi hanya jika dikirim. 404 di level HTTP, bukan
	// JSON-RPC error, sesuai spec.
	if s.sessions != nil {
		sessionID := r.Header.Get(SessionHeader)
		switch {
		case request.Method == "server/discover":
			// Stateless (2026-07-28): tanpa sesi, tidak divalidasi.
		case request.Method == "initialize":
			if newID := s.sessions.Create(tenantID, userID); newID != "" {
				w.Header().Set(SessionHeader, newID)
			}
		case sessionID != "":
			if !s.sessions.Validate(sessionID, tenantID, userID) {
				http.Error(w, "session not found", http.StatusNotFound)
				return
			}
		case s.strictSessions:
			http.Error(w, "session required", http.StatusNotFound)
			return
		}
	}

	response := s.handleRequest(
		tenantID,
		userID,
		roleID,
		planTier,
		&request,
	)

		writeMCPResponse(w, r, response)
}

// writeMCPResponse melakukan content negotiation sesuai Streamable
// HTTP: kalau client minta text/event-stream lewat Accept header,
// balas sebagai SSE (satu event, lalu selesai). Kalau tidak, balas
// JSON biasa seperti sebelumnya (backward compatible untuk semua
// client lama).
func writeMCPResponse(
	w http.ResponseWriter,
	r *http.Request,
	resp *JSONRPCResponse,
) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		writeSSEResponse(w, resp)
		return
	}
	writeJSONRPCResponse(w, resp)
}

// writeSSEResponse membalas satu JSON-RPC response lewat SSE.
func writeSSEResponse(w http.ResponseWriter, resp *JSONRPCResponse) {
	body, err := json.Marshal(resp)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// handleRequest dispatches JSON-RPC methods.
func (s *Server) handleRequest(
	tenantID string,
	userID string,
	roleID string,
	planTier string,
	req *JSONRPCRequest,
) *JSONRPCResponse {
	if s == nil || s.gateway == nil {
		return newJSONRPCError(
			req.ID,
			JSONRPCInternalError,
			"internal error",
			nil,
		)
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.ID)

	case "server/discover":
		return s.handleServerDiscover(req.ID, req.Params)	

	case "tools/list":
		return s.handleToolsList(
			tenantID,
			req.ID,
			req.Params,
		)

	case "tools/call":
		return s.handleToolsCall(
			tenantID,
			userID,
			req.ID,
			req.Params,
		)

	case "resources/list":
		return s.handleResourcesList(req.ID, req.Params)

	case "resources/read":
		return s.handleResourcesRead(
			tenantID,
			req.ID,
			req.Params,
		)

	case "prompts/list":
		return s.handlePromptsList(tenantID, req.ID, req.Params)

	case "prompts/get":
		return s.handlePromptsGet(tenantID, req.ID, req.Params)

	case "prompts/create":
		return s.handlePromptsCreate(
			tenantID,
			roleID,
			planTier,
			req.ID,
			req.Params,
		)

	case "prompts/delete":
		return s.handlePromptsDelete(
			tenantID,
			roleID,
			planTier,
			req.ID,
			req.Params,
		)

	case "prompts/update":
		return s.handlePromptsUpdate(
			tenantID,
			roleID,
			planTier,
			req.ID,
			req.Params,
		)

	case "roots/list":
		return s.handleRootsList(req.ID)

	case "sampling/createMessage":
		return s.handleSamplingCreateMessage(req.ID)

	default:
		// Pass-through minimal untuk client lama: notification
		// roots/sampling di-acknowledge sebagai no-op (lihat
		// roots_sampling.go), bukan diperlakukan sebagai method
		// tidak dikenal.
		if isRootsOrSamplingNotification(req.Method) {
			return s.handleRootsSamplingNotification(req.ID)
		}

		return newJSONRPCError(
			req.ID,
			JSONRPCMethodNotFound,
			"method not found",
			nil,
		)
	}
}

// handleInitialize menangani MCP initialize.
func (s *Server) handleInitialize(
	id interface{},
) *JSONRPCResponse {
	result := map[string]interface{}{
		"protocolVersion": "2025-06-18",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
			"prompts":   map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "trailseal-mcp",
			"version": "1.0.0",
		},
	}

	return newJSONRPCResult(id, result)
}

// handleServerDiscover menangani "server/discover" — probe awal yang
// dikirim MCP Inspector SEBELUM "initialize". Stateless (lihat
// ServeHTTP: method ini tidak divalidasi session-nya). Method ini
// bagian dari spec 2026-07-28 (beda dari "initialize" yang masih
// 2025-06-18), makanya protocolVersion di bawah ini sengaja beda
// dengan yang di handleInitialize.
func (s *Server) handleServerDiscover(
id interface{},
rawParams json.RawMessage,
) *JSONRPCResponse {
_ = rawParams

result := map[string]interface{}{
"resultType":       "complete",
"supportedVersions": []string{"2026-07-28"},
"capabilities": map[string]interface{}{
"tools":     map[string]interface{}{},
"resources": map[string]interface{}{},
"prompts":   map[string]interface{}{},
},
"_meta": map[string]interface{}{
"io.modelcontextprotocol/serverInfo": map[string]interface{}{
"name":    "trailseal-mcp",
"version": "1.0.0",
},
},
}

return newJSONRPCResult(id, result)
}

// handleToolsList menangani tools/list.
func (s *Server) handleToolsList(
	tenantID string,
	id interface{},
	rawParams json.RawMessage,
) *JSONRPCResponse {
	var params MCPToolsListParams

	if err := decodeJSONRPCParams(
		rawParams,
		&params,
	); err != nil {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"invalid params",
			nil,
		)
	}

	// Cursor belum digunakan pada tahap ini.
	_ = params

	// --------------------------------------------------------
	// CRITICAL TENANT BOUNDARY
	//
	// discoverTools hanya menggunakan tenantID authoritative
	// dari HTTP layer.
	//
	// MCP params tidak pernah diberikan sebagai tenant source.
	// --------------------------------------------------------
	result, err := s.gateway.discoverTools(tenantID)
	if err != nil {
		return jsonRPCResponseFromMCPError(id, err)
	}

	return newJSONRPCResult(id, result)
}

// handleToolsCall menangani tools/call.
//
// MCP name diterjemahkan ke existing MCP Gateway menggunakan
// explicit ToolCall mode.
func (s *Server) handleToolsCall(
	tenantID string,
	userID string,
	id interface{},
	rawParams json.RawMessage,
) *JSONRPCResponse {
	var params MCPToolsCallParams

	if err := decodeJSONRPCParams(
		rawParams,
		&params,
	); err != nil {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"invalid params",
			nil,
		)
	}

	if strings.TrimSpace(params.Name) == "" {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"name is required",
			nil,
		)
	}

	// --------------------------------------------------------
	// CRITICAL TENANT BOUNDARY
	//
	// params.Arguments hanya menjadi parameter tool.
	//
	// tenant_id dari arguments TIDAK PERNAH menjadi
	// authoritative tenant identity.
	//
	// tenantID tetap berasal dari authenticated HTTP caller.
	// --------------------------------------------------------
	request := &MCPRequest{
		ToolCall: &MCPToolCall{
			ToolName: params.Name,
			Params:   params.Arguments,
		},
	}

	response, err := s.gateway.Query(
		tenantID,
		userID,
		request,
	)

	if err != nil {
		// ----------------------------------------------------
		// IMPORTANT:
		//
		// Tool resolution failure seperti:
		//
		//     tenant-a -> echo-b
		//
		// harus menjadi JSON-RPC error.
		//
		// Sebelumnya error diubah menjadi:
		//
		//     result.isError = true
		//
		// sehingga TestServer_ToolsCall_CannotEscapeTenant
		// tidak mendapatkan response.Error.
		// ----------------------------------------------------
		return jsonRPCResponseFromMCPError(id, err)
	}

	return newJSONRPCResult(
		id,
		MCPToolsCallResult{
			Content: []MCPContentBlock{
				{
					Type: "text",
					Text: response.Response,
				},
			},
		},
	)
}

// handleResourcesList menangani resources/list.
func (s *Server) handleResourcesList(
	id interface{},
	rawParams json.RawMessage,
) *JSONRPCResponse {
	var params MCPResourcesListParams

	if err := decodeJSONRPCParams(
		rawParams,
		&params,
	); err != nil {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"invalid params",
			nil,
		)
	}

	// Cursor belum digunakan pada tahap ini â€” hanya 1 resource statis.
	_ = params

	return newJSONRPCResult(id, buildMCPResourcesList())
}

// handleResourcesRead menangani resources/read.
func (s *Server) handleResourcesRead(
	tenantID string,
	id interface{},
	rawParams json.RawMessage,
) *JSONRPCResponse {
	var params MCPResourcesReadParams

	if err := decodeJSONRPCParams(
		rawParams,
		&params,
	); err != nil {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"invalid params",
			nil,
		)
	}

	if strings.TrimSpace(params.URI) == "" {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"uri is required",
			nil,
		)
	}

	// --------------------------------------------------------
	// CRITICAL TENANT BOUNDARY
	//
	// tenantID tetap berasal dari authenticated HTTP caller, bukan
	// dari params â€” sama seperti handleToolsCall.
	// --------------------------------------------------------
	result, err := s.gateway.readResource(tenantID, params.URI)
	if err != nil {
		return jsonRPCResponseFromMCPError(id, err)
	}

	return newJSONRPCResult(id, result)
}

// handlePromptsList menangani prompts/list.
func (s *Server) handlePromptsList(
	tenantID string,
	id interface{},
	rawParams json.RawMessage,
) *JSONRPCResponse {
	var params MCPPromptsListParams

	if err := decodeJSONRPCParams(
		rawParams,
		&params,
	); err != nil {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"invalid params",
			nil,
		)
	}

	// Pagination (cursor) belum diimplementasikan â€” daftar prompt saat
	// ini kecil (built-in saja). Lihat prompts.go untuk PromptRegistry.
	_ = params

	return newJSONRPCResult(id, MCPPromptsListResult{
		Prompts: s.prompts.ListForTenant(tenantID),
	})
}

// handlePromptsGet menangani prompts/get.
//
// Prompt dicari lewat PromptRegistry (built-in untuk saat ini â€” lihat
// prompts.go). Kalau nama tidak ketemu, jatuh ke error prompt_not_found
// dengan pola yang sama persis seperti sebelumnya (-32602). Kalau
// ketemu tapi argumen required tidak dikirim, balas invalid params
// (belum ada di pola lama karena dulu semua request selalu
// prompt_not_found lebih dulu).
func (s *Server) handlePromptsGet(
	tenantID string,
	id interface{},
	rawParams json.RawMessage,
) *JSONRPCResponse {
	var params MCPPromptsGetParams

	if err := decodeJSONRPCParams(
		rawParams,
		&params,
	); err != nil {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"invalid params",
			nil,
		)
	}

	if strings.TrimSpace(params.Name) == "" {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"name is required",
			nil,
		)
	}

	prompt, err := s.prompts.Get(tenantID, params.Name)
	if err != nil {
		return jsonRPCResponseFromMCPError(
			id,
			newMCPError(
				MCPErrorPromptNotFound,
				fmt.Sprintf("prompt not found: %s", params.Name),
				nil,
			),
		)
	}

	if missing := missingRequiredArgument(prompt, params.Arguments); missing != "" {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			fmt.Sprintf("missing required argument: %s", missing),
			nil,
		)
	}

	return newJSONRPCResult(id, MCPPromptsGetResult{
		Description: prompt.Description,
		Messages:    renderPromptMessages(prompt, params.Arguments),
	})
}

// handlePromptsCreate menangani "prompts/create" â€” method BARU,
// TIDAK ada di spec MCP standar (spec hanya define prompts/list dan
// prompts/get; pembuatan prompt adalah aksi admin tenant, bukan
// bagian protokol client-facing). KEPUTUSAN DESAIN, perlu dikonfirmasi
// user: method ini ditambahkan ke dispatcher JSON-RPC yang sama
// (bukan REST endpoint baru) supaya reuse tenant-scoping/error-taxonomy
// yang sudah ada di server ini â€” alternatif (REST endpoint terpisah)
// sama validnya, tapi butuh app.go route + handler baru dari nol.
//
// Gate (lihat catatan panjang di ServeHTTP soal roleID/planTier):
//   - roleID HARUS "owner" â€” persis string role ID yang dipakai
//     rbac/tenant provisioning ("owner"/"admin"/"member"), BUKAN
//     dicek lewat CapabilityGrants (billing/service.go belum tersedia
//     untuk ditambah grant "prompts.write" dengan aman).
//   - planTier HARUS "business" (lowercase, sama dengan konvensi
//     Tenant.PlanTier yang sudah dipakai GET /usage â€” "free"/"pro"/
//     "business").
//   - Nama prompt TIDAK boleh menimpa nama built-in (audit-investigate,
//     agent-health-report, tool-failure-triage) â€” dicek lewat
//     PromptRegistry.isBuiltinName, supaya tidak ambigu prompt mana
//     yang jalan saat prompts/get dipanggil.
func (s *Server) handlePromptsCreate(
	tenantID string,
	roleID string,
	planTier string,
	id interface{},
	rawParams json.RawMessage,
) *JSONRPCResponse {
	if roleID != "owner" {
		return jsonRPCResponseFromMCPError(
			id,
			newMCPError(
				MCPErrorForbidden,
				"only the tenant owner can create custom prompts",
				nil,
			),
		)
	}

	if planTier != "business" {
		return jsonRPCResponseFromMCPError(
			id,
			newMCPError(
				MCPErrorForbidden,
				"custom prompts require the Business plan",
				nil,
			),
		)
	}

	var params MCPPromptsCreateParams

	if err := decodeJSONRPCParams(
		rawParams,
		&params,
	); err != nil {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"invalid params",
			nil,
		)
	}

	if msg := validatePromptsCreateParams(params); msg != "" {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			msg,
			nil,
		)
	}

	if s.prompts.isBuiltinName(params.Name) {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			fmt.Sprintf(
				"name %q is reserved by a built-in prompt",
				params.Name,
			),
			nil,
		)
	}

	def := MCPPromptDefinition{
		Name:        params.Name,
		TenantID:    tenantID,
		Description: params.Description,
		Arguments:   params.Arguments,
		Template:    params.Template,
		Source:      PromptSourceCustom,
		CreatedAt:   time.Now(),
	}

	if err := s.prompts.createCustom(def); err != nil {
		return jsonRPCResponseFromMCPError(
			id,
			InternalMCPError(err),
		)
	}

	return newJSONRPCResult(id, MCPPromptsCreateResult{
		Name:        def.Name,
		Description: def.Description,
		Arguments:   def.Arguments,
	})
}

// handlePromptsDelete menangani "prompts/delete" â€” method BARU,
// non-standar MCP, sama sifatnya dengan prompts/create (aksi admin
// tenant, bukan bagian protokol client-facing).
//
// Gate: IDENTIK dengan handlePromptsCreate (roleID=="owner" DAN
// planTier=="business") â€” menghapus custom prompt sama sensitifnya
// dengan membuatnya, jadi tidak ada alasan gate-nya lebih longgar.
//
// Nama built-in TIDAK BISA dihapus (isBuiltinName check) â€” built-in
// prompt bukan bagian dari custom prompt store sama sekali, jadi
// mencoba menghapusnya akan gagal di deleteCustom sebagai "not
// found"; check eksplisit di sini dipertahankan supaya pesan error
// untuk kasus itu jelas ("reserved by a built-in prompt"), bukan
// "not found" yang membingungkan (karena secara teknis prompt itu
// ADA, cuma bukan di custom store).
func (s *Server) handlePromptsDelete(
	tenantID string,
	roleID string,
	planTier string,
	id interface{},
	rawParams json.RawMessage,
) *JSONRPCResponse {
	if roleID != "owner" {
		return jsonRPCResponseFromMCPError(
			id,
			newMCPError(
				MCPErrorForbidden,
				"only the tenant owner can delete custom prompts",
				nil,
			),
		)
	}

	if planTier != "business" {
		return jsonRPCResponseFromMCPError(
			id,
			newMCPError(
				MCPErrorForbidden,
				"custom prompts require the Business plan",
				nil,
			),
		)
	}

	var params MCPPromptsDeleteParams

	if err := decodeJSONRPCParams(
		rawParams,
		&params,
	); err != nil {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"invalid params",
			nil,
		)
	}

	if msg := validatePromptsDeleteParams(params); msg != "" {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			msg,
			nil,
		)
	}

	if s.prompts.isBuiltinName(params.Name) {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			fmt.Sprintf(
				"name %q is reserved by a built-in prompt and cannot be deleted",
				params.Name,
			),
			nil,
		)
	}

	if err := s.prompts.deleteCustom(tenantID, params.Name); err != nil {
		if strings.HasPrefix(err.Error(), "prompt not found") {
			return jsonRPCResponseFromMCPError(
				id,
				newMCPError(
					MCPErrorPromptNotFound,
					fmt.Sprintf("prompt not found: %s", params.Name),
					nil,
				),
			)
		}
		return jsonRPCResponseFromMCPError(
			id,
			InternalMCPError(err),
		)
	}

	return newJSONRPCResult(id, MCPPromptsDeleteResult{
		Name:    params.Name,
		Deleted: true,
	})
}

// handlePromptsUpdate menangani "prompts/update" â€” method BARU,
// non-standar MCP, sama sifatnya dengan prompts/create.
//
// Gate: IDENTIK dengan handlePromptsCreate/Delete (roleID=="owner"
// DAN planTier=="business").
//
// Update SELALU mengganti description/arguments/template sekaligus
// (bukan partial patch) â€” konsisten dengan Put yang upsert-penuh di
// store layer (lihat MCPPromptsUpdateParams, prompts.go). Nama
// built-in tidak bisa diupdate, dan nama yang belum pernah dibuat
// sebagai custom prompt mengembalikan prompt_not_found (bukan
// diam-diam membuat baru) â€” lihat updateCustom di prompts.go.
func (s *Server) handlePromptsUpdate(
	tenantID string,
	roleID string,
	planTier string,
	id interface{},
	rawParams json.RawMessage,
) *JSONRPCResponse {
	if roleID != "owner" {
		return jsonRPCResponseFromMCPError(
			id,
			newMCPError(
				MCPErrorForbidden,
				"only the tenant owner can update custom prompts",
				nil,
			),
		)
	}

	if planTier != "business" {
		return jsonRPCResponseFromMCPError(
			id,
			newMCPError(
				MCPErrorForbidden,
				"custom prompts require the Business plan",
				nil,
			),
		)
	}

	var params MCPPromptsUpdateParams

	if err := decodeJSONRPCParams(
		rawParams,
		&params,
	); err != nil {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			"invalid params",
			nil,
		)
	}

	if msg := validatePromptsUpdateParams(params); msg != "" {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			msg,
			nil,
		)
	}

	if s.prompts.isBuiltinName(params.Name) {
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			fmt.Sprintf(
				"name %q is reserved by a built-in prompt and cannot be updated",
				params.Name,
			),
			nil,
		)
	}

	def := MCPPromptDefinition{
		Name:        params.Name,
		TenantID:    tenantID,
		Description: params.Description,
		Arguments:   params.Arguments,
		Template:    params.Template,
	}

	if err := s.prompts.updateCustom(def); err != nil {
		if strings.HasPrefix(err.Error(), "prompt not found") {
			return jsonRPCResponseFromMCPError(
				id,
				newMCPError(
					MCPErrorPromptNotFound,
					fmt.Sprintf("prompt not found: %s", params.Name),
					nil,
				),
			)
		}
		return jsonRPCResponseFromMCPError(
			id,
			InternalMCPError(err),
		)
	}

	return newJSONRPCResult(id, MCPPromptsUpdateResult{
		Name:        def.Name,
		Description: def.Description,
		Arguments:   def.Arguments,
	})
}

// jsonRPCResponseFromMCPError memetakan MCP taxonomy ke JSON-RPC.
func jsonRPCResponseFromMCPError(
	id interface{},
	err error,
) *JSONRPCResponse {
	code := MCPErrorCodeOf(err)

	switch code {
	case MCPErrorInvalidRequest:
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			err.Error(),
			nil,
		)

	case MCPErrorToolNotFound:
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			err.Error(),
			nil,
		)

	case MCPErrorUnsupportedTool:
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			err.Error(),
			nil,
		)

	case MCPErrorResourceNotFound:
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			err.Error(),
			nil,
		)

	case MCPErrorPromptNotFound:
		return newJSONRPCError(
			id,
			JSONRPCInvalidParams,
			err.Error(),
			nil,
		)

	case MCPErrorForbidden:
		// -32600 (InvalidRequest), BUKAN -32602 (InvalidParams) â€”
		// menandakan request ITU SENDIRI tidak diizinkan (siapa yang
		// memanggil), bukan argumen yang salah bentuk. Tidak ada kode
		// JSON-RPC standar untuk "forbidden" (JSON-RPC 2.0 tidak define
		// analog HTTP 403), jadi ini pemetaan terbaik yang tersedia di
		// 4 kode standar yang dipakai codebase ini.
		return newJSONRPCError(
			id,
			JSONRPCInvalidRequest,
			err.Error(),
			nil,
		)

	default:
		return newJSONRPCError(
			id,
			JSONRPCInternalError,
			"internal error",
			nil,
		)
	}
}

// writeJSONRPCResponse menulis JSON-RPC response.
func writeJSONRPCResponse(
	w http.ResponseWriter,
	response *JSONRPCResponse,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(response)
}

// writeJSONRPCError menulis protocol-level JSON-RPC error.
func writeJSONRPCError(
	w http.ResponseWriter,
	id interface{},
	code int,
	message string,
	data interface{},
) {
	writeJSONRPCResponse(
		w,
		newJSONRPCError(
			id,
			code,
			message,
			data,
		),
	)
}
