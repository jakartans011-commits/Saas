package main

import (
	"github.com/gofiber/fiber/v2"

	"trailseal/rbac"
)

// TenantRolesHandler returns the roles available for this tenant, for
// use in the Team page's role dropdown (previously hardcoded "owner").
//
// RoleStore has no "list roles for tenant" method (see rbac/role.go —
// GetRole is per-ID only), and we deliberately do NOT add one: role IDs
// are deterministic ("owner" always, "admin"/"member" on Pro/Business —
// see tenant/provisioning.go and billing/service.go), so we just probe
// each known ID and skip the ones that don't exist for this tenant
// (e.g. Free tier tenants will only get "owner" back). This avoids
// touching RoleStore/RoleAdminStore interfaces, which would otherwise
// break every mock implementing them (InMemoryRoleStore, BoltRoleStore,
// fakeRoleAdminStore, fakeMismatchStore, failingPutRoleStore — see
// API-CONTRACTS.md bug pattern #9).
func TenantRolesHandler(roleStore rbac.RoleStore) fiber.Handler {
	knownRoleIDs := []string{"owner", "admin", "member"}

	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		roles := make([]*rbac.Role, 0, len(knownRoleIDs))
		for _, roleID := range knownRoleIDs {
			role, err := roleStore.GetRole(tenantID, roleID)
			if err != nil {
				continue // role not provisioned for this tenant (e.g. Free tier)
			}
			roles = append(roles, role)
		}

		return c.JSON(fiber.Map{
			"roles": roles,
			"count": len(roles),
		})
	}
}
