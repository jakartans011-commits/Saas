package rbac

import (
	"testing"

	"trailseal/core/capability"
)

// ============================================================
// TEST CheckCapabilityGrant
// ============================================================

func TestCheckCapabilityGrant_ExactMatch_Allowed(t *testing.T) {
	engine := capability.NewEngine()

	err := CheckCapabilityGrant(
		engine,
		[]string{"tenant.tenant-a.agent.read"},
		"tenant.tenant-a.agent.read",
	)
	if err != nil {
		t.Errorf("expected nil (granted), got %v", err)
	}
}

func TestCheckCapabilityGrant_NoGrants_Denied(t *testing.T) {
	engine := capability.NewEngine()

	err := CheckCapabilityGrant(
		engine,
		[]string{},
		"tenant.tenant-a.agent.read",
	)
	if err == nil {
		t.Fatal("expected capability_denied error when no grants present, got nil")
	}
	if err.Reason != ReasonCapabilityDenied {
		t.Errorf("expected reason %q, got %q", ReasonCapabilityDenied, err.Reason)
	}
}

func TestCheckCapabilityGrant_NoMatchingGrant_Denied(t *testing.T) {
	engine := capability.NewEngine()

	err := CheckCapabilityGrant(
		engine,
		[]string{"tenant.tenant-a.tool.read"},
		"tenant.tenant-a.agent.read",
	)
	if err == nil {
		t.Fatal("expected capability_denied error when no grant matches, got nil")
	}
	if err.Reason != ReasonCapabilityDenied {
		t.Errorf("expected reason %q, got %q", ReasonCapabilityDenied, err.Reason)
	}
}

func TestCheckCapabilityGrant_MatchAmongMultipleGrants_Allowed(t *testing.T) {
	engine := capability.NewEngine()

	// Requested capability matches the SECOND grant in the list —
	// verifies the loop checks all grants, not just Capabilities[0]
	// (this is the exact bug this function was written to avoid,
	// per the comment in tenant_boundary.go).
	err := CheckCapabilityGrant(
		engine,
		[]string{
			"tenant.tenant-a.tool.read",
			"tenant.tenant-a.agent.read",
		},
		"tenant.tenant-a.agent.read",
	)
	if err != nil {
		t.Errorf("expected nil (granted via second entry), got %v", err)
	}
}

func TestCheckCapabilityGrant_NilGrantedCapabilities_Denied(t *testing.T) {
	engine := capability.NewEngine()

	err := CheckCapabilityGrant(
		engine,
		nil,
		"tenant.tenant-a.agent.read",
	)
	if err == nil {
		t.Fatal("expected capability_denied error for nil grants, got nil")
	}
	if err.Reason != ReasonCapabilityDenied {
		t.Errorf("expected reason %q, got %q", ReasonCapabilityDenied, err.Reason)
	}
}

// ============================================================
// TEST CheckAccess (combined: tenant boundary FIRST, then
// capability grant — this enforces Section 11/30 order per the
// comment in tenant_boundary.go)
// ============================================================

func TestCheckAccess_SameTenantAndGranted_Allowed(t *testing.T) {
	engine := capability.NewEngine()

	err := CheckAccess(
		engine,
		"tenant-a",
		[]string{"tenant.tenant-a.agent.read"},
		"tenant.tenant-a.agent.read",
	)
	if err != nil {
		t.Errorf("expected nil (allowed), got %v", err)
	}
}

func TestCheckAccess_SameTenantButNotGranted_CapabilityDenied(t *testing.T) {
	engine := capability.NewEngine()

	err := CheckAccess(
		engine,
		"tenant-a",
		[]string{"tenant.tenant-a.tool.read"},
		"tenant.tenant-a.agent.read",
	)
	if err == nil {
		t.Fatal("expected capability_denied error, got nil")
	}
	if err.Reason != ReasonCapabilityDenied {
		t.Errorf("expected reason %q, got %q", ReasonCapabilityDenied, err.Reason)
	}
}

func TestCheckAccess_CrossTenant_TenantMismatchTakesPriorityOverCapability(t *testing.T) {
	engine := capability.NewEngine()

	// Caller is tenant-a, but the requested capability belongs to
	// tenant-b. Even though the caller happens to hold a grant that
	// would match if tenants matched, tenant boundary MUST be
	// checked first and MUST fail with tenant_mismatch, never
	// capability_denied. This is the core "admin wildcard cannot
	// cross tenant boundary" guarantee described in the file's
	// CheckAccess doc comment.
	err := CheckAccess(
		engine,
		"tenant-a",
		[]string{"tenant.tenant-b.agent.read"},
		"tenant.tenant-b.agent.read",
	)
	if err == nil {
		t.Fatal("expected tenant_mismatch error for cross-tenant access, got nil")
	}
	if err.Reason != ReasonTenantMismatch {
		t.Errorf("expected reason %q (tenant boundary must be checked first), got %q", ReasonTenantMismatch, err.Reason)
	}
}

func TestCheckAccess_CrossTenant_NoMatchingGrantEither_StillTenantMismatch(t *testing.T) {
	engine := capability.NewEngine()

	// Cross-tenant AND no matching grant at all — the error reason
	// must still be tenant_mismatch, not capability_denied, because
	// tenant boundary is checked before capability matching ever runs.
	err := CheckAccess(
		engine,
		"tenant-a",
		[]string{"tenant.tenant-a.agent.read"},
		"tenant.tenant-b.agent.read",
	)
	if err == nil {
		t.Fatal("expected tenant_mismatch error, got nil")
	}
	if err.Reason != ReasonTenantMismatch {
		t.Errorf("expected reason %q, got %q", ReasonTenantMismatch, err.Reason)
	}
}

func TestCheckAccess_MalformedRequestedCapability_CapabilityDenied(t *testing.T) {
	engine := capability.NewEngine()

	err := CheckAccess(
		engine,
		"tenant-a",
		[]string{"tenant.tenant-a.agent.read"},
		"not-a-valid-capability-string",
	)
	if err == nil {
		t.Fatal("expected capability_denied error for malformed capability, got nil")
	}
	if err.Reason != ReasonCapabilityDenied {
		t.Errorf("expected reason %q, got %q", ReasonCapabilityDenied, err.Reason)
	}
}
