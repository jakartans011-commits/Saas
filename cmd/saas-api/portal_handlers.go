package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"html/template"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/billing"
	"trailseal/events"
	"trailseal/middleware"
	"trailseal/project"
	"trailseal/tenant"
	"trailseal/usage"
)

//go:embed templates/*
var portalTemplatesFS embed.FS

//go:embed portal_static/*
var portalStaticFS embed.FS

var portalPageTemplateFiles = map[string]string{
	"overview":   "dashboard.html",
	"agents":     "agents.html",
	"operations": "operations.html",
	"audit":      "audit.html",
	"projects":   "projects.html",
	"apikeys":    "apikeys.html",
	"usage":      "usage.html",
}

var portalPageTemplates = func() map[string]*template.Template {
	out := make(map[string]*template.Template, len(portalPageTemplateFiles))
	for page, file := range portalPageTemplateFiles {
		out[page] = template.Must(template.ParseFS(
			portalTemplatesFS,
			"templates/layout.html",
			"templates/"+file,
		))
	}
	return out
}()

type PortalStores struct {
	TenantStore   *tenant.BoltTenantStore
	ProjectStore  *project.PostgresStore
	APIKeyStore   *apikey.PostgresStore
	UsageStore    *usage.BoltEventStore
	AuditStore    audit.Store
	PlansRegistry *billing.Registry
	EventsStore   EventsStore // BARU — untuk baca quota counter riil, lihat handleUsage
}

func RegisterPortalRoutes(
	app *fiber.App,
	requireAuth fiber.Handler,
	stores PortalStores,
) {
	app.Use("/portal-static", func(c *fiber.Ctx) error {
		path := "portal_static" + c.Path()[len("/portal-static"):]
		data, err := portalStaticFS.ReadFile(path)
		if err != nil {
			return c.Status(fiber.StatusNotFound).SendString("not found")
		}
		if len(path) > 4 && path[len(path)-4:] == ".css" {
			c.Set("Content-Type", "text/css; charset=utf-8")
		}
		return c.Send(data)
	})

	app.Get("/overview", requireAuth, stores.handleOverview)
	app.Get("/agents", requireAuth, stores.handleAgents)
	app.Get("/operations", requireAuth, stores.handleOperations)
	app.Get("/audit", requireAuth, stores.handleAudit)
	app.Get("/developer/projects", requireAuth, stores.handleProjects)
	app.Post("/developer/projects", requireAuth, stores.handleProjectCreate)
	app.Get("/developer/api-keys", requireAuth, stores.handleAPIKeys)
	app.Post("/developer/api-keys/generate", requireAuth, stores.handleAPIKeyGenerate)
	app.Post("/developer/api-keys/revoke", requireAuth, stores.handleAPIKeyRevoke)
	app.Get("/developer/usage", requireAuth, stores.handleUsage)
}

func portalTenantID(c *fiber.Ctx) (string, error) {
	claims, ok := middleware.ClaimsFromFiberCtx(c)
	if !ok || claims == nil || claims.TenantID == "" {
		return "", errors.New("unauthenticated_context")
	}
	return claims.TenantID, nil
}

func (s PortalStores) loadTenant(tenantID string) *tenant.Tenant {
	t, err := s.TenantStore.Get(tenantID)
	if err != nil {
		return &tenant.Tenant{ID: tenantID, Name: tenantID, Status: "unprovisioned"}
	}
	return t
}

func (s PortalStores) resolveProject(
	c *fiber.Ctx,
	tenantID string,
	requestedProjectID string,
) (*project.Project, error) {
	if requestedProjectID != "" {
		return s.ProjectStore.Get(c.UserContext(), tenantID, requestedProjectID)
	}

	projects, err := s.ProjectStore.ListByTenant(c.UserContext(), tenantID)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, nil
	}
	return projects[0], nil
}

