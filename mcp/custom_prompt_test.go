package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// executeServerRequestAs mirrors executeServerRequest (server_test.go)
// but additionally sets X-Role-ID/X-Plan-Tier — needed for
// prompts/create, prompts/delete, prompts/update tests. A separate
// helper (not a change to executeServerRequest's signature) so none
// of the existing call sites in server_test.go need to change.
func executeServerRequestAs(
	t *testing.T,
	server *Server,
	tenantID string,
	userID string,
	roleID string,
	planTier string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(
		http.MethodPost,
		"/mcp",
		bytes.NewBufferString(body),
	)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Role-ID", roleID)
	req.Header.Set("X-Plan-Tier", planTier)

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)

	return recorder
}

// newServerWithCustomPrompts membuat Server dengan BoltCustomPromptStore
// terpasang lewat SetCustomPromptStore, memakai db yang sama dengan
// gateway test (bbolt mendukung banyak bucket dalam satu db).
func newServerWithCustomPrompts(t *testing.T) *Server {
	t.Helper()

	gateway, db := newTestGateway(t)
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewBoltCustomPromptStore(db)
	if err != nil {
		t.Fatalf("failed to create custom prompt store: %v", err)
	}

	server := NewServer(gateway)
	server.SetCustomPromptStore(store)
	return server
}

func TestPromptsCreate_OwnerBusinessTier_Succeeds(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{
				"name":"weekly-digest",
				"description":"Ringkas aktivitas minggu ini",
				"template":"Ringkas semua aktivitas tenant minggu ini."
			}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error != nil {
		t.Fatalf("expected success, got error: %+v", response.Error)
	}
}

func TestPromptsCreate_NonOwner_Forbidden(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "member", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{
				"name":"weekly-digest",
				"description":"x",
				"template":"x"
			}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected forbidden error for non-owner, got success")
	}
	if response.Error.Code != JSONRPCInvalidRequest {
		t.Fatalf("expected JSONRPCInvalidRequest (-32600), got %d", response.Error.Code)
	}
}

func TestPromptsCreate_NonBusinessTier_Forbidden(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "pro",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{
				"name":"weekly-digest",
				"description":"x",
				"template":"x"
			}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected forbidden error for non-business tier, got success")
	}
	if response.Error.Code != JSONRPCInvalidRequest {
		t.Fatalf("expected JSONRPCInvalidRequest (-32600), got %d", response.Error.Code)
	}
}

func TestPromptsCreate_CannotShadowBuiltinName(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{
				"name":"audit-investigate",
				"description":"x",
				"template":"x"
			}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected error when shadowing a built-in prompt name")
	}
	if response.Error.Code != JSONRPCInvalidParams {
		t.Fatalf("expected JSONRPCInvalidParams (-32602), got %d", response.Error.Code)
	}
}

// TestPromptsGet_CustomPrompt_CannotEscapeTenant mengikuti gaya
// TestServer_ToolsCall_CannotEscapeTenant (server_test.go): tenant-a
// membuat custom prompt, tenant-b TIDAK boleh bisa prompts/get itu.
func TestPromptsGet_CustomPrompt_CannotEscapeTenant(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	createRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{
				"name":"tenant-a-only",
				"description":"x",
				"template":"x"
			}
		}`,
	)
	createResponse := decodeJSONRPCResponse(t, createRecorder)
	if createResponse.Error != nil {
		t.Fatalf("setup failed: could not create prompt: %+v", createResponse.Error)
	}

	// tenant-b (walau juga Business, owner) mencoba prompts/get nama
	// yang sama — HARUS gagal seperti prompt tidak pernah ada.
	getRecorder := executeServerRequestAs(
		t, server, "tenant-b", "user-b", "owner", "business",
		`{
			"jsonrpc":"2.0","id":2,"method":"prompts/get",
			"params":{"name":"tenant-a-only"}
		}`,
	)
	getResponse := decodeJSONRPCResponse(t, getRecorder)

	if getResponse.Error == nil {
		t.Fatal("expected prompt_not_found when tenant-b reads tenant-a's custom prompt")
	}
	if getResponse.Error.Code != JSONRPCInvalidParams {
		t.Fatalf("expected JSONRPCInvalidParams (-32602), got %d", getResponse.Error.Code)
	}

	// tenant-a sendiri HARUS tetap bisa mengambilnya.
	ownRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":3,"method":"prompts/get",
			"params":{"name":"tenant-a-only"}
		}`,
	)
	ownResponse := decodeJSONRPCResponse(t, ownRecorder)
	if ownResponse.Error != nil {
		t.Fatalf("tenant-a should still be able to read its own custom prompt: %+v", ownResponse.Error)
	}
}

