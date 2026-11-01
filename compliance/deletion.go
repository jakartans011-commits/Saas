package compliance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"trailseal/agent"
	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/auth"
	"trailseal/project"
	"trailseal/tenant"
)

// ============================================================
// GDPR DELETION — Soft delete, restore, purge
// ============================================================

const (
	// DefaultGracePeriod — Free & Pro: 30 hari
	DefaultGracePeriod = 30 * 24 * time.Hour

	// BusinessGracePeriod — Business tier: 60 hari (sesuai pricing)
	BusinessGracePeriod = 60 * 24 * time.Hour
)

// DeletionConfig holds configuration for deletion
type DeletionConfig struct {
	GracePeriod time.Duration
}

// DefaultDeletionConfig returns default configuration (Free/Pro)
func DefaultDeletionConfig() *DeletionConfig {
	return &DeletionConfig{
		GracePeriod: DefaultGracePeriod,
	}
}

// gracePeriodFor mengembalikan grace period sesuai plan tier tenant.
// Dipanggil oleh PurgeTenant saat config == nil (production path).
// Test yang butuh kontrol eksplisit tetap bisa pass config sendiri.
func gracePeriodFor(planTier string) time.Duration {
	if planTier == "business" {
		return BusinessGracePeriod
	}
	return DefaultGracePeriod
}

// ============================================================
// SOFT DELETE TENANT
// ============================================================

func SoftDeleteTenant(
	tenantStore *tenant.BoltTenantStore,
	userStore *auth.BoltUserStore,
	agentStore *agent.BoltAgentStore,
	tenantID string,
) error {
	t, err := tenantStore.GetIncludingDeleted(tenantID)
	if err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	if t.DeletedAt != nil {
		return errors.New("tenant already soft-deleted")
	}

	now := time.Now()
	t.DeletedAt = &now
	if err := tenantStore.Put(t); err != nil {
		return fmt.Errorf("failed to soft delete tenant: %w", err)
	}

	if err := softDeleteUsers(userStore, tenantID); err != nil {
		return fmt.Errorf("failed to soft delete users: %w", err)
	}

	if err := softDeleteAgents(agentStore, tenantID); err != nil {
		return fmt.Errorf("failed to soft delete agents: %w", err)
	}

	if err := softDeleteTools(agentStore, tenantID); err != nil {
		return fmt.Errorf("failed to soft delete tools: %w", err)
	}

	return nil
}