var pagePaths = map[string]string{
	"overview":   "/overview",
	"agents":     "/agents",
	"operations": "/operations",
	"audit":      "/audit",
	"projects":   "/developer/projects",
	"apikeys":    "/developer/api-keys",
	"usage":      "/developer/usage",
}

func baseData(page string, t *tenant.Tenant) map[string]interface{} {
	return map[string]interface{}{
		"Page":         page,
		"PagePath":     pagePaths[page],
		"TenantID":     t.ID,
		"TenantStatus": string(t.Status),
		"Tenant":       t,
	}
}

func renderPortal(c *fiber.Ctx, data map[string]interface{}) error {
	page, _ := data["Page"].(string)

	tmpl, ok := portalPageTemplates[page]
	if !ok {
		return c.Status(fiber.StatusInternalServerError).SendString(
			"portal: no template registered for page " + page,
		)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(buf.Bytes())
}

func (s PortalStores) handleOverview(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}
	t := s.loadTenant(tenantID)

	events, _ := s.UsageStore.ListForTenant(tenantID, 200)

	activeKeys, revokedKeys := 0, 0
	if proj, err := s.resolveProject(c, tenantID, ""); err == nil && proj != nil {
		if keys, err := s.APIKeyStore.ListByProject(c.UserContext(), tenantID, proj.ID); err == nil {
			for _, k := range keys {
				if k.RevokedAt == nil {
					activeKeys++
				} else {
					revokedKeys++
				}
			}
		}
	}

	recent := events
	if len(recent) > 8 {
		recent = recent[:8]
	}

	var lastEventAt string
	if len(events) > 0 {
		lastEventAt = events[0].Timestamp.Format("2006-01-02 15:04:05")
	}

	planTier := t.PlanTier
	if planTier == "" {
		planTier = string(billing.PlanFree)
	}

	quotaLimit := quotaLimitForPlan(t.PlanTier)
	quotaUsed := len(events)
	quotaPercent := 0
	if quotaLimit > 0 {
		quotaPercent = quotaUsed * 100 / quotaLimit
		if quotaPercent > 100 {
			quotaPercent = 100
		}
	}

	retentionDays := 0
	if s.PlansRegistry != nil {
		if cfg, ok := s.PlansRegistry.Get(billing.PlanTier(planTier)); ok {
			retentionDays = cfg.RetentionDays
		}
	}

	projects, projErr := s.ProjectStore.ListByTenant(c.UserContext(), tenantID)
	if projErr != nil {
		projects = nil
	}
	recentProjects := projects
	if len(recentProjects) > 5 {
		recentProjects = recentProjects[:5]
	}

	var recentAudit []*audit.AuditEvent
	if s.AuditStore != nil {
		recentAudit, _ = s.AuditStore.ListForTenant(tenantID, 8)
	}

	data := baseData("overview", t)
	data["PageTitle"] = "Overview"
	data["UsageCount"] = len(events)
	data["ActiveKeyCount"] = activeKeys
	data["RevokedKeyCount"] = revokedKeys
	data["RecentUsage"] = recent
	data["LastEventAt"] = lastEventAt
	data["QuotaLimit"] = quotaLimit
	data["QuotaUsed"] = quotaUsed
	data["QuotaPercent"] = quotaPercent
	data["QuotaNearLimit"] = quotaPercent >= 80
	data["QuotaResetDate"] = nextMonthlyResetDate(time.Now())
	data["RetentionDays"] = retentionDays
	data["ProjectCount"] = len(projects)
	data["RecentProjects"] = recentProjects
	data["RecentAuditEvents"] = recentAudit

	return renderPortal(c, data)
}

func (s PortalStores) handleAgents(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}
	t := s.loadTenant(tenantID)
	data := baseData("agents", t)
	data["PageTitle"] = "Agent Fleet"
	return renderPortal(c, data)
}

func (s PortalStores) handleOperations(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}
	t := s.loadTenant(tenantID)
	data := baseData("operations", t)
	data["PageTitle"] = "Operations"
	return renderPortal(c, data)
}