// TestPromptsList_MergesBuiltinAndCustom_ScopedPerTenant memverifikasi
// prompts/list menggabungkan built-in (semua tenant) + custom (hanya
// milik tenant pemanggil).
func TestPromptsList_MergesBuiltinAndCustom_ScopedPerTenant(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	createRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{
				"name":"only-tenant-a",
				"description":"x",
				"template":"x"
			}
		}`,
	)
	if resp := decodeJSONRPCResponse(t, createRecorder); resp.Error != nil {
		t.Fatalf("setup failed: %+v", resp.Error)
	}

	// tenant-b: list HARUS berisi 3 built-in TAPI TIDAK "only-tenant-a".
	listRecorder := executeServerRequestAs(
		t, server, "tenant-b", "user-b", "owner", "business",
		`{"jsonrpc":"2.0","id":2,"method":"prompts/list","params":{}}`,
	)
	listResponse := decodeJSONRPCResponse(t, listRecorder)
	if listResponse.Error != nil {
		t.Fatalf("unexpected error: %+v", listResponse.Error)
	}

	// Result terdecode sebagai interface{} (map[string]interface{})
	// oleh decodeJSONRPCResponse — marshal ulang lalu decode ke tipe
	// konkret, satu-satunya cara aman menavigasi interface{} generik.
	raw, err := json.Marshal(listResponse.Result)
	if err != nil {
		t.Fatalf("failed to re-marshal result: %v", err)
	}
	var result MCPPromptsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("failed to decode prompts/list result: %v", err)
	}

	if len(result.Prompts) != 3 {
		t.Fatalf("expected exactly 3 built-in prompts for tenant-b (no custom), got %d", len(result.Prompts))
	}
	for _, p := range result.Prompts {
		if p.Name == "only-tenant-a" {
			t.Fatal("tenant-b must NOT see tenant-a's custom prompt in prompts/list")
		}
	}
}

// ============================================================
// prompts/delete
// ============================================================

func TestPromptsDelete_OwnerBusinessTier_Succeeds(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	createRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{"name":"to-delete","description":"x","template":"x"}
		}`,
	)
	if resp := decodeJSONRPCResponse(t, createRecorder); resp.Error != nil {
		t.Fatalf("setup failed: could not create prompt: %+v", resp.Error)
	}

	deleteRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":2,"method":"prompts/delete",
			"params":{"name":"to-delete"}
		}`,
	)
	deleteResponse := decodeJSONRPCResponse(t, deleteRecorder)
	if deleteResponse.Error != nil {
		t.Fatalf("expected successful delete, got error: %+v", deleteResponse.Error)
	}

	// Prompt harus benar-benar hilang setelahnya — bukan cuma
	// response sukses palsu.
	getRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":3,"method":"prompts/get",
			"params":{"name":"to-delete"}
		}`,
	)
	getResponse := decodeJSONRPCResponse(t, getRecorder)
	if getResponse.Error == nil {
		t.Fatal("expected prompt_not_found after delete, got success")
	}
}

func TestPromptsDelete_NonOwner_Forbidden(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "member", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/delete",
			"params":{"name":"anything"}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected forbidden error for non-owner, got success")
	}
	if response.Error.Code != JSONRPCInvalidRequest {
		t.Fatalf("expected JSONRPCInvalidRequest (-32600), got %d", response.Error.Code)
	}
}

func TestPromptsDelete_NonBusinessTier_Forbidden(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "pro",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/delete",
			"params":{"name":"anything"}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected forbidden error for non-business tier, got success")
	}
	if response.Error.Code != JSONRPCInvalidRequest {
		t.Fatalf("expected JSONRPCInvalidRequest (-32600), got %d", response.Error.Code)
	}
}

