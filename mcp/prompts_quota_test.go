package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// ============================================================
// KUOTA PADA prompts/get
//
// usage_handlers.go menghitung prompts/get sebagai event billable, tapi
// handlePromptsGet tidak pernah memanggil TryConsumeQuota — jadi metode
// yang ditagih tidak pernah mengurangi kuota siapa pun. Pelanggan bisa
// memanggilnya tanpa batas pada paket apa pun.
//
// Test di bawah mengunci tiga hal sekaligus:
//
//  1. prompts/get yang SUKSES memotong tepat satu unit kuota.
//  2. prompts/get yang GAGAL tidak memotong apa pun. Ini bukan detail:
//     kuota dipotong setelah validasi, bukan sebelumnya, supaya penghitung
//     kuota dan laporan pemakaian tidak pernah berbeda. Kalau nanti ada
//     yang memindahkan pemotongan ke awal fungsi, test ini yang berteriak.
//  3. Kuota habis menghasilkan error, bukan prompt.
//
// Helper openGatewayQuotaDB / newGatewayWithQuotaDB dan mockQuotaEnforcer
// dipinjam dari gateway_quota_test.go — package yang sama.
//
//	go test ./mcp -run PromptsGetQuota -v
// ============================================================

const quotaTestPromptName = "audit-investigate"

func newServerWithQuota(t *testing.T, enforcer *mockQuotaEnforcer) *Server {
	t.Helper()
	db, cleanup := openGatewayQuotaDB(t)
	t.Cleanup(cleanup)

	gw := newGatewayWithQuotaDB(t, db)
	if enforcer != nil {
		gw.SetQuotaEnforcer(enforcer)
	}
	return NewServer(gw)
}

// promptsGetParams mengisi SEMUA argumen audit-investigate (resource,
// date_range) supaya jalur sukses tidak tersandung validasi argumen.
func promptsGetParams(t *testing.T, name string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(MCPPromptsGetParams{
		Name: name,
		Arguments: map[string]string{
			"resource":   "evt_01HQ",
			"date_range": "2026-11-01..2026-11-02",
		},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return raw
}

// responseJSON mengembalikan response sebagai map mentah. Sengaja lewat
// JSON, bukan lewat field struct, supaya test ini tidak ikut pecah kalau
// bentuk internal JSONRPCResponse berubah — yang dijanjikan ke client
// adalah bentuk JSON-nya.
func responseJSON(t *testing.T, resp *JSONRPCResponse) map[string]json.RawMessage {
	t.Helper()
	if resp == nil {
		t.Fatal("handlePromptsGet mengembalikan nil response")
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return out
}

func TestPromptsGetQuota_SuccessConsumesExactlyOne(t *testing.T) {
	enforcer := &mockQuotaEnforcer{allowed: true, remaining: 99}
	s := newServerWithQuota(t, enforcer)

	resp := s.handlePromptsGet("tenant-1", 1, promptsGetParams(t, quotaTestPromptName))

	if _, hasErr := responseJSON(t, resp)["error"]; hasErr {
		body, _ := json.Marshal(resp)
		t.Fatalf("prompts/get yang valid malah error: %s", body)
	}
	if enforcer.calls != 1 {
		t.Errorf("TryConsumeQuota dipanggil %d kali, harusnya tepat 1", enforcer.calls)
	}
	if enforcer.lastID != "tenant-1" {
		t.Errorf("tenantID yang dipotong = %q, harusnya %q", enforcer.lastID, "tenant-1")
	}
	if enforcer.lastN != 1 {
		t.Errorf("unit yang dipotong = %d, harusnya 1", enforcer.lastN)
	}
}

func TestPromptsGetQuota_UnknownPromptConsumesNothing(t *testing.T) {
	enforcer := &mockQuotaEnforcer{allowed: true, remaining: 99}
	s := newServerWithQuota(t, enforcer)

	resp := s.handlePromptsGet("tenant-1", 1, promptsGetParams(t, "prompt-yang-tidak-ada"))

	if _, hasErr := responseJSON(t, resp)["error"]; !hasErr {
		t.Fatal("nama prompt yang tidak ada harus menghasilkan error")
	}
	if enforcer.calls != 0 {
		t.Errorf("kuota dipotong %d kali untuk permintaan yang gagal; harusnya 0. "+
			"Pemotongan kuota harus SETELAH validasi, bukan sebelumnya — kalau tidak, "+
			"pelanggan yang salah ketik nama prompt kehilangan kuota tanpa ada event "+
			"yang muncul di laporan pemakaiannya.", enforcer.calls)
	}
}

func TestPromptsGetQuota_MissingNameConsumesNothing(t *testing.T) {
	enforcer := &mockQuotaEnforcer{allowed: true, remaining: 99}
	s := newServerWithQuota(t, enforcer)

	resp := s.handlePromptsGet("tenant-1", 1, json.RawMessage(`{"name":""}`))

	if _, hasErr := responseJSON(t, resp)["error"]; !hasErr {
		t.Fatal("name kosong harus menghasilkan error")
	}
	if enforcer.calls != 0 {
		t.Errorf("kuota dipotong %d kali untuk params yang invalid; harusnya 0", enforcer.calls)
	}
}

func TestPromptsGetQuota_ExhaustedQuotaIsRejected(t *testing.T) {
	enforcer := &mockQuotaEnforcer{allowed: false, remaining: 0}
	s := newServerWithQuota(t, enforcer)

	resp := s.handlePromptsGet("tenant-1", 1, promptsGetParams(t, quotaTestPromptName))

	body := responseJSON(t, resp)
	if _, hasErr := body["error"]; !hasErr {
		t.Fatal("kuota habis harus menghasilkan error, bukan prompt")
	}
	if _, hasResult := body["result"]; hasResult {
		t.Error("kuota habis tidak boleh mengembalikan result")
	}

	raw, _ := json.Marshal(resp)
	if !strings.Contains(strings.ToLower(string(raw)), "quota") {
		t.Errorf("error kuota harus menyebut quota supaya client bisa membedakannya "+
			"dari error lain; dapat: %s", raw)
	}
	if enforcer.calls != 1 {
		t.Errorf("TryConsumeQuota dipanggil %d kali, harusnya 1", enforcer.calls)
	}
}

func TestPromptsGetQuota_NilEnforcerDoesNotBlock(t *testing.T) {
	// SetQuotaEnforcer tidak dipanggil — pola nil-safe yang sama seperti
	// Gateway.Query, supaya test lama dan instalasi tanpa kuota tetap jalan.
	s := newServerWithQuota(t, nil)

	resp := s.handlePromptsGet("tenant-1", 1, promptsGetParams(t, quotaTestPromptName))

	if _, hasErr := responseJSON(t, resp)["error"]; hasErr {
		body, _ := json.Marshal(resp)
		t.Fatalf("enforcer nil tidak boleh memblokir prompts/get: %s", body)
	}
}
