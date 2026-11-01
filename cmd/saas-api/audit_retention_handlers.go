package main

import (
	"github.com/gofiber/fiber/v2"

	"trailseal/audit"
	"trailseal/project"
)

// ============================================================
// AUDIT RETENTION HANDLERS — tenant-scoped (self-service)
//
// PERBAIKAN (Phase 9): versi sebelumnya endpoint ini ada di bawah
// /api/admin/audit/retention (requireRoot) dan RetentionStore GLOBAL
// (1 policy untuk SEMUA tenant, key tetap "policy" tanpa tenantID).
// Itu bug arsitektur, bukan keputusan desain yang disengaja — retensi
// WAJIB per-tenant (§1 pricing: 14/180/365 hari beda per plan) dan
// WAJIB self-service (Owner/Admin TrailSeal cuma monitoring read-only,
// tenant kelola sendiri retention & cleanup mereka).
//
// PERBAIKAN KEDUA (ditemukan saat live-test): resolvePlan sebelumnya
// mengembalikan (0, 0) untuk tenant Free/belum-checkout. Angka 0 itu jatuh ke
// audit.DefaultRetentionDays (365), sehingga tenant Free mendapat retensi audit
// setara Business. Sekarang planResolver (app.go) mengembalikan batas plan Free
// (5.000 events / 14 hari) untuk tenant Free, belum checkout, tidak terbaca,
// atau plan tidak dikenal. resolvePlanRetentionDays di bawah tetap menjamin
// hasil tidak pernah 0: bila resolver nil atau days <= 0, dipakai
// project.DefaultRetentionDays (14, sama dengan retensi Free pada tabel pricing).

// resolvePlanRetentionDays memanggil resolvePlan, dan menjamin hasilnya
// TIDAK PERNAH 0 — fallback ke project.DefaultRetentionDays (14), bukan
// biarkan audit.RetentionStore pakai default internalnya sendiri (365).
func resolvePlanRetentionDays(tenantID string, resolvePlan project.PlanResolver) int {
	if resolvePlan != nil {
		if _, days := resolvePlan(tenantID); days > 0 {
			return days
		}
	}
	return project.DefaultRetentionDays
}

// GetRetentionPolicyHandler — GET /api/tenants/:id/audit/retention
func GetRetentionPolicyHandler(retentionStore *audit.RetentionStore, resolvePlan project.PlanResolver) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "tenant_id_required",
			})
		}

		planRetentionDays := resolvePlanRetentionDays(tenantID, resolvePlan)

		policy, err := retentionStore.GetPolicy(tenantID, planRetentionDays)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to get retention policy",
			})
		}

		return c.JSON(fiber.Map{
			"tenant_id":              tenantID,
			"retention_days":         policy.RetentionDays,
			"anonymization_days":     policy.AnonymizationDays,
			"cleanup_interval_hours": int(policy.CleanupInterval.Hours()),
			"plan_retention_days":    planRetentionDays,
		})
	}
}

// UpdateRetentionPolicyHandler — PUT /api/tenants/:id/audit/retention
//
// Tenant HANYA bisa: (a) set anonymization_days bebas (dalam batas
// retention_days efektif), (b) PERSINGKAT retention_days sampai batas
// bawah wajar — TIDAK BISA memperpanjang melebihi plan_retention_days
// (kapasitas yang mereka bayar).
func UpdateRetentionPolicyHandler(retentionStore *audit.RetentionStore, resolvePlan project.PlanResolver) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "tenant_id_required",
			})
		}

		var req struct {
			RetentionDays     int `json:"retention_days"`
			AnonymizationDays int `json:"anonymization_days"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		planRetentionDays := resolvePlanRetentionDays(tenantID, resolvePlan)

		policy := &audit.RetentionPolicy{
			RetentionDays:     req.RetentionDays,
			AnonymizationDays: req.AnonymizationDays,
		}

		if err := retentionStore.SetPolicy(tenantID, policy, planRetentionDays); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		updated, err := retentionStore.GetPolicy(tenantID, planRetentionDays)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to reload retention policy",
			})
		}

		return c.JSON(fiber.Map{
			"status":                 "updated",
			"tenant_id":              tenantID,
			"retention_days":         updated.RetentionDays,
			"anonymization_days":     updated.AnonymizationDays,
			"cleanup_interval_hours": int(updated.CleanupInterval.Hours()),
			"plan_retention_days":    planRetentionDays,
		})
	}
}

// TriggerCleanupHandler — POST /api/tenants/:id/audit/retention/cleanup
//
// PERBAIKAN: versi lama (runAuditCleanup) adalah stub kosong yang selalu
// balas {"events_deleted":0,"events_anonymized":0} tanpa kerja apapun.
// Sekarang benar-benar hapus/anonymize event milik tenant yang login,
// scoped ke tenant itu saja (bukan trawl semua tenant sekaligus).
func TriggerCleanupHandler(
	auditStore audit.Store,
	retentionStore *audit.RetentionStore,
	resolvePlan project.PlanResolver,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "tenant_id_required",
			})
		}

		planRetentionDays := resolvePlanRetentionDays(tenantID, resolvePlan)

		policy, err := retentionStore.GetPolicy(tenantID, planRetentionDays)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to get retention policy",
			})
		}

		deleted, anonymized, err := auditStore.CleanupForTenant(tenantID, policy)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "cleanup failed: " + err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"status":             "cleanup_completed",
			"tenant_id":          tenantID,
			"events_deleted":     deleted,
			"events_anonymized":  anonymized,
			"retention_days":     policy.RetentionDays,
			"anonymization_days": policy.AnonymizationDays,
		})
	}
}