func TestPromptsDelete_CannotDeleteBuiltinName(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/delete",
			"params":{"name":"audit-investigate"}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected error when deleting a built-in prompt name")
	}
	if response.Error.Code != JSONRPCInvalidParams {
		t.Fatalf("expected JSONRPCInvalidParams (-32602), got %d", response.Error.Code)
	}
}

func TestPromptsDelete_NotFound_ReturnsError(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/delete",
			"params":{"name":"never-existed"}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected prompt_not_found when deleting a name that was never created")
	}
	if response.Error.Code != JSONRPCInvalidParams {
		t.Fatalf("expected JSONRPCInvalidParams (-32602), got %d", response.Error.Code)
	}
}

// TestPromptsDelete_CannotEscapeTenant mengikuti gaya
// TestPromptsGet_CustomPrompt_CannotEscapeTenant: tenant-b tidak
// boleh bisa menghapus custom prompt milik tenant-a.
func TestPromptsDelete_CannotEscapeTenant(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	createRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{"name":"tenant-a-secret","description":"x","template":"x"}
		}`,
	)
	if resp := decodeJSONRPCResponse(t, createRecorder); resp.Error != nil {
		t.Fatalf("setup failed: could not create prompt: %+v", resp.Error)
	}

	// tenant-b (walau juga Business, owner) mencoba menghapus prompt
	// tenant-a — HARUS gagal seperti prompt tidak pernah ada.
	deleteRecorder := executeServerRequestAs(
		t, server, "tenant-b", "user-b", "owner", "business",
		`{
			"jsonrpc":"2.0","id":2,"method":"prompts/delete",
			"params":{"name":"tenant-a-secret"}
		}`,
	)
	deleteResponse := decodeJSONRPCResponse(t, deleteRecorder)
	if deleteResponse.Error == nil {
		t.Fatal("expected prompt_not_found when tenant-b deletes tenant-a's custom prompt")
	}

	// tenant-a sendiri HARUS tetap bisa mengambilnya (belum terhapus).
	getRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":3,"method":"prompts/get",
			"params":{"name":"tenant-a-secret"}
		}`,
	)
	getResponse := decodeJSONRPCResponse(t, getRecorder)
	if getResponse.Error != nil {
		t.Fatalf("tenant-a's prompt should survive tenant-b's failed delete attempt: %+v", getResponse.Error)
	}
}

// ============================================================
// prompts/update
// ============================================================

