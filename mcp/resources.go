package mcp

import (
	"encoding/json"
	"fmt"
	"time"
)

// ============================================================
// MCP RESOURCES
//
// Keputusan produk: Resources TrailSeal mengekspos audit log tenant
// (read-only) lewat auditStore yang SUDAH ADA di Gateway — bukan
// entity/store baru. Satu resource statis per tenant (bukan satu
// resource per event) supaya tetap simpel dan cacheable, sesuai
// prioritas primitive MCP bagian 2 (keputusan final).
// ============================================================

const auditLogResourceURI = "trailseal://audit-log"

// auditLogResourceReadLimit membatasi berapa banyak audit event yang
// dikembalikan lewat resources/read — nilainya sama dengan
// default/max yang sudah dipakai executeAuditLookup (tool
// audit_lookup) supaya perilakunya konsisten antar dua jalur akses
// audit log yang sekarang ada.
const auditLogResourceReadLimit = 50

// MCPResourceDefinition adalah representasi resource pada resources/list.
type MCPResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// MCPResourcesListParams adalah params resources/list.
type MCPResourcesListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// MCPResourcesListResult adalah result resources/list.
type MCPResourcesListResult struct {
	Resources  []MCPResourceDefinition `json:"resources"`
	NextCursor string                  `json:"nextCursor,omitempty"`
}

// MCPResourcesReadParams adalah params resources/read.
type MCPResourcesReadParams struct {
	URI string `json:"uri"`
}

// MCPResourceContent adalah satu content block hasil resources/read.
type MCPResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// MCPResourcesReadResult adalah result resources/read.
type MCPResourcesReadResult struct {
	Contents []MCPResourceContent `json:"contents"`
}

// buildMCPResourcesList membuat daftar resource yang tersedia.
//
// Statis untuk saat ini — hanya 1 resource (audit log tenant). Tidak
// butuh tenantID di sini karena descriptor-nya sama untuk semua
// tenant; isi datanya baru tenant-scoped saat resources/read.
func buildMCPResourcesList() MCPResourcesListResult {
	return MCPResourcesListResult{
		Resources: []MCPResourceDefinition{
			{
				URI:         auditLogResourceURI,
				Name:        "Audit Log",
				Description: "Recent audit trail events for this tenant (read-only).",
				MimeType:    "application/json",
			},
		},
	}
}

// readResource membaca isi resource berdasarkan URI, dibatasi ketat
// pada tenant yang authenticated (tenantID authoritative dari HTTP
// layer, BUKAN dari params — sama seperti discoverTools/executeTool).
func (g *Gateway) readResource(
	tenantID string,
	uri string,
) (MCPResourcesReadResult, error) {
	if g == nil || g.auditStore == nil {
		return MCPResourcesReadResult{}, InternalMCPError(
			fmt.Errorf("mcp gateway is not initialized"),
		)
	}

	if tenantID == "" {
		return MCPResourcesReadResult{}, newMCPError(
			MCPErrorInvalidRequest,
			"tenant_id is required",
			nil,
		)
	}

	// Quota check di titik yang sama seperti Gateway.Query() (sebelum
	// resource di-resolve) — konsisten dengan pola existing, resources/read
	// adalah billable event (keputusan final bagian 4) jadi harus tunduk
	// pada hard-limit kuota bulanan yang sama seperti tools/call.
	if g.quota != nil {
		allowed, _, err := g.quota.TryConsumeQuota(tenantID, 1)
		if err != nil {
			return MCPResourcesReadResult{}, InternalMCPError(
				fmt.Errorf("quota check failed: %w", err),
			)
		}
		if !allowed {
			return MCPResourcesReadResult{}, newMCPError(
				MCPErrorQuotaExceeded,
				"monthly event quota exceeded",
				nil,
			)
		}
	}

	switch uri {
	case auditLogResourceURI:
		return g.readAuditLogResource(tenantID)
	default:
		return MCPResourcesReadResult{}, newMCPError(
			MCPErrorResourceNotFound,
			fmt.Sprintf("resource not found: %s", uri),
			nil,
		)
	}
}

// readAuditLogResource mengembalikan isi audit log tenant sebagai
// satu content block JSON. Query selalu di-scope ke tenantID yang
// diberikan — tidak ada global lookup.
func (g *Gateway) readAuditLogResource(
	tenantID string,
) (MCPResourcesReadResult, error) {
	records, err := g.auditStore.ListForTenant(tenantID, auditLogResourceReadLimit)
	if err != nil {
		return MCPResourcesReadResult{}, InternalMCPError(
			fmt.Errorf("failed to read audit log: %w", err),
		)
	}

	events := make([]map[string]interface{}, 0, len(records))
	for _, e := range records {
		if e == nil {
			continue
		}
		events = append(events, map[string]interface{}{
			"action":    e.Action,
			"outcome":   string(e.Outcome),
			"timestamp": e.Timestamp.Format(time.RFC3339),
			"resource":  e.Resource,
		})
	}

	body, err := json.Marshal(map[string]interface{}{
		"events":      events,
		"total_count": len(events),
	})
	if err != nil {
		return MCPResourcesReadResult{}, InternalMCPError(
			fmt.Errorf("failed to encode audit log: %w", err),
		)
	}

	return MCPResourcesReadResult{
		Contents: []MCPResourceContent{
			{
				URI:      auditLogResourceURI,
				MimeType: "application/json",
				Text:     string(body),
			},
		},
	}, nil
}