package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v2"

	"trailseal/auth"
	"trailseal/billing"
	"trailseal/rbac"
	"trailseal/tenant"
)

// ============================================================
// CUSTOM ROLES (Business) — tabel pricing: RBAC Business = custom roles.
//
// - Capability dipilih dari katalog TETAP (bukan teks bebas).
// - Hanya owner yang boleh mengelola dan memberikan role kustom.
// - Turun dari Business: pemegang role kustom bertindak sebagai Member (Pro)
//   atau ditangguhkan (Free); definisi role tidak dihapus dan pulih saat upgrade.
// ============================================================

const (
	customRolePrefix        = "custom-"
	maxCustomRolesPerTenant = 20
)

// Dikeluarkan sengaja: billing.write, compliance.delete/purge/restore,
// users.create, apikeys.write, audit.retention.write, dan kelola role.
var customRoleCatalog = []string{
	"agent.read", "agent.write",
	"apikeys.read",
	"audit.export", "audit.read", "audit.retention.read",
	"compliance.read",
	"mcp.query",
	"notifications.read", "notifications.write",
	"projects.read", "projects.write",
	"security_events.read",
	"tool.execute", "tool.read", "tool.write",
	"usage.read",
}

var customRoleCatalogSet = func() map[string]bool {
	m := make(map[string]bool, len(customRoleCatalog))
	for _, c := range customRoleCatalog {
		m[c] = true
	}
	return m
}()

var customRoleNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{1,30}[A-Za-z0-9]$`)

var customRoleReservedNames = map[string]bool{"owner": true, "admin": true, "member": true}

func isCustomRoleID(id string) bool { return strings.HasPrefix(id, customRolePrefix) }

func customRoleSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	return strings.NewReplacer(" ", "-", "_", "-").Replace(s)
}

func customRoleGrants(tenantID string, caps []string) (grants []string, unknown string) {
	seen := make(map[string]bool, len(caps))
	for _, raw := range caps {
		c := strings.TrimSpace(raw)
		if !customRoleCatalogSet[c] {
			if c == "" {
				c = "(empty)"
			}
			return nil, c
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		grants = append(grants, "tenant."+tenantID+"."+c)
	}
	sort.Strings(grants)
	return grants, ""
}

func customRoleView(r *rbac.Role) fiber.Map {
	prefix := "tenant." + r.TenantID + "."
	caps := make([]string, 0, len(r.CapabilityGrants))
	for _, g := range r.CapabilityGrants {
		caps = append(caps, strings.TrimPrefix(g, prefix))
	}
	return fiber.Map{"id": r.ID, "name": r.Name, "capabilities": caps}
}

func customRoleErr(c *fiber.Ctx, status int, code string) error {
	return c.Status(status).JSON(fiber.Map{"error": code})
}

func customRoleUpgradeErr(c *fiber.Ctx) error {
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
		"error":         "plan_upgrade_required",
		"required_plan": "business",
		"message":       "Custom roles are available on the Business plan.",
	})
}

// ------------------------------------------------------------
// Dekorator RoleStore sadar-plan: dipasang di RoleResolver sehingga
// penangguhan berlaku saat request untuk SEMUA jalur downgrade.
// ------------------------------------------------------------

type customRoleTenantReader interface {
	Get(id string) (*tenant.Tenant, error)
}

type planAwareRoleStore struct {
	inner   rbac.RoleStore
	tenants customRoleTenantReader
}

func newPlanAwareRoleStore(inner rbac.RoleStore, tenants customRoleTenantReader) *planAwareRoleStore {
	return &planAwareRoleStore{inner: inner, tenants: tenants}
}

func (s *planAwareRoleStore) PutRole(role *rbac.Role) error { return s.inner.PutRole(role) }

func (s *planAwareRoleStore) GetRole(tenantID, roleID string) (*rbac.Role, error) {
	if !isCustomRoleID(roleID) {
		return s.inner.GetRole(tenantID, roleID)
	}
	tn, err := s.tenants.Get(tenantID)
	if err != nil {
		// Fail closed: tenant tidak terbaca = role kustom tidak berlaku.
		return nil, fmt.Errorf("role_resolution_failed: tenant %s unreadable for custom role %s", tenantID, roleID)
	}
	if billing.PlanAllowsCustomRoles(tn.PlanTier) {
		return s.inner.GetRole(tenantID, roleID)
	}
	if billing.PlanAllowsTeamSeats(tn.PlanTier) {
		// Pro: bertindak sebagai Member (hak paling kecil).
		return s.inner.GetRole(tenantID, "member")
	}
	return nil, fmt.Errorf("role_resolution_failed: custom role %s suspended for tenant %s", roleID, tenantID)
}

// ------------------------------------------------------------
// Handlers
// ------------------------------------------------------------

type customRoleStore interface {
	GetRole(tenantID, roleID string) (*rbac.Role, error)
	PutRole(role *rbac.Role) error
	DeleteRole(tenantID, roleID string) error
	ListRolesForTenant(tenantID string) ([]*rbac.Role, error)
}

type customRoleUserLister interface {
	ListAllForTenant(tenantID string) ([]*auth.User, error)
}

type customRoleHandlers struct {
	tenants customRoleTenantReader
	roles   customRoleStore
	users   customRoleUserLister
}

func newCustomRoleHandlers(tenants customRoleTenantReader, roles customRoleStore, users customRoleUserLister) *customRoleHandlers {
	return &customRoleHandlers{tenants: tenants, roles: roles, users: users}
}

// GET /api/tenants/:id/custom-roles
func (h *customRoleHandlers) List(c *fiber.Ctx) error {
	tenantID := c.Params("id")
	tn, err := h.tenants.Get(tenantID)
	if err != nil {
		return customRoleErr(c, fiber.StatusNotFound, "tenant_not_found")
	}
	all, err := h.roles.ListRolesForTenant(tenantID)
	if err != nil {
		return customRoleErr(c, fiber.StatusInternalServerError, "failed_to_list_roles")
	}
	out := make([]fiber.Map, 0, len(all))
	for _, r := range all {
		if isCustomRoleID(r.ID) {
			out = append(out, customRoleView(r))
		}
	}
	return c.JSON(fiber.Map{
		"roles":       out,
		"count":       len(out),
		"limit":       maxCustomRolesPerTenant,
		"plan_allows": billing.PlanAllowsCustomRoles(tn.PlanTier),
		"catalog":     customRoleCatalog,
	})
}

// POST /api/tenants/:id/custom-roles
func (h *customRoleHandlers) Create(c *fiber.Ctx) error {
	tenantID := c.Params("id")

	var req struct {
		Name         string   `json:"name"`
		Capabilities []string `json:"capabilities"`
	}
	if err := c.BodyParser(&req); err != nil {
		return customRoleErr(c, fiber.StatusBadRequest, "invalid_body")
	}

	tn, err := h.tenants.Get(tenantID)
	if err != nil {
		return customRoleErr(c, fiber.StatusNotFound, "tenant_not_found")
	}
	if !billing.PlanAllowsCustomRoles(tn.PlanTier) {
		return customRoleUpgradeErr(c)
	}

	name := strings.TrimSpace(req.Name)
	if !customRoleNameRE.MatchString(name) {
		return customRoleErr(c, fiber.StatusBadRequest, "invalid_role_name")
	}
	if customRoleReservedNames[strings.ToLower(name)] {
		return customRoleErr(c, fiber.StatusBadRequest, "reserved_role_name")
	}

	grants, unknown := customRoleGrants(tenantID, req.Capabilities)
	if unknown != "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "unknown_capability", "capability": unknown})
	}
	if len(grants) == 0 {
		return customRoleErr(c, fiber.StatusBadRequest, "capabilities_required")
	}

	roleID := customRolePrefix + customRoleSlug(name)

	existing, err := h.roles.ListRolesForTenant(tenantID)
	if err != nil {
		return customRoleErr(c, fiber.StatusInternalServerError, "failed_to_list_roles")
	}
	customCount := 0
	for _, r := range existing {
		if r.ID == roleID {
			return customRoleErr(c, fiber.StatusConflict, "role_already_exists")
		}
		if isCustomRoleID(r.ID) {
			customCount++
		}
	}
	if customCount >= maxCustomRolesPerTenant {
		return customRoleErr(c, fiber.StatusConflict, "role_limit_reached")
	}

	role := &rbac.Role{ID: roleID, TenantID: tenantID, Name: name, CapabilityGrants: grants}
	if err := h.roles.PutRole(role); err != nil {
		return customRoleErr(c, fiber.StatusInternalServerError, "failed_to_save_role")
	}
	return c.JSON(fiber.Map{"role": customRoleView(role)})
}

// PUT /api/tenants/:id/custom-roles/:role_id (hanya capabilities; nama tetap)
func (h *customRoleHandlers) Update(c *fiber.Ctx) error {
	tenantID := c.Params("id")
	roleID := c.Params("role_id")

	var req struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := c.BodyParser(&req); err != nil {
		return customRoleErr(c, fiber.StatusBadRequest, "invalid_body")
	}

	tn, err := h.tenants.Get(tenantID)
	if err != nil {
		return customRoleErr(c, fiber.StatusNotFound, "tenant_not_found")
	}
	if !billing.PlanAllowsCustomRoles(tn.PlanTier) {
		return customRoleUpgradeErr(c)
	}
	if !isCustomRoleID(roleID) {
		return customRoleErr(c, fiber.StatusBadRequest, "not_a_custom_role")
	}
	role, err := h.roles.GetRole(tenantID, roleID)
	if err != nil {
		return customRoleErr(c, fiber.StatusNotFound, "role_not_found")
	}

	grants, unknown := customRoleGrants(tenantID, req.Capabilities)
	if unknown != "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "unknown_capability", "capability": unknown})
	}
	if len(grants) == 0 {
		return customRoleErr(c, fiber.StatusBadRequest, "capabilities_required")
	}

	role.CapabilityGrants = grants
	if err := h.roles.PutRole(role); err != nil {
		return customRoleErr(c, fiber.StatusInternalServerError, "failed_to_save_role")
	}
	return c.JSON(fiber.Map{"role": customRoleView(role)})
}

// DELETE /api/tenants/:id/custom-roles/:role_id
// Tidak digate plan: tenant yang sudah turun tetap bisa membersihkan role.
func (h *customRoleHandlers) Delete(c *fiber.Ctx) error {
	tenantID := c.Params("id")
	roleID := c.Params("role_id")

	if _, err := h.tenants.Get(tenantID); err != nil {
		return customRoleErr(c, fiber.StatusNotFound, "tenant_not_found")
	}
	if !isCustomRoleID(roleID) {
		return customRoleErr(c, fiber.StatusBadRequest, "not_a_custom_role")
	}
	if _, err := h.roles.GetRole(tenantID, roleID); err != nil {
		return customRoleErr(c, fiber.StatusNotFound, "role_not_found")
	}

	users, err := h.users.ListAllForTenant(tenantID)
	if err != nil {
		return customRoleErr(c, fiber.StatusInternalServerError, "failed_to_list_users")
	}
	holders := 0
	for _, u := range users {
		if u.RoleID == roleID {
			holders++
		}
	}
	if holders > 0 {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "role_in_use", "users": holders})
	}

	if err := h.roles.DeleteRole(tenantID, roleID); err != nil {
		return customRoleErr(c, fiber.StatusInternalServerError, "failed_to_delete_role")
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}