func TestPromptsUpdate_OwnerBusinessTier_Succeeds(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	createRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{"name":"to-update","description":"old","template":"old template"}
		}`,
	)
	if resp := decodeJSONRPCResponse(t, createRecorder); resp.Error != nil {
		t.Fatalf("setup failed: could not create prompt: %+v", resp.Error)
	}

	updateRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":2,"method":"prompts/update",
			"params":{"name":"to-update","description":"new","template":"new template"}
		}`,
	)
	updateResponse := decodeJSONRPCResponse(t, updateRecorder)
	if updateResponse.Error != nil {
		t.Fatalf("expected successful update, got error: %+v", updateResponse.Error)
	}

	// Verifikasi isi benar-benar berubah, bukan cuma response sukses
	// palsu — description baru harus muncul, bukan yang lama.
	getRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":3,"method":"prompts/get",
			"params":{"name":"to-update"}
		}`,
	)
	getResponse := decodeJSONRPCResponse(t, getRecorder)
	if getResponse.Error != nil {
		t.Fatalf("unexpected error: %+v", getResponse.Error)
	}
	raw, err := json.Marshal(getResponse.Result)
	if err != nil {
		t.Fatalf("failed to re-marshal result: %v", err)
	}
	var result MCPPromptsGetResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("failed to decode prompts/get result: %v", err)
	}
	if len(result.Messages) != 1 || result.Messages[0].Content.Text != "new template" {
		t.Fatalf("expected rendered template to reflect the update, got: %+v", result.Messages)
	}
}

func TestPromptsUpdate_NonOwner_Forbidden(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "member", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/update",
			"params":{"name":"anything","description":"x","template":"x"}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected forbidden error for non-owner, got success")
	}
	if response.Error.Code != JSONRPCInvalidRequest {
		t.Fatalf("expected JSONRPCInvalidRequest (-32600), got %d", response.Error.Code)
	}
}

func TestPromptsUpdate_NonBusinessTier_Forbidden(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "pro",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/update",
			"params":{"name":"anything","description":"x","template":"x"}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected forbidden error for non-business tier, got success")
	}
	if response.Error.Code != JSONRPCInvalidRequest {
		t.Fatalf("expected JSONRPCInvalidRequest (-32600), got %d", response.Error.Code)
	}
}

func TestPromptsUpdate_CannotUpdateBuiltinName(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/update",
			"params":{"name":"audit-investigate","description":"x","template":"x"}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected error when updating a built-in prompt name")
	}
	if response.Error.Code != JSONRPCInvalidParams {
		t.Fatalf("expected JSONRPCInvalidParams (-32602), got %d", response.Error.Code)
	}
}

func TestPromptsUpdate_NotFound_ReturnsError(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	// Update terhadap nama yang belum pernah dibuat HARUS gagal
	// prompt_not_found — TIDAK diam-diam berperilaku seperti create.
	recorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/update",
			"params":{"name":"never-existed","description":"x","template":"x"}
		}`,
	)

	response := decodeJSONRPCResponse(t, recorder)
	if response.Error == nil {
		t.Fatal("expected prompt_not_found when updating a name that was never created")
	}
	if response.Error.Code != JSONRPCInvalidParams {
		t.Fatalf("expected JSONRPCInvalidParams (-32602), got %d", response.Error.Code)
	}

	// Konfirmasi tambahan: prompt itu benar-benar TIDAK tercipta
	// sebagai efek samping dari percobaan update yang gagal.
	getRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":2,"method":"prompts/get",
			"params":{"name":"never-existed"}
		}`,
	)
	getResponse := decodeJSONRPCResponse(t, getRecorder)
	if getResponse.Error == nil {
		t.Fatal("failed update must NOT silently create the prompt")
	}
}

// TestPromptsUpdate_CannotEscapeTenant mengikuti gaya
// TestPromptsDelete_CannotEscapeTenant: tenant-b tidak boleh bisa
// mengupdate custom prompt milik tenant-a.
func TestPromptsUpdate_CannotEscapeTenant(t *testing.T) {
	server := newServerWithCustomPrompts(t)

	createRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":1,"method":"prompts/create",
			"params":{"name":"tenant-a-secret","description":"original","template":"original template"}
		}`,
	)
	if resp := decodeJSONRPCResponse(t, createRecorder); resp.Error != nil {
		t.Fatalf("setup failed: could not create prompt: %+v", resp.Error)
	}

	// tenant-b (walau juga Business, owner) mencoba mengupdate prompt
	// tenant-a — HARUS gagal seperti prompt tidak pernah ada.
	updateRecorder := executeServerRequestAs(
		t, server, "tenant-b", "user-b", "owner", "business",
		`{
			"jsonrpc":"2.0","id":2,"method":"prompts/update",
			"params":{"name":"tenant-a-secret","description":"hijacked","template":"hijacked template"}
		}`,
	)
	updateResponse := decodeJSONRPCResponse(t, updateRecorder)
	if updateResponse.Error == nil {
		t.Fatal("expected prompt_not_found when tenant-b updates tenant-a's custom prompt")
	}

	// tenant-a sendiri HARUS tetap melihat isi ASLI (tidak berubah).
	getRecorder := executeServerRequestAs(
		t, server, "tenant-a", "user-a", "owner", "business",
		`{
			"jsonrpc":"2.0","id":3,"method":"prompts/get",
			"params":{"name":"tenant-a-secret"}
		}`,
	)
	getResponse := decodeJSONRPCResponse(t, getRecorder)
	if getResponse.Error != nil {
		t.Fatalf("unexpected error: %+v", getResponse.Error)
	}
	raw, err := json.Marshal(getResponse.Result)
	if err != nil {
		t.Fatalf("failed to re-marshal result: %v", err)
	}
	var result MCPPromptsGetResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("failed to decode prompts/get result: %v", err)
	}
	if len(result.Messages) != 1 || result.Messages[0].Content.Text != "original template" {
		t.Fatalf("tenant-a's prompt must survive tenant-b's failed update attempt unchanged, got: %+v", result.Messages)
	}
}