func (s PortalStores) handleAudit(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}
	t := s.loadTenant(tenantID)
	data := baseData("audit", t)
	data["PageTitle"] = "Audit & Compliance"
	return renderPortal(c, data)
}

func (s PortalStores) handleProjects(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}
	t := s.loadTenant(tenantID)

	projects, err := s.ProjectStore.ListByTenant(c.UserContext(), tenantID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	data := baseData("projects", t)
	data["PageTitle"] = "Projects"
	data["Projects"] = projects

	return renderPortal(c, data)
}

func (s PortalStores) handleProjectCreate(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}

	name := c.FormValue("name")
	if name == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Project name is required")
	}

	p := &project.Project{
		TenantID: tenantID,
		Name:     name,
	}

	if err := s.ProjectStore.Create(c.UserContext(), p); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	return c.Redirect("/developer/projects", fiber.StatusSeeOther)
}

func (s PortalStores) handleAPIKeys(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}
	t := s.loadTenant(tenantID)

	data := baseData("apikeys", t)
	data["PageTitle"] = "API Keys"

	projects, err := s.ProjectStore.ListByTenant(c.UserContext(), tenantID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	data["Projects"] = projects

	proj, err := s.resolveProject(c, tenantID, c.Query("project"))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	if proj == nil {
		data["NoProject"] = true
		data["Keys"] = []*apikey.APIKey{}
		return renderPortal(c, data)
	}

	data["SelectedProject"] = proj

	keys, err := s.APIKeyStore.ListByProject(c.UserContext(), tenantID, proj.ID)
	if err != nil {
		keys = []*apikey.APIKey{}
	}
	data["Keys"] = keys

	return renderPortal(c, data)
}

func (s PortalStores) handleAPIKeyGenerate(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}

	var name, scope, projectID string
	if c.Get("Content-Type") == "application/json" {
		var req struct {
			Name      string `json:"name"`
			Scope     string `json:"scope"`
			ProjectID string `json:"project_id"`
		}
		if err := c.BodyParser(&req); err == nil {
			name = req.Name
			scope = req.Scope
			projectID = req.ProjectID
		}
	} else {
		name = c.FormValue("name")
		scope = c.FormValue("scope")
		projectID = c.FormValue("project_id")
	}

	if name == "" {
		return c.Status(fiber.StatusBadRequest).SendString("name is required")
	}

	proj, err := s.resolveProject(c, tenantID, projectID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	if proj == nil {
		return c.Status(fiber.StatusBadRequest).SendString(
			"tenant has no project provisioned yet; create a project before generating an API key",
		)
	}

	generated, err := apikey.GenerateAPIKey(tenantID, proj.ID, name, apikey.Scope(scope))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString(err.Error())
	}

	limit := s.apiKeyLimit(tenantID)
	if err := s.APIKeyStore.CreateWithLimit(c.UserContext(), generated.Record, limit); err != nil {
		if errors.Is(err, apikey.ErrKeyLimitReached) {
			if c.Get("X-Requested-With") == "XMLHttpRequest" || c.Get("Accept") == "application/json" {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "api_key_limit_reached",
					"limit": limit,
				})
			}
			return c.Status(fiber.StatusForbidden).SendString(
				"api key limit reached for your plan; revoke an existing key or upgrade",
			)
		}
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	if c.Get("X-Requested-With") == "XMLHttpRequest" || c.Get("Accept") == "application/json" {
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"api_key": generated.Record,
			"key":     generated.Plaintext,
			"warning": "This is the only time the plaintext key will be shown. Store it securely now.",
		})
	}

	return c.Redirect("/developer/api-keys?project="+proj.ID, fiber.StatusSeeOther)
}

