// rbac/tenant_boundary.go
package rbac

import (
	"fmt"

	"trailseal/core/capability"
	"trailseal/sdk"
)

// ============================================================
// TENANT BOUNDARY CHECK
//
// Diekstrak dari pola yang SUDAH TERVERIFIKASI PASS di
// tests/integration/multi_tenant_test.go (TryAccessTenantData,
// TryAccessTenantCapability), dijadikan package reusable untuk
// dipakai RBAC di jalur produksi (bukan hanya test).
//
// TIDAK mengubah core/permission atau core/capability.
// HANYA memanggil capability.Engine yang sudah ada, DENGAN
// mengelilingi bug Capabilities[0] (lihat CATATAN di bawah).
// ============================================================

type AccessDenialReason string

const (
	ReasonTenantMismatch   AccessDenialReason = "tenant_mismatch"
	ReasonCapabilityDenied AccessDenialReason = "capability_denied"
)

type AccessError struct {
	Reason  AccessDenialReason
	Message string
}

func (e *AccessError) Error() string {
	return fmt.Sprintf("%s: %s", e.Reason, e.Message)
}

// CheckTenantBoundary memverifikasi bahwa capability yang diminta
// (requestedCapability) berada dalam boundary tenant milik caller
// (callerTenantID), SEBELUM capability matching dilakukan.
//
// Ini mengimplementasikan Section 18 (Resource Tenant Boundary):
// cross-tenant harus ditolak dengan reason "tenant_mismatch",
// BUKAN "capability_denied" — dua kegagalan mode yang berbeda.
//
// ============================================================
// ⚠️ CATATAN PENTING — BUKAN PROTEKSI BOUNDARY YANG BERDIRI SENDIRI:
//
// callerTenantID di sini SELALU datang dari claims.TenantID (lewat
// middleware/auth_middleware.go RequireCapability), dan
// requestedTenantID diekstrak balik dari requestedCapability —
// yang mana requestedCapability ITU SENDIRI dibentuk dengan
// mensubstitusi placeholder "{tenant_id}" dengan claims.TenantID
// yang SAMA. Akibatnya perbandingan callerTenantID != requestedTenantID
// di fungsi ini TIDAK PERNAH bisa gagal ketika dipanggil lewat jalur
// RequireCapability/CheckAccess — kedua sisi selalu berasal dari
// sumber yang sama.
//
// Fungsi ini TIDAK memverifikasi apakah caller mengakses resource
// tenant lain lewat parameter request (mis. :id di URL path). Itu
// tanggung jawab middleware terpisah, middleware.RequireTenantScope
// (lihat middleware/tenant_scope.go), yang WAJIB dipasang di depan
// RequireCapability untuk setiap endpoint tenant-scoped.
//
// Endpoint baru yang hanya mengandalkan RequireCapability tanpa
// RequireTenantScope di depannya TIDAK terlindungi dari cross-tenant
// access lewat manipulasi path parameter. Jangan anggap fungsi ini
// sebagai pengganti RequireTenantScope.
// ============================================================
func CheckTenantBoundary(
	callerTenantID string,
	requestedCapability string,
) *AccessError {
	// Konvensi capability string di repo ini: "tenant.<tenantID>.rest..."
	// Ekstrak tenantID dari requested capability.
	requestedTenantID, err := extractTenantIDFromCapability(requestedCapability)
	if err != nil {
		return &AccessError{
			Reason:  ReasonCapabilityDenied,
			Message: fmt.Sprintf("malformed capability: %v", err),
		}
	}

	if callerTenantID != requestedTenantID {
		return &AccessError{
			Reason: ReasonTenantMismatch,
			Message: fmt.Sprintf(
				"tenant %s cannot access resource scoped to tenant %s",
				callerTenantID,
				requestedTenantID,
			),
		}
	}

	return nil // boundary OK, lanjut ke capability matching
}

// CheckCapabilityGrant memverifikasi apakah salah satu grantedCapabilities
// (dari Role, lihat rbac/role.go) cocok dengan requestedCapability,
// memakai capability.Engine yang SUDAH ADA (WildcardMatcher).
//
// CATATAN PENTING soal bug core/capability/engine.go HasCapability():
// method itu hanya cek Capabilities[0] (comment "perlu di-loop" belum
// diperbaiki, dan SENGAJA TIDAK DIPERBAIKI di sini sesuai Core Freeze
// policy). Karena itu, fungsi ini TIDAK memanggil HasCapability() —
// melainkan MatchCapability() satu per satu dalam loop manual di sini,
// supaya SEMUA grantedCapabilities benar-benar dicek, bukan cuma yang
// pertama.
func CheckCapabilityGrant(
	engine *capability.Engine,
	grantedCapabilities []string,
	requestedCapability string,
) *AccessError {
	for _, granted := range grantedCapabilities {
		if engine.MatchCapability(requestedCapability, granted) {
			return nil // GRANTED
		}
	}
	return &AccessError{
		Reason: ReasonCapabilityDenied,
		Message: fmt.Sprintf(
			"capability %s not covered by any granted pattern",
			requestedCapability,
		),
	}
}

// CheckAccess adalah entry point gabungan: tenant boundary DULU,
// baru capability matching. Ini menegakkan Section 11 (Security Order)
// dan Section 30 (Security Gate): admin wildcard TIDAK BOLEH menembus
// tenant boundary — urutan ini yang menjaminnya, karena tenant boundary
// dicek SEBELUM wildcard capability matching sempat dievaluasi.
func CheckAccess(
	engine *capability.Engine,
	callerTenantID string,
	grantedCapabilities []string,
	requestedCapability string,
) *AccessError {
	// STEP 1: Tenant boundary check — WAJIB LEBIH DULU.
	if err := CheckTenantBoundary(callerTenantID, requestedCapability); err != nil {
		return err
	}

	// STEP 2: Capability grant check — hanya sampai sini kalau
	// tenant boundary sudah lolos.
	return CheckCapabilityGrant(engine, grantedCapabilities, requestedCapability)
}

func extractTenantIDFromCapability(cap string) (string, error) {
	const prefix = "tenant."
	if len(cap) <= len(prefix) || cap[:len(prefix)] != prefix {
		return "", fmt.Errorf("capability %q does not follow tenant.<id>.* convention", cap)
	}
	rest := cap[len(prefix):]
	for i, ch := range rest {
		if ch == '.' {
			return rest[:i], nil
		}
	}
	return "", fmt.Errorf("capability %q missing segment after tenant id", cap)
}

var _ = sdk.Manifest{} // placeholder import guard, hapus jika sdk dipakai langsung nanti