func softDeleteUsers(userStore *auth.BoltUserStore, tenantID string) error {
	users, err := userStore.ListAllForTenant(tenantID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, u := range users {
		u.DeletedAt = &now
		if err := userStore.Put(u); err != nil {
			return err
		}
	}
	return nil
}

func softDeleteAgents(agentStore *agent.BoltAgentStore, tenantID string) error {
	agents, err := agentStore.ListAgents(tenantID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, a := range agents {
		a.DeletedAt = &now
		if err := agentStore.SaveAgent(a); err != nil {
			return err
		}
	}
	return nil
}

func softDeleteTools(agentStore *agent.BoltAgentStore, tenantID string) error {
	tools, err := agentStore.ListTools(tenantID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, t := range tools {
		t.DeletedAt = &now
		if err := agentStore.SaveTool(t); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================
// RESTORE TENANT
// ============================================================

func RestoreTenant(
	tenantStore *tenant.BoltTenantStore,
	userStore *auth.BoltUserStore,
	agentStore *agent.BoltAgentStore,
	tenantID string,
) error {
	t, err := tenantStore.GetIncludingDeleted(tenantID)
	if err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	if t.DeletedAt == nil {
		return errors.New("tenant is not soft-deleted")
	}

	t.DeletedAt = nil
	if err := tenantStore.Put(t); err != nil {
		return fmt.Errorf("failed to restore tenant: %w", err)
	}

	if err := restoreUsers(userStore, tenantID); err != nil {
		return fmt.Errorf("failed to restore users: %w", err)
	}

	if err := restoreAgents(agentStore, tenantID); err != nil {
		return fmt.Errorf("failed to restore agents: %w", err)
	}

	if err := restoreTools(agentStore, tenantID); err != nil {
		return fmt.Errorf("failed to restore tools: %w", err)
	}

	return nil
}

func restoreUsers(userStore *auth.BoltUserStore, tenantID string) error {
	users, err := userStore.ListDeletedForTenant(tenantID)
	if err != nil {
		return err
	}
	for _, u := range users {
		u.DeletedAt = nil
		if err := userStore.Put(u); err != nil {
			return err
		}
	}
	return nil
}

func restoreAgents(agentStore *agent.BoltAgentStore, tenantID string) error {
	agents, err := agentStore.ListDeletedAgents(tenantID)
	if err != nil {
		return err
	}
	for _, a := range agents {
		a.DeletedAt = nil
		if err := agentStore.SaveAgent(a); err != nil {
			return err
		}
	}
	return nil
}

func restoreTools(agentStore *agent.BoltAgentStore, tenantID string) error {
	tools, err := agentStore.ListDeletedTools(tenantID)
	if err != nil {
		return err
	}
	for _, t := range tools {
		t.DeletedAt = nil
		if err := agentStore.SaveTool(t); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================
// PURGE TENANT (HARD DELETE)
// ============================================================

// PurgeTenant permanently deletes a tenant and all its children.
// Grace period ditentukan dari plan tier tenant (30 hari Free/Pro,
// 60 hari Business — sesuai pricing). Config eksplisit tetap dihormati
// kalau diisi (dipakai test untuk override periode).
//
// FIX (GDPR gap): sebelumnya hanya users/agents/tools/tenant record yang
// dihapus — projects, API key metadata, dan audit log events tertinggal
// selamanya sebagai orphan data. Sekarang ketiganya ikut dihapus, sebelum
// tenant record sendiri dihapus.
func PurgeTenant(
	ctx context.Context,
	tenantStore *tenant.BoltTenantStore,
	userStore *auth.BoltUserStore,
	agentStore *agent.BoltAgentStore,
	projectStore *project.PostgresStore,
	apiKeyStore *apikey.PostgresStore,
	auditStore audit.Store,
	tenantID string,
	config *DeletionConfig,
) error {
	t, err := tenantStore.GetIncludingDeleted(tenantID)
	if err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	if t.DeletedAt == nil {
		return errors.New("tenant must be soft-deleted before purge")
	}

	// Tier-aware grace period: config eksplisit menang (untuk test),
	// fallback ke grace period berdasarkan plan tier.
	if config == nil {
		config = &DeletionConfig{
			GracePeriod: gracePeriodFor(t.PlanTier),
		}
	}

	gracePeriodEnd := t.DeletedAt.Add(config.GracePeriod)
	if time.Now().Before(gracePeriodEnd) {
		return fmt.Errorf("grace period not yet passed: %s remaining",
			time.Until(gracePeriodEnd).Truncate(time.Second).String())
	}

	_ = userStore.DeleteAllForTenant(tenantID)

	if err := agentStore.DeleteAllAgentsForTenant(tenantID); err != nil {
		return fmt.Errorf("failed to hard delete agents: %w", err)
	}

	if err := agentStore.DeleteAllToolsForTenant(tenantID); err != nil {
		return fmt.Errorf("failed to hard delete tools: %w", err)
	}

	if err := apiKeyStore.DeleteAllForTenant(ctx, tenantID); err != nil {
		return fmt.Errorf("failed to hard delete api keys: %w", err)
	}

	if err := projectStore.DeleteAllForTenant(ctx, tenantID); err != nil {
		return fmt.Errorf("failed to hard delete projects: %w", err)
	}

	if err := auditStore.DeleteAllForTenant(tenantID); err != nil {
		return fmt.Errorf("failed to hard delete audit events: %w", err)
	}

	if err := tenantStore.Delete(tenantID); err != nil {
		return fmt.Errorf("failed to hard delete tenant: %w", err)
	}

	return nil
}