func (s PortalStores) handleAPIKeyRevoke(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}

	keyID := c.FormValue("key_id")
	if keyID == "" {
		return c.Status(fiber.StatusBadRequest).SendString("key_id is required")
	}

	proj, err := s.resolveProject(c, tenantID, c.FormValue("project_id"))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	if proj == nil {
		return c.Status(fiber.StatusBadRequest).SendString("tenant has no project provisioned")
	}

	if err := s.APIKeyStore.Revoke(c.UserContext(), tenantID, proj.ID, keyID); err != nil {
		if errors.Is(err, apikey.ErrNotFound) {
			return c.Status(fiber.StatusNotFound).SendString(err.Error())
		}
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	return c.Redirect("/developer/api-keys?project="+proj.ID, fiber.StatusSeeOther)
}

func quotaLimitForPlan(planTier string) int {
	return int(billing.GetMonthlyEventLimit(billing.PlanTier(planTier)))
}

// sumQuotaUsedSafely menjumlahkan event_count riil (dari
// project_usage_counters, via EventsStore.GetUsageCount) di semua
// project tenant untuk satu periode. Dibungkus recover() karena
// ProjectStore/EventsStore bisa backed oleh pgxpool nil di sebagian
// test (lihat komentar "pgPool BOLEH nil" di app.go) — juga jaga-jaga
// production kalau DB project sempat down: halaman usage tetap
// render (quota tampil 0/fail-open) alih-alih 500 total.
func (s PortalStores) sumQuotaUsedSafely(c *fiber.Ctx, tenantID, period string) (total int) {
	defer func() {
		if r := recover(); r != nil {
			total = 0
		}
	}()

	projects, err := s.ProjectStore.ListByTenant(c.UserContext(), tenantID)
	if err != nil {
		return 0
	}
	for _, p := range projects {
		count, cErr := s.EventsStore.GetUsageCount(c.UserContext(), p.ID, period)
		if cErr == nil {
			total += int(count)
		}
		// error per-project diabaikan sengaja (fail-open ke 0 untuk
		// project itu) supaya satu project bermasalah tidak
		// menjatuhkan seluruh halaman usage tenant.
	}
	return total
}

type dailyCount struct {
	Date    string
	Count   int
	Percent int
}

func nextMonthlyResetDate(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
}

func buildDailySeries(events []*usage.UsageEvent, days int) []dailyCount {
	counts := map[string]int{}
	for _, e := range events {
		day := e.Timestamp.UTC().Format("2006-01-02")
		counts[day]++
	}

	series := make([]dailyCount, days)
	maxCount := 0
	now := time.Now().UTC()

	for i := 0; i < days; i++ {
		day := now.AddDate(0, 0, -(days - 1 - i)).Format("2006-01-02")
		c := counts[day]
		series[i] = dailyCount{Date: day, Count: c}
		if c > maxCount {
			maxCount = c
		}
	}

	for i := range series {
		if maxCount > 0 {
			series[i].Percent = series[i].Count * 100 / maxCount
		}
	}

	return series
}

type eventTypeBreakdown struct {
	EventType string
	Count     int
	Percent   int
}

