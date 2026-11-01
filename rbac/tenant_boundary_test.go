package rbac

import (
	"testing"
)

// ============================================================
// TEST CheckTenantBoundary
// ============================================================

func TestCheckTenantBoundary_SameTenant_Allowed(t *testing.T) {
	err := CheckTenantBoundary("tenant-a", "tenant.tenant-a.agent.read")
	if err != nil {
		t.Errorf("expected nil (boundary OK), got %v", err)
	}
}

func TestCheckTenantBoundary_CrossTenant_Denied(t *testing.T) {
	err := CheckTenantBoundary("tenant-a", "tenant.tenant-b.agent.read")
	if err == nil {
		t.Fatal("expected tenant_mismatch error, got nil")
	}
	if err.Reason != ReasonTenantMismatch {
		t.Errorf("expected reason %q, got %q", ReasonTenantMismatch, err.Reason)
	}
}

func TestCheckTenantBoundary_MalformedCapability_NoPrefix(t *testing.T) {
	err := CheckTenantBoundary("tenant-a", "invalid-capability-string")
	if err == nil {
		t.Fatal("expected capability_denied error for malformed capability, got nil")
	}
	if err.Reason != ReasonCapabilityDenied {
		t.Errorf("expected reason %q, got %q", ReasonCapabilityDenied, err.Reason)
	}
}

func TestCheckTenantBoundary_MalformedCapability_MissingSegmentAfterTenant(t *testing.T) {
	err := CheckTenantBoundary("tenant-a", "tenant.tenant-a")
	if err == nil {
		t.Fatal("expected capability_denied error for missing segment, got nil")
	}
	if err.Reason != ReasonCapabilityDenied {
		t.Errorf("expected reason %q, got %q", ReasonCapabilityDenied, err.Reason)
	}
}

func TestCheckTenantBoundary_EmptyCallerTenantID(t *testing.T) {
	err := CheckTenantBoundary("", "tenant.tenant-a.agent.read")
	if err == nil {
		t.Fatal("expected tenant_mismatch error when caller tenant ID is empty, got nil")
	}
	if err.Reason != ReasonTenantMismatch {
		t.Errorf("expected reason %q, got %q", ReasonTenantMismatch, err.Reason)
	}
}

func TestCheckTenantBoundary_EmptyRequestedCapability(t *testing.T) {
	err := CheckTenantBoundary("tenant-a", "")
	if err == nil {
		t.Fatal("expected capability_denied error for empty capability, got nil")
	}
	if err.Reason != ReasonCapabilityDenied {
		t.Errorf("expected reason %q, got %q", ReasonCapabilityDenied, err.Reason)
	}
}

// ============================================================
// TEST extractTenantIDFromCapability (via CheckTenantBoundary,
// since it's unexported — tested indirectly through its only
// caller, which is the standard Go convention for unexported
// helpers)
// ============================================================

func TestCheckTenantBoundary_ExtractsMultiSegmentTenantIDCorrectly(t *testing.T) {
	// tenant ID itself should stop at the first '.' after "tenant."
	// even if the rest of the capability has many segments.
	err := CheckTenantBoundary("tenant-a", "tenant.tenant-a.module.recon.read.deep")
	if err != nil {
		t.Errorf("expected nil (boundary OK) for matching multi-segment capability, got %v", err)
	}
}

func TestCheckTenantBoundary_DoesNotConfuseSimilarTenantIDPrefixes(t *testing.T) {
	// "tenant-a" vs "tenant-ab" must NOT be treated as the same tenant
	// just because one is a string-prefix of the other.
	err := CheckTenantBoundary("tenant-a", "tenant.tenant-ab.agent.read")
	if err == nil {
		t.Fatal("expected tenant_mismatch for similar-but-different tenant IDs, got nil")
	}
	if err.Reason != ReasonTenantMismatch {
		t.Errorf("expected reason %q, got %q", ReasonTenantMismatch, err.Reason)
	}
}

// ============================================================
// TEST AccessError.Error()
// ============================================================

func TestAccessError_ErrorStringIncludesReasonAndMessage(t *testing.T) {
	err := &AccessError{
		Reason:  ReasonTenantMismatch,
		Message: "some detail",
	}
	got := err.Error()
	want := "tenant_mismatch: some detail"
	if got != want {
		t.Errorf("expected error string %q, got %q", want, got)
	}
}