func buildEventTypeBreakdown(typeCounts map[string]int, total int) []eventTypeBreakdown {
	out := make([]eventTypeBreakdown, 0, len(typeCounts))
	for et, count := range typeCounts {
		percent := 0
		if total > 0 {
			percent = count * 100 / total
		}
		out = append(out, eventTypeBreakdown{EventType: et, Count: count, Percent: percent})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// quotaWarningTier — 2C.2: tiga ambang eksplisit (80/90/100%), bukan
// satu ambang biner seperti versi sebelumnya. "exceeded" persis di
// 100%+ karena itu titik di mana request tambahan mulai kena 429 —
// beda makna dengan sekadar "mendekati limit".
func quotaWarningTier(percent int) string {
	switch {
	case percent >= 100:
		return "exceeded"
	case percent >= 90:
		return "critical"
	case percent >= 80:
		return "warning"
	default:
		return "ok"
	}
}

// EventsStore — interface minimal biar portal_handlers.go tidak
// perlu import *events.PostgresStore langsung di sini kalau tidak
// mau; kalau lebih simpel, pakai *events.PostgresStore langsung di
// PortalStores. Ditulis sebagai interface untuk gampang di-mock test.
type EventsStore interface {
	GetUsageCount(ctx context.Context, projectID, period string) (int64, error)
}

func (s PortalStores) handleUsage(c *fiber.Ctx) error {
	tenantID, err := portalTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}
	t := s.loadTenant(tenantID)

	// events (dashboard "Recent Events" table + daily/breakdown chart)
	// TETAP dari usage.BoltEventStore — itu memang mencatat traffic
	// dashboard/API request lain, bukan sumber kuota billing.
	usageEvents, _ := s.UsageStore.ListForTenant(tenantID, 200)

	typeCounts := map[string]int{}
	for _, e := range usageEvents {
		typeCounts[string(e.EventType)]++
	}

	// --------------------------------------------------------
	// QUOTA — dijumlahkan dari SEMUA project tenant, dibaca dari
	// project_usage_counters via events.PostgresStore.GetUsageCount,
	// sumber counter YANG SAMA PERSIS dipakai POST /v1/events untuk
	// menegakkan kuota. INI angka pemakaian produk riil, bukan lagi
	// traffic dashboard.
	//
	// CATATAN PENTING (belum diperbaiki, di luar scope perubahan
	// ini): enforcement di events.PostgresStore.Append() saat ini
	// mengecek monthly_event_limit PER PROJECT, bukan pooled
	// tenant-level — beda dari cara MCP gateway menegakkan kuota
	// (tenantStore, pooled). Menjumlahkan di sini membuat ANGKA
	// YANG DITAMPILKAN akurat terhadap definisi pricing (pooled),
	// tapi TIDAK mengubah bagaimana backend benar-benar reject
	// request (Append() masih per-project). Kalau tenant punya >1
	// project, jumlah tampilan ini bisa melebihi apa yang sebenarnya
	// di-enforce di titik mana pun. Ini gap arsitektur terpisah,
	// perlu keputusan sadar founder sebelum diubah — TIDAK disentuh
	// di perubahan ini.
	// --------------------------------------------------------
	planTier := t.PlanTier
	if planTier == "" {
		planTier = string(billing.PlanFree)
	}

	quotaUsed := 0
	if s.EventsStore != nil {
		quotaUsed = s.sumQuotaUsedSafely(c, tenantID, events.CurrentPeriod())
	}

	quotaLimit := quotaLimitForPlan(t.PlanTier)
	quotaPercent := 0
	if quotaLimit > 0 {
		quotaPercent = quotaUsed * 100 / quotaLimit
		if quotaPercent > 100 {
			quotaPercent = 100
		}
	}
	quotaRemaining := quotaLimit - quotaUsed
	if quotaRemaining < 0 {
		quotaRemaining = 0
	}

	data := baseData("usage", t)
	data["PageTitle"] = "Usage & Telemetry"
	data["UsageCount"] = len(usageEvents)
	data["UsageEvents"] = usageEvents
	data["EventTypeCounts"] = typeCounts
	data["EventTypeBreakdown"] = buildEventTypeBreakdown(typeCounts, len(usageEvents))
	data["DailySeries"] = buildDailySeries(usageEvents, 14)
	data["PlanTier"] = planTier
	data["QuotaLimit"] = quotaLimit
	data["QuotaUsed"] = quotaUsed
	data["QuotaRemaining"] = quotaRemaining
	data["QuotaPercent"] = quotaPercent
	data["QuotaNearLimit"] = quotaPercent >= 80
	data["QuotaWarningTier"] = quotaWarningTier(quotaPercent)
	data["QuotaResetDate"] = nextMonthlyResetDate(time.Now())
	render := renderPortal
	return render(c, data)
}
