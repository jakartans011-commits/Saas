package main

import (
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor" // <-- TAMBAH BARIS INI
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/stripe/stripe-go/v78"
	bolt "go.etcd.io/bbolt"

	"github.com/jackc/pgx/v5/pgxpool"

	"trailseal/agent"
	"trailseal/apikey"
	"trailseal/audit"
	"trailseal/auth"
	//"trailseal/compliance"//
	"os"
	"trailseal/alerts"
	"trailseal/billing"
	"trailseal/core/capability"
	"trailseal/events"
	"trailseal/mcp"
	"trailseal/middleware"
	"trailseal/notify"
	"trailseal/observability"
	"trailseal/project"
	"trailseal/rbac"
	"trailseal/tenant"
	"trailseal/usage"
	"trailseal/web"
)

// ============================================================
// USER ID
// ============================================================

func newUserID() string {
	b := make([]byte, 16)

	_, _ = rand.Read(b)

	return "user-" + hex.EncodeToString(b)
}

// ============================================================
// TENANT ID SLUG (for self-serve signup — POST /api/auth/register)
//
// Admin-provisioned tenants (POST /api/admin/tenants) receive their
// tenant_id explicitly from the caller. Self-serve signup has no such
// caller-supplied ID — it only has a company name — so we derive one.
// ============================================================

func slugifyCompanyName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))

	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "tenant"
	}
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return slug
}

func randomTenantSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ============================================================
// BUILD APP
//
// pgPool BOLEH nil (dipakai di beberapa test yang tidak menyentuh
// endpoint project/apikey/events sama sekali — lihat app_test.go).
// Endpoint yang benar-benar memanggil query lewat pgPool akan
// panic kalau pgPool nil DAN endpoint itu benar dipanggil — jadi
// nil HANYA aman untuk test yang tidak pernah hit /api/tenants/:id/projects
// atau /v1/events/*.
// ============================================================

func BuildApp(
	db *bolt.DB,
	pgPool *pgxpool.Pool,
	signingKey,
	rootAPIKey string,
	views fiber.Views,
) (*fiber.App, *billing.Service, error) {
	// ============================================================
	// ERROR TRACKING (#4) & CRITICAL ALERTS (#5)
	//
	// Keduanya no-op aman kalau env var terkait tidak diset (dipakai
	// terus-menerus oleh test lewat BuildApp tanpa SENTRY_DSN/
	// ALERT_WEBHOOK_URL) — lihat observability.Init / alerts.Init.
	// ============================================================

	observability.Init()
	alerts.Init()

	userStore, err := auth.NewBoltUserStore(db)
	if err != nil {
		return nil, nil, err
	}

	roleStore, err := rbac.NewBoltRoleStore(db)
	if err != nil {
		return nil, nil, err
	}

	tenantStore, err := tenant.NewBoltTenantStore(db)
	if err != nil {
		return nil, nil, err
	}

	refreshStore, err := auth.NewBoltRefreshTokenStore(db)
	if err != nil {
		return nil, nil, err
	}

	// ============================================================
	// ACCESS TOKEN REVOCATION STORE
	// ============================================================

	revocationStore, err := auth.NewBoltAccessTokenRevocationStore(db)
	if err != nil {
		return nil, nil, err
	}

	emailVerificationStore, err := auth.NewBoltEmailVerificationStore(db)
	if err != nil {
		return nil, nil, err
	}

	passwordResetStore, err := auth.NewBoltPasswordResetStore(db)
	if err != nil {
		return nil, nil, err
	}

	mailer := auth.NewMailerFromEnv()

	// ============================================================
	// USAGE METERING STORE (internal, unrelated ke events/ product)
	// ============================================================

	eventStore, err := usage.NewBoltEventStore(db)
	if err != nil {
		return nil, nil, err
	}

	// ============================================================
	// EMAIL QUOTA NOTIFICATION QUEUE (#9)
	//
	// Queue durable saja di fase ini — belum ada EmailSender yang
	// benar-benar mengirim (lihat notify/email.go). Draining queue
	// ditangani proses terpisah (cmd/notify-worker), bukan di sini.
	// ============================================================

	notifyQueue, err := notify.NewBoltEmailQueue(db)
	if err != nil {
		return nil, nil, err
	}
	notify.SetQueue(notifyQueue)

	// ============================================================
	// AUDIT LOGGING STORE
	//
	// MIGRASI 2026-09: pindah dari BoltAuditStore (file lokal per-
	// instance) ke PostgresAuditStore, supaya jejak audit konsisten
	// walau saas-api dijalankan >1 instance. pgPool WAJIB non-nil
	// sekarang untuk audit (sebelumnya cuma project/apikey/events yang
	// butuh) — lihat catatan pgPool di komentar BuildApp di atas.
	// ============================================================

	auditStore, err := audit.NewPostgresAuditStore(pgPool)
	if err != nil {
		return nil, nil, err
	}

	// ============================================================
	// AUDIT RETENTION STORE
	// ============================================================

	retentionStore, err := audit.NewRetentionStore(db)
	if err != nil {
		return nil, nil, err
	}

	// ============================================================
	// AGENT STORE
	// ============================================================

	agentStore, err := agent.NewBoltAgentStore(db)
	if err != nil {
		return nil, nil, err
	}

	// ============================================================
	// PROJECT / API KEY / EVENTS STORES (Postgres) — INTI PRODUK
	// TrailSeal (audit-log-as-a-service). pgPool di-share ke ketiganya.
	// ============================================================

	projectStore := project.NewPostgresStore(pgPool)
	apiKeyStore := apikey.NewPostgresStore(pgPool)
	eventsStore := events.NewPostgresStore(pgPool)

	// ============================================================
	// BILLING SETUP - TAMBAHKAN INI
	// ============================================================

	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")

	plansRegistry := billing.NewRegistry(
		os.Getenv("STRIPE_PRICE_PRO"),
		os.Getenv("STRIPE_PRICE_BUSINESS"),
	)

	// Opsi tahunan + alias price legacy (semua opsional: env kosong = tidak aktif).
	for _, a := range []struct {
		tier   billing.PlanTier
		env    string
		annual bool
	}{
		{billing.PlanBusiness, "STRIPE_PRICE_BUSINESS_LEGACY", false},
		{billing.PlanPro, "STRIPE_PRICE_PRO_ANNUAL", true},
		{billing.PlanBusiness, "STRIPE_PRICE_BUSINESS_ANNUAL", true},
	} {
		var regErr error
		if a.annual {
			regErr = plansRegistry.SetAnnualPriceID(a.tier, os.Getenv(a.env))
		} else {
			regErr = plansRegistry.AddPriceAlias(os.Getenv(a.env), a.tier)
		}
		if regErr != nil {
			log.Printf("billing: %s ignored: %v", a.env, regErr)
		}
	}

	webhookEventStore := billing.NewWebhookEventStore(pgPool)

	billingService := billing.NewService(
		tenantStore,
		projectStore,
		webhookEventStore,
		plansRegistry,
		os.Getenv("STRIPE_WEBHOOK_SECRET"),
		os.Getenv("BILLING_SUCCESS_URL"), // mis. https://app.trailseal.dev/billing/success
		os.Getenv("BILLING_CANCEL_URL"),
	)

	billingService.SetPortalReturnURL(os.Getenv("BILLING_PORTAL_RETURN_URL"))
	billingService.SetAuditLogger(auditStore) // BARU
	billingService.SetRoleStore(roleStore)
	billingService.SetSubscriptionFetcher(billing.NewStripeSubscriptionFetcher())

	// ============================================================
	// PLAN RESOLVER
	//
	// Project baru harus mengetahui plan tenant PADA SAAT project
	// dibuat. Webhook Stripe hanya meng-update project yang sudah
	// ada ketika webhook diproses.
	//
	// Package project tidak import billing. Resolver ini menjadi
	// bridge dari app layer ke billing registry.
	// ============================================================

	planResolver := func(tenantID string) (int64, int) {
		free := plansRegistry.Free()
		t, err := tenantStore.Get(tenantID)
		if err != nil {
			// Tenant tidak terbaca: fail closed ke Free.
			return free.MonthlyEventLimit, free.RetentionDays
		}
		if t.PlanTier == "" {
			// Tenant belum checkout / masih Free.
			return free.MonthlyEventLimit, free.RetentionDays
		}

		plan, ok := plansRegistry.Get(
			billing.PlanTier(t.PlanTier),
		)
		if !ok {
			// Plan tenant tidak dikenali: fail closed ke Free.
			return free.MonthlyEventLimit, free.RetentionDays
		}

		return plan.MonthlyEventLimit, plan.RetentionDays
	}

	// Batas key aktif per tenant dari PlanConfig (0 = tanpa batas).
	apiKeyLimitResolver := func(tenantID string) int {
		tier := billing.PlanFree
		if t, err := tenantStore.Get(tenantID); err == nil && t.PlanTier != "" {
			tier = billing.PlanTier(t.PlanTier)
		}
		plan, ok := plansRegistry.Get(tier)
		if !ok {
			plan = plansRegistry.Free() // fail closed ke Free
		}
		return plan.MaxActiveAPIKeys
	}

	// Batas export audit per plan dari PlanConfig (0 = penuh). Fail closed ke Free.
	exportCapResolver := func(tenantID string) int {
		tier := billing.PlanFree
		if t, err := tenantStore.Get(tenantID); err == nil && t.PlanTier != "" {
			tier = billing.PlanTier(t.PlanTier)
		}
		plan, ok := plansRegistry.Get(tier)
		if !ok {
			plan = plansRegistry.Free()
		}
		return plan.AuditExportMaxEvents
	}

	// ============================================================
	// RATE LIMITER
	// ============================================================

	rateLimiterConfig := middleware.DefaultRateLimiterConfig(db)
	rateLimiterConfig.MCPLimitResolver = func(tenantID string) uint64 {
		tier := billing.PlanFree
		if t, err := tenantStore.Get(tenantID); err == nil && t.PlanTier != "" {
			tier = billing.PlanTier(t.PlanTier)
		}
		plan, ok := plansRegistry.Get(tier)
		if !ok {
			plan = plansRegistry.Free() // fail closed ke Free
		}
		if plan.MCPRequestsPerMinute <= 0 {
			return 0 // fallback ke DefaultMCPLimit
		}
		return uint64(plan.MCPRequestsPerMinute)
	}
	rateLimiter := middleware.NewRateLimiter(rateLimiterConfig)

	// ============================================================
	// LOGIN ATTEMPT LIMITER
	// ============================================================

	loginLimiter := auth.NewLoginAttemptLimiter()

	// ============================================================
	// CREDENTIAL STUFFING LIMITER (IP-only)
	//
	// loginLimiter above is keyed IP+email, so an attacker can
	// still spam many different emails from the same IP without
	// tripping it. This limiter is keyed IP-only (30/minute — see
	// auth/login_security.go) and is checked in ADDITION to
	// loginLimiter on both /api/auth/register and /api/auth/login,
	// not instead of it.
	// ============================================================

	stuffingLimiter := auth.NewCredentialStuffingLimiter()

	accountLockout := auth.NewAccountLockout()

	capEngine := capability.NewEngine()
	roleResolver := rbac.NewRoleResolver(newPlanAwareRoleStore(roleStore, tenantStore))

	// ============================================================
	// AUTH MIDDLEWARE — JWT (dashboard/user side)
	// ============================================================

	requireAuth := middleware.RequireAuth(
		[]byte(signingKey),
		userStore,
		revocationStore,
	)

	requireRoot := middleware.NewRequireRoot(rootAPIKey)

	// ============================================================
	// AUTH MIDDLEWARE — API KEY (programmatic /v1/events + /mcp/v1/rpc)
	// Gate plan: Free hanya mengizinkan key aktif paling lama (api_key_suspended_plan).
	// ============================================================

	requireAPIKey := middleware.RequireAPIKey(apiKeyStore, newAPIKeyPlanGate(apiKeyStore, apiKeyLimitResolver))

	// ============================================================
	// MCP GATEWAY
	// ============================================================

	mcpGateway := mcp.NewGateway(agentStore, auditStore)
	mcpGateway.SetQuotaEnforcer(tenantStore)

	// ============================================================
	// MCP JSON-RPC SERVER (protocol-compliant: initialize / tools/list
	// / tools/call) — dipakai oleh client eksternal seperti Cursor,
	// Claude Desktop, MCP Inspector. SEBELUMNYA tidak pernah di-mount
	// ke HTTP app sehingga client eksternal tidak bisa connect sama
	// sekali walau kodenya sudah ada & teruji di mcp/server_test.go.
	// ============================================================

	mcpServer := mcp.NewServer(mcpGateway)

	// Custom prompt store (Business tier) — mengikuti pola persis
	// rbac.NewBoltRoleStore(db) di atas. SetCustomPromptStore adalah
	// setter opsional pasca-construct (lihat komentar di server.go),
	// jadi baris ini aman ditambahkan tanpa mengubah NewServer maupun
	// test lama yang memanggil mcp.NewServer(gateway) langsung.
	customPromptStore, err := mcp.NewBoltCustomPromptStore(db)
	if err != nil {
		return nil, nil, err
	}
	mcpServer.SetCustomPromptStore(customPromptStore)

	// ============================================================
	// FIBER APP
	// ============================================================

	app := fiber.New(fiber.Config{
		AppName: "TrailSeal API",
		Views:   views,
	})

	// HARUS paling awal: recover dari panic apa pun di handler manapun
	// di bawahnya, laporkan ke Sentry (#4) DAN Slack/Discord (#5) untuk
	// panic, lalu balas 500 alih-alih koneksi mati. 5xx non-panic juga
	// dilaporkan ke Sentry di sini (lihat observability/middleware.go).
	app.Use(observability.ErrorReporting(func(title, message string, fields map[string]string) {
		alerts.NotifyCritical("panic:"+fields["path"], title, message, fields)
	}))

	if err := validateProductionEnv(); err != nil {
		return nil, nil, err
	}
	corsOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if corsOrigins == "" {
		corsOrigins = "http://localhost:3000" // fallback dev — di production WAJIB di-set lewat env var
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins, // comma-separated kalau lebih dari satu origin (mis. www + app subdomain)
		AllowMethods:     "GET,POST,PUT,DELETE",
		AllowCredentials: true,
	}))

	// ============================================================
	// STATIC LANDING PAGE (embedded web/)
	//
	// web.Assets is compiled into this binary (see web/embed.go) —
	// no separate static host, reverse proxy, or "web/ folder must
	// ship next to the executable" requirement. Routes below are
	// intentionally listed one by one (not a catch-all) so paths
	// stay explicit and don't accidentally shadow API routes
	// registered later.
	// ============================================================

	webRoot, err := fs.Sub(web.Assets, ".")
	if err != nil {
		return nil, nil, err
	}

	app.Use("/static", filesystem.New(filesystem.Config{
		Root:       http.FS(webRoot),
		PathPrefix: "static",
		Browse:     false,
	}))

	serveEmbedded := func(name string) fiber.Handler {
		return func(c *fiber.Ctx) error {
			data, readErr := fs.ReadFile(webRoot, name)
			if readErr != nil {
				return c.Status(fiber.StatusNotFound).SendString("not found")
			}
			c.Set("Content-Type", "text/html; charset=utf-8")
			return c.Send(data)
		}
	}

	app.Get("/", serveEmbedded("index.html"))
	app.Get("/pricing.html", serveEmbedded("pricing.html"))
	app.Get("/security.html", serveEmbedded("security.html"))
	app.Get("/docs.html", serveEmbedded("docs.html"))
	app.Get("/api-reference.html", serveEmbedded("api-reference.html"))
	app.Get("/signup", serveEmbedded("signup.html"))
	app.Get("/login", serveEmbedded("login.html"))
	app.Get("/forgot-password", serveEmbedded("forgot-password.html"))
	app.Get("/reset-password", serveEmbedded("reset-password.html"))
	app.Get("/verify-email", serveEmbedded("verify-email.html"))
	app.Get("/resend-verification", serveEmbedded("resend-verification.html"))

	// /dashboard adalah tujuan redirect signup.html dan login.html setelah auth
	// berhasil. Dashboard ada di aplikasi Next.js terpisah; satu-satunya sumber
	// URL-nya getFrontendBaseURL() (oauth_handlers.go), bukan os.Getenv langsung.
	app.Get("/dashboard", func(c *fiber.Ctx) error {
		return c.Redirect(getFrontendBaseURL(), fiber.StatusFound)
	})

	// ============================================================
	// DEVELOPER PORTAL ROUTES (MERGED FROM cmd/developer-portal)
	// ============================================================

	// Portal HTML lama dimatikan — lihat catatan di bawah. Struct ini
	// dipertahankan supaya RegisterPortalRoutes tetap bisa compile kalau
	// nanti dihidupkan lagi dengan rantai capability yang benar.
	//nolint:staticcheck // sengaja tidak dipakai
	portalStores := PortalStores{
		TenantStore:   tenantStore,
		ProjectStore:  projectStore,
		APIKeyStore:   apiKeyStore,
		UsageStore:    eventStore,
		AuditStore:    auditStore,    // BARU — sudah ada sbg var lokal
		PlansRegistry: plansRegistry, // BARU — sudah ada sbg var lokal
		EventsStore:   eventsStore,   // BARU — eventsStore sudah ada sbg var lokal (events.NewPostgresStore)
	}

	_ = portalStores
	// Portal HTML lama (/overview, /developer/*) SENGAJA tidak didaftarkan.
	//
	// Route-nya hanya memakai requireAuth: tanpa RequireCapability, tanpa
	// rateLimiter, tanpa AuditLogger. Akibatnya POST /developer/api-keys/generate
	// bisa dipakai Member untuk membuat API key ber-scope admin — melewati
	// pemisahan hak yang dijual sebagai fitur Pro/Business.
	//
	// Dashboard sungguhan adalah aplikasi Next.js terpisah yang memakai jalur
	// JSON ber-capability, jadi portal ini tidak dibutuhkan siapa pun.
	// Dihidupkan kembali HANYA setelah setiap route-nya diberi rantai yang sama
	// dengan jalur JSON-nya.
	_ = RegisterPortalRoutes

	// ============================================================
	// HEALTH
	// ============================================================

	// /health lama dipertahankan apa adanya — sudah dipakai agent_test.go
	// dan konfigurasi lain. Menghapusnya bukan bagian pekerjaan ini.
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
		})
	})

	// Liveness vs readiness — lihat health_handlers.go.
	//
	// Didaftarkan di sini, sebelum requireAuth/rateLimiter/AuditLogger,
	// supaya pemantau eksternal bisa mencapainya tanpa kredensial dan
	// tanpa menulis 43.200 baris audit per bulan.
	app.Get("/healthz", HealthzHandler())
	app.Get("/readyz", ReadyzHandler(db, pgPool))

	// ============================================================
	// POST /api/admin/tenants
	// ============================================================

	// Snapshot bbolt. requireRoot, BUKAN JWT tenant: endpoint ini
	// mengeluarkan SELURUH database — semua tenant, user, hash password,
	// hash API key, refresh token. Siapa pun yang bisa memanggilnya
	// memiliki seluruh produk.
	app.Get(
		"/api/admin/backup/bolt",
		requireRoot,
		BoltBackupHandler(db),
	)

	app.Post(
		"/api/admin/tenants",
		requireRoot,
		func(c *fiber.Ctx) error {
			var req struct {
				TenantID string `json:"tenant_id"`
				Name     string `json:"name"`
			}

			if err := c.BodyParser(&req); err != nil {
				return c.Status(
					fiber.StatusBadRequest,
				).JSON(fiber.Map{
					"error": "invalid_body",
				})
			}

			// Tenant ID menjadi bagian dari string capability
			// "tenant.<id>.<resource>", dan ekstraksinya memotong di titik
			// PERTAMA (rbac/tenant_boundary.go). Tenant ID yang mengandung
			// titik membuat setiap pemeriksaan capability-nya gagal dengan
			// tenant_mismatch — tenant lumpuh total, dan pesan errornya
			// tidak menunjukkan sebabnya.
			for _, r := range req.TenantID {
				ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
					(r >= '0' && r <= '9') || r == '-' || r == '_'
				if !ok {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"error":   "invalid_tenant_id",
						"message": "tenant_id hanya boleh huruf, angka, '-', dan '_'",
					})
				}
			}

			t, err := tenant.ProvisionTenant(
				tenantStore,
				roleStore,
				req.TenantID,
				req.Name,
			)
			if err != nil {
				return c.Status(
					fiber.StatusConflict,
				).JSON(fiber.Map{
					"error": err.Error(),
				})
			}

			return c.JSON(fiber.Map{
				"tenant": t,
			})
		},
	)

	// ============================================================
	// DELETE /api/admin/tenants/:id
	// ============================================================

	app.Delete(
		"/api/admin/tenants/:id",
		requireRoot,
		func(c *fiber.Ctx) error {
			id := c.Params("id")

			result, err := tenant.DeprovisionTenant(
				tenantStore,
				roleStore,
				userStore,
				id,
			)
			if err != nil {
				return c.Status(
					fiber.StatusNotFound,
				).JSON(fiber.Map{
					"error": err.Error(),
				})
			}

			return c.JSON(fiber.Map{
				"result": result,
			})
		},
	)

	// ============================================================
	// POST /api/admin/tenants/:id/bootstrap-owner
	// ============================================================

	app.Post(
		"/api/admin/tenants/:id/bootstrap-owner",
		requireRoot,
		func(c *fiber.Ctx) error {
			tenantID := c.Params("id")

			var req struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}

			if err := c.BodyParser(&req); err != nil {
				return c.Status(
					fiber.StatusBadRequest,
				).JSON(fiber.Map{
					"error": "invalid_body",
				})
			}

			if req.Email == "" || req.Password == "" {
				return c.Status(
					fiber.StatusBadRequest,
				).JSON(fiber.Map{
					"error": "email and password are required",
				})
			}

			if _, err := tenantStore.Get(tenantID); err != nil {
				return c.Status(
					fiber.StatusNotFound,
				).JSON(fiber.Map{
					"error": "tenant_not_found",
				})
			}

			if _, err := roleStore.GetRole(
				tenantID,
				"owner",
			); err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "owner_role_missing",
				})
			}

			if userStore.HasOwner(tenantID) {
				return c.Status(
					fiber.StatusConflict,
				).JSON(fiber.Map{
					"error": "tenant_owner_already_exists",
				})
			}

			if _, err := userStore.FindByEmail(req.Email); err == nil {
				return c.Status(
					fiber.StatusConflict,
				).JSON(fiber.Map{
					"error": "email_already_registered",
				})
			}

			hash, err := auth.HashPassword(req.Password)
			if err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "hash_failed",
				})
			}

			now := time.Now()
			user := &auth.User{
				ID:              newUserID(),
				TenantID:        tenantID,
				Email:           req.Email,
				PasswordHash:    hash,
				RoleID:          "owner",
				Status:          auth.UserStatusActive,
				EmailVerifiedAt: &now, // root-provisioned, bukan alur signup publik
			}

			if err := userStore.Put(user); err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": err.Error(),
				})
			}

			return c.JSON(fiber.Map{
				"user": fiber.Map{
					"id":        user.ID,
					"tenant_id": user.TenantID,
					"email":     user.Email,
					"role_id":   user.RoleID,
					"status":    user.Status,
				},
			})
		},
	)

	// ============================================================
	// GET /api/admin/tenants/:id/usage
	// ============================================================

	app.Get(
		"/api/admin/tenants/:id/usage",
		requireRoot,
		AdminUsageHandler(
			eventStore,
			tenantStore,
		),
	)

	// ============================================================
	// GET /api/admin/tenants/:id/audit
	// ============================================================

	app.Get(
		"/api/admin/tenants/:id/audit",
		requireRoot,
		AdminAuditHandler(
			auditStore,
			tenantStore,
		),
	)

	// GET /api/admin/platform/audit — event tanpa tenant (login email tak terdaftar, dll.)
	app.Get(
		"/api/admin/platform/audit",
		requireRoot,
		AdminPlatformAuditHandler(auditStore),
	)

	// ============================================================
	// GET /api/admin/agents
	// ============================================================

	app.Get(
		"/api/admin/agents",
		requireRoot,
		func(c *fiber.Ctx) error {
			agents, err := agentStore.ListAllAgents()
			if err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "failed to list agents",
				})
			}
			return c.JSON(fiber.Map{
				"agents": agents,
				"count":  len(agents),
			})
		},
	)

	// ============================================================
	// GET /api/admin/tools
	// ============================================================

	app.Get(
		"/api/admin/tools",
		requireRoot,
		func(c *fiber.Ctx) error {
			tools, err := agentStore.ListAllTools()
			if err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "failed to list tools",
				})
			}
			return c.JSON(fiber.Map{
				"tools": tools,
				"count": len(tools),
			})
		},
	)

	// ============================================================
	// POST /api/tenants/:id/users
	// ============================================================

	app.Post(
		"/api/tenants/:id/users",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.users.create",
		),
		middleware.UsageMetering(eventStore),
		func(c *fiber.Ctx) error {
			tenantID := c.Params("id")

			var req struct {
				Email    string `json:"email"`
				Password string `json:"password"`
				RoleID   string `json:"role_id"`
			}

			if err := c.BodyParser(&req); err != nil {
				return c.Status(
					fiber.StatusBadRequest,
				).JSON(fiber.Map{
					"error": "invalid_body",
				})
			}

			if req.Email == "" ||
				req.Password == "" ||
				req.RoleID == "" {
				return c.Status(
					fiber.StatusBadRequest,
				).JSON(fiber.Map{
					"error": "email, password, role_id are required",
				})
			}

			tn, tnErr := tenantStore.Get(tenantID)
			if tnErr != nil {
				return c.Status(
					fiber.StatusNotFound,
				).JSON(fiber.Map{
					"error": "tenant_not_found",
				})
			}

			// Free = Owner saja (tabel pricing). Anggota tim hanya untuk Pro/Business.
			// PlanTier kosong dianggap Free, sama dengan planResolver.
			if !billing.PlanAllowsTeamSeats(tn.PlanTier) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error":         "plan_upgrade_required",
					"required_plan": "pro",
					"message":       "Team members are available on Pro and Business plans.",
				})
			}

			// Role owner hanya boleh diberikan oleh owner. Tanpa ini, admin
			// (yang punya users.create) bisa membuat akun owner dan memperoleh
			// hak billing/purge yang sengaja hanya untuk Owner.
			if req.RoleID == "owner" {
				if cl, ok := middleware.ClaimsFromFiberCtx(c); !ok || cl.RoleID != "owner" {
					return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
						"error": "owner_role_requires_owner",
					})
				}
			}

			// Role kustom (Business): hanya di plan Business dan hanya boleh
			// diberikan oleh owner, supaya admin tidak bisa menaikkan hak lewat
			// role kustom.
			if isCustomRoleID(req.RoleID) {
				if !billing.PlanAllowsCustomRoles(tn.PlanTier) {
					return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
						"error":         "plan_upgrade_required",
						"required_plan": "business",
						"message":       "Custom roles are available on the Business plan.",
					})
				}
				if cl, ok := middleware.ClaimsFromFiberCtx(c); !ok || cl.RoleID != "owner" {
					return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
						"error": "custom_role_requires_owner",
					})
				}
			}

			if _, err := roleStore.GetRole(
				tenantID,
				req.RoleID,
			); err != nil {
				return c.Status(
					fiber.StatusBadRequest,
				).JSON(fiber.Map{
					"error": "role_not_found_in_tenant",
				})
			}

			if _, err := userStore.FindByEmail(req.Email); err == nil {
				return c.Status(
					fiber.StatusConflict,
				).JSON(fiber.Map{
					"error": "email_already_registered",
				})
			}

			hash, err := auth.HashPassword(req.Password)
			if err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "hash_failed",
				})
			}

			now := time.Now()
			user := &auth.User{
				ID:              newUserID(),
				TenantID:        tenantID,
				Email:           req.Email,
				PasswordHash:    hash,
				RoleID:          req.RoleID,
				Status:          auth.UserStatusActive,
				EmailVerifiedAt: &now, // diundang admin tenant, bukan self-register
			}

			if err := userStore.Put(user); err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": err.Error(),
				})
			}

			return c.JSON(fiber.Map{
				"user": fiber.Map{
					"id":        user.ID,
					"tenant_id": user.TenantID,
					"email":     user.Email,
					"role_id":   user.RoleID,
					"status":    user.Status,
				},
			})
		},
	)

	// ============================================================
	// GET /api/tenants/:id/users
	// ============================================================

	app.Get(
		"/api/tenants/:id/users",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.users.create",
		),
		func(c *fiber.Ctx) error {
			tenantID := c.Params("id")

			if _, err := tenantStore.Get(tenantID); err != nil {
				return c.Status(
					fiber.StatusNotFound,
				).JSON(fiber.Map{
					"error": "tenant_not_found",
				})
			}

			users, err := userStore.ListAllForTenant(tenantID)
			if err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "failed_to_list_users",
				})
			}

			out := make([]fiber.Map, 0, len(users))
			for _, user := range users {
				out = append(out, fiber.Map{
					"id":        user.ID,
					"tenant_id": user.TenantID,
					"email":     user.Email,
					"role_id":   user.RoleID,
					"status":    user.Status,
				})
			}

			return c.JSON(fiber.Map{
				"users": out,
				"count": len(out),
			})
		},
	)

	// ============================================================
	// GET /api/tenants/:id/roles
	// ============================================================

	app.Get(
		"/api/tenants/:id/roles",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.users.create",
		),
		TenantRolesHandler(roleStore),
	)

	// ============================================================
	// CUSTOM ROLES (Business) — /api/tenants/:id/custom-roles
	// Kelola hanya owner (roles.write lewat wildcard owner).
	// ============================================================

	customRoles := newCustomRoleHandlers(tenantStore, roleStore, userStore)

	app.Get(
		"/api/tenants/:id/custom-roles",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.users.create",
		),
		customRoles.List,
	)

	app.Post(
		"/api/tenants/:id/custom-roles",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.roles.write",
		),
		customRoles.Create,
	)

	app.Put(
		"/api/tenants/:id/custom-roles/:role_id",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.roles.write",
		),
		customRoles.Update,
	)

	app.Delete(
		"/api/tenants/:id/custom-roles/:role_id",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.roles.write",
		),
		customRoles.Delete,
	)

	// ============================================================
	// GET /api/tenants/:id/usage
	// ============================================================

	app.Get(
		"/api/tenants/:id/usage",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.usage.read",
		),
		middleware.UsageMetering(eventStore),
		TenantUsageHandler(eventStore, tenantStore, eventsStore),
	)

	// ============================================================
	// GET /api/tenants/:id/usage/history — Phase 3, ROADMAP §8
	// ============================================================

	app.Get(
		"/api/tenants/:id/usage/history",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.usage.read",
		),
		TenantUsageHistoryHandler(eventStore, tenantStore, projectStore, apiKeyStore),
	)

	// ============================================================
	// GET /api/tenants/:id/audit
	// ============================================================

	app.Get(
		"/api/tenants/:id/audit",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.audit.read",
		),
		TenantAuditHandler(auditStore),
	)

	app.Get(
		"/api/tenants/:id/security-events",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.security_events.read",
		),
		SecurityEventsHandler(auditStore),
	)

	// ============================================================
	// AGENT ROUTES
	// ============================================================

	app.Post(
		"/api/tenants/:id/agents",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.agent.write",
		),
		CreateAgentHandler(agentStore),
	)

	app.Get(
		"/api/tenants/:id/agents",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.agent.read",
		),
		ListAgentsHandler(agentStore),
	)

	app.Get(
		"/api/tenants/:id/agents/:agent_id",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.agent.read",
		),
		GetAgentHandler(agentStore),
	)

	app.Put(
		"/api/tenants/:id/agents/:agent_id/status",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.agent.write",
		),
		UpdateAgentStatusHandler(agentStore),
	)

	app.Delete(
		"/api/tenants/:id/agents/:agent_id",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.agent.write",
		),
		DeleteAgentHandler(agentStore),
	)

	// ============================================================
	// TOOL ROUTES
	// ============================================================

	app.Post(
		"/api/tenants/:id/tools",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.tool.write",
		),
		CreateToolHandler(agentStore),
	)

	app.Get(
		"/api/tenants/:id/tools",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.tool.read",
		),
		ListToolsHandler(agentStore),
	)

	app.Get(
		"/api/tenants/:id/tools/:tool_id",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.tool.read",
		),
		GetToolHandler(agentStore),
	)

	app.Delete(
		"/api/tenants/:id/tools/:tool_id",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.tool.write",
		),
		DeleteToolHandler(agentStore),
	)

	app.Post(
		"/api/tenants/:id/tools/:tool_id/execute",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.tool.execute",
		),
		ExecuteToolHandler(agentStore),
	)

	// ============================================================
	// MCP ROUTES
	// ============================================================

	app.Post(
		"/api/tenants/:id/mcp/query",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.mcp.query",
		),
		MCPQueryHandler(mcpGateway),
	)

	// ============================================================
	// MCP JSON-RPC ROUTE (protocol-compliant untuk client eksternal)
	//
	// server.go men-trust header X-Tenant-ID/X-User-ID sebagai
	// authoritative identity. Karena itu, SEBELUM diteruskan ke
	// mcpServer.ServeHTTP, kedua header ini WAJIB ditimpa paksa dari
	// JWT ter-autentikasi (claims) — nilai apapun yang dikirim client
	// di header itu harus diabaikan/di-overwrite, supaya caller tidak
	// bisa menyamar sebagai tenant/user lain.
	// ============================================================

	app.Post(
		"/api/tenants/:id/mcp/rpc",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.mcp.query",
		),
		middleware.MCPUsageMeteringDashboard(eventStore),
		func(c *fiber.Ctx) error {
			claims, ok := middleware.ClaimsFromFiberCtx(c)
			if !ok || claims == nil {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "unauthenticated",
				})
			}

			// KEAMANAN: override paksa, JANGAN percaya header dari client.
			// RequireTenantScope("id") sudah memastikan c.Params("id")
			// cocok dengan tenant milik claims (atau root), jadi path
			// param dipakai sebagai tenantID authoritative — sama seperti
			// pola /mcp/query yang sudah ada.
			c.Request().Header.Set("X-Tenant-ID", c.Params("id"))
			c.Request().Header.Set("X-User-ID", claims.UserID)

			// BARU (prompts/create — custom prompt, Business tier):
			// X-Role-ID/X-Plan-Tier ditimpa paksa sama seperti dua
			// header di atas — TIDAK boleh dipercaya dari client.
			// roleID dipakai mcp.Server HANYA untuk gate
			// "prompts/create" (Owner-only); method lain tidak
			// menyentuh header ini sama sekali.
			c.Request().Header.Set("X-Role-ID", claims.RoleID)

			planTier := "free"
			if t, err := tenantStore.Get(c.Params("id")); err == nil && t.PlanTier != "" {
				planTier = t.PlanTier
			}
			c.Request().Header.Set("X-Plan-Tier", planTier)

			return adaptor.HTTPHandlerFunc(mcpServer.ServeHTTP)(c)
		},
	)

	// DELETE = terminate MCP session (Streamable HTTP), jalur dashboard/JWT.
	// Identitas tenant/user tetap authoritative dari claims (bukan header
	// client), sehingga sesi hanya bisa diterminate oleh pemiliknya.
	// Tanpa lookup role/plan: terminate tidak memakainya.
	app.Delete(
		"/api/tenants/:id/mcp/rpc",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.mcp.query",
		),
		func(c *fiber.Ctx) error {
			claims, ok := middleware.ClaimsFromFiberCtx(c)
			if !ok || claims == nil {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "unauthenticated",
				})
			}

			c.Request().Header.Set("X-Tenant-ID", c.Params("id"))
			c.Request().Header.Set("X-User-ID", claims.UserID)

			return adaptor.HTTPHandlerFunc(mcpServer.ServeHTTP)(c)
		},
	)

	// ============================================================
	// MCP NATIVE ENDPOINT — untuk client MCP generic (Claude Desktop,
	// Cursor) yang autentikasi via API key, bukan JWT dashboard.
	// Terpisah dari /api/tenants/:id/mcp/rpc di atas — jangan gabung.
	// ============================================================

	app.Post(
		"/mcp/v1/rpc",
		requireAPIKey,
		middleware.RequireScope(func(s apikey.Scope) bool {
			return s.CanRead()
		}),
		rateLimiter,
		middleware.AuditLogger(auditStore),
		middleware.InjectMCPIdentityFromAPIKey(),
		middleware.MCPUsageMetering(eventStore),
		adaptor.HTTPHandlerFunc(mcpServer.ServeHTTP),
	)

	// DELETE = terminate MCP session (Streamable HTTP). Tanpa
	// MCPUsageMetering: terminate bukan pemakaian tool dan body-nya
	// kosong, jadi tidak boleh masuk hitungan usage/kuota.
	app.Delete(
		"/mcp/v1/rpc",
		requireAPIKey,
		middleware.RequireScope(func(s apikey.Scope) bool {
			return s.CanRead()
		}),
		rateLimiter,
		middleware.AuditLogger(auditStore),
		middleware.InjectMCPIdentityFromAPIKey(),
		adaptor.HTTPHandlerFunc(mcpServer.ServeHTTP),
	)

	// ============================================================
	// PROJECT ROUTES — JWT-authed, dashboard side.
	// Chain sama persis dengan AGENT/TOOL routes di atas: RequireAuth
	// → RequireTenantScope → RateLimiter → RequireCapability.
	// ============================================================

	app.Post(
		"/api/tenants/:id/projects",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.projects.write",
		),
		project.CreateProjectHandler(projectStore, planResolver),
	)

	app.Get(
		"/api/tenants/:id/projects",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.projects.read",
		),
		project.ListProjectsHandler(projectStore),
	)

	app.Get(
		"/api/tenants/:id/projects/:project_id",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.projects.read",
		),
		project.GetProjectHandler(projectStore),
	)

	app.Delete(
		"/api/tenants/:id/projects/:project_id",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.projects.write",
		),
		project.DeleteProjectHandler(projectStore),
	)

	// ============================================================
	// BILLING ROUTES
	// ============================================================

	app.Post(
		"/api/tenants/:id/billing/checkout",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.billing.write",
		),
		billingService.CreateCheckoutHandler(),
	)

	app.Post(
		"/api/tenants/:id/billing/portal",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.billing.write",
		),
		billingService.CreatePortalHandler(),
	)

	app.Post(
		"/api/billing/webhook",
		billingService.WebhookHandler(),
	)

	// ============================================================
	// API KEY ROUTES — JWT-authed, dashboard side.
	// ============================================================

	app.Post(
		"/api/tenants/:id/projects/:project_id/keys",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.apikeys.write",
		),
		apikey.CreateAPIKeyHandler(apiKeyStore, projectStore, apiKeyLimitResolver),
	)

	app.Get(
		"/api/tenants/:id/projects/:project_id/keys",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.apikeys.read",
		),
		apikey.ListAPIKeysHandler(apiKeyStore),
	)

	app.Post(
		"/api/tenants/:id/projects/:project_id/keys/:key_id/revoke",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.apikeys.write",
		),
		apikey.RevokeAPIKeyHandler(apiKeyStore),
	)

	// ============================================================
	// EVENTS ROUTES — API-KEY-authed, BUKAN JWT. Ini yang dipanggil
	// programmatic dari server customer. Project ditentukan SEPENUHNYA
	// dari API key (requireAPIKey), TIDAK ADA project_id di URL.
	// ============================================================

	app.Post(
		"/v1/events",
		requireAPIKey,
		// Satu pelanggan dengan loop yang salah bisa membanjiri ingest
		// sepanjang malam. Limiter ini SUDAH mendukung jalur API key
		// (rate_limit.go mengambil tenant dari APIKeyFromFiberCtx), jadi
		// cukup dipasang — tidak perlu limiter kedua.
		rateLimiter,
		middleware.RequireScope(events.ScopeCanIngest),
		events.IngestHandler(eventsStore, eventStore, auditStore, tenantStore),
	)

	app.Post(
		"/v1/events/batch",
		requireAPIKey,
		// 500 event per panggilan. Tanpa limiter di sini, limiter pada
		// POST /v1/events tidak ada artinya — pemanggil tinggal pindah
		// ke endpoint ini.
		rateLimiter,
		middleware.RequireScope(events.ScopeCanIngest),
		events.BatchIngestHandler(eventsStore, eventStore, auditStore, tenantStore),
	)

	app.Get(
		"/v1/events",
		requireAPIKey,
		rateLimiter,
		middleware.RequireScope(events.ScopeCanRead),
		events.QueryHandler(eventsStore),
	)

	app.Post(
		"/v1/events/verify",
		requireAPIKey,
		// VerifyRange memuat seluruh event project ke memori:
		// 1 juta event = 74,7 detik dan +582 MB (lihat pengukuran di
		// verify_incremental.go). Endpoint termahal di jalur API key.
		rateLimiter,
		middleware.RequireScope(events.ScopeCanRead),
		events.VerifyHandler(eventsStore),
	)

	// GET /v1/events/export — export sebagai JSON/CSV (#8). Scope
	// sama dengan GET /v1/events (baca-saja).
	app.Get(
		"/v1/events/export",
		requireAPIKey,
		// Sampai 100.000 event dimuat ke memori per panggilan.
		rateLimiter,
		middleware.RequireScope(events.ScopeCanRead),
		events.ExportHandler(eventsStore, exportCapResolver),
	)

	// ============================================================
	// COMPLIANCE ROUTES — GDPR Deletion
	// ============================================================

	app.Get(
		"/api/tenants/:id/data/status",
		middleware.RequireAuthAllowDeletedUser(
			[]byte(signingKey),
			userStore,
			revocationStore,
		),
		middleware.RequireTenantScope("id"),
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.compliance.read",
		),
		TenantDeletionStatusHandler(tenantStore),
	)

	app.Post(
		"/api/tenants/:id/data/delete",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.compliance.delete",
		),
		SoftDeleteTenantHandler(tenantStore, userStore, agentStore),
	)

	app.Post(
		"/api/tenants/:id/data/restore",
		middleware.RequireAuthAllowDeletedUser(
			[]byte(signingKey),
			userStore,
			revocationStore,
		),
		middleware.RequireTenantScope("id"),
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.compliance.restore",
		),
		RestoreTenantHandler(tenantStore, userStore, agentStore),
	)

	app.Post(
		"/api/tenants/:id/data/purge",
		middleware.RequireAuthAllowDeletedUser(
			[]byte(signingKey),
			userStore,
			revocationStore,
		),
		// Hard delete permanen. Kalau satu route di seluruh produk ini
		// harus tercatat, route inilah. AuditLogger menulis SETELAH
		// c.Next(), jadi catatannya tidak ikut terhapus.
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.compliance.purge",
		),
		PurgeTenantHandler(tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore),
	)

	// ============================================================
	// ADMIN COMPLIANCE ROUTES
	// ============================================================

	app.Post(
		"/api/admin/tenants/:id/data/delete",
		requireRoot,
		AdminSoftDeleteTenantHandler(tenantStore, userStore, agentStore),
	)

	app.Post(
		"/api/admin/tenants/:id/data/purge",
		requireRoot,
		AdminPurgeTenantHandler(tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore),
	)

	// ============================================================
	// AUDIT RETENTION ROUTES — tenant-scoped self-service (Phase 9)
	//
	// PERBAIKAN: sebelumnya di bawah /api/admin (requireRoot) dan
	// RetentionStore GLOBAL (1 policy untuk semua tenant). Sekarang
	// tenant-scoped penuh, pakai planResolver yang sama dengan Projects
	// supaya retention_days ceiling konsisten satu sumber kebenaran.
	//
	// Capability "audit.retention.*" SENGAJA Owner-only untuk sekarang
	// (belum ditambahkan ke grant list Admin di provisionRolesForPlan) —
	// konsisten dengan compliance.*/billing.write yang juga Owner-only.
	// Kalau mau dibuka ke Admin, perlu update capability grants terpisah,
	// bukan bagian dari perubahan ini.
	// ============================================================

	// ============================================================
	// GET/POST /api/tenants/:id/audit/verify — Phase 2, ROADMAP §7
	// ============================================================

	app.Get(
		"/api/tenants/:id/audit/verify",
		requireAuth,
		middleware.RequireTenantScope("id"),
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.audit.read",
		),
		AuditVerifyHandler(pgPool, tenantStore, eventsStore, projectStore, false),
	)

	app.Post(
		"/api/tenants/:id/audit/verify",
		requireAuth,
		middleware.RequireTenantScope("id"),
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.audit.read",
		),
		AuditVerifyHandler(pgPool, tenantStore, eventsStore, projectStore, true),
	)

	app.Get(
		"/api/tenants/:id/audit/retention",
		requireAuth,
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.audit.retention.read",
		),
		GetRetentionPolicyHandler(retentionStore, planResolver),
	)

	app.Put(
		"/api/tenants/:id/audit/retention",
		requireAuth,
		// Mempersempit retensi = memperkecil jendela bukti. Justru
		// tindakan inilah yang paling perlu tercatat.
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.audit.retention.write",
		),
		UpdateRetentionPolicyHandler(retentionStore, planResolver),
	)

	app.Post(
		"/api/tenants/:id/audit/retention/cleanup",
		requireAuth,
		middleware.AuditLogger(auditStore),
		middleware.RequireTenantScope("id"),
		rateLimiter,
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.audit.retention.write",
		),
		TriggerCleanupHandler(auditStore, retentionStore, planResolver),
	)

	app.Get(
		"/api/tenants/:id/audit/export",
		requireAuth,
		middleware.RequireTenantScope("id"),
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.audit.export",
		),
		ExportAuditHandler(auditStore, exportCapResolver),
	)

	app.Get(
		"/api/tenants/:id/notification-settings",
		requireAuth,
		middleware.RequireTenantScope("id"),
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.notifications.read",
		),
		GetNotificationSettingsHandler(tenantStore),
	)

	app.Put(
		"/api/tenants/:id/notification-settings",
		requireAuth,
		middleware.RequireTenantScope("id"),
		middleware.RequireCapability(
			capEngine,
			roleResolver,
			"tenant.{tenant_id}.notifications.write",
		),
		UpdateNotificationSettingsHandler(tenantStore),
	)
	// ============================================================
	// POST /api/auth/register
	//
	// Public self-serve signup: creates a new tenant + owner user in
	// one request and logs the new owner in immediately (same token
	// issuance as /api/auth/login below).
	//
	// Unlike /api/admin/tenants + /api/admin/tenants/:id/bootstrap-owner
	// (both requireRoot-gated, meant for internal/admin provisioning),
	// this endpoint is intentionally public — it is the ONLY place in
	// the API that creates a tenant without a root credential. It
	// reuses the same internal building blocks (tenant.ProvisionTenant,
	// the owner-role capability grant, auth.HashPassword) so a
	// self-serve tenant ends up in exactly the same state as an
	// admin-provisioned one.
	//
	// SECURITY NOTE: making tenant creation public is a real change in
	// attack surface versus the rest of this API, which gates it behind
	// SAAS_ROOT_API_KEY everywhere else. Before this goes to production,
	// consider: (1) email verification before the account is usable,
	// not just created; (2) a CAPTCHA or similar on this route — bcrypt
	// cost 12 plus three store writes per request is a meaningful cost
	// to give an unauthenticated caller; (3) whether loginLimiter's
	// IP+email keying is sufficient, or whether the IP-only
	// CredentialStuffingLimiter in auth/login_security.go should also
	// apply here, since an attacker can vary the email per request.
	//
	// ATOMICITY NOTE: tenant, role, and user live in three separate
	// stores (tenant/, rbac/, auth/) with no shared transaction across
	// them — the same limitation already documented in
	// tenant.DeprovisionTenant's own comments. If tenant+role
	// provisioning succeeds but user creation fails, this handler
	// deprovisions the tenant it just created as a best-effort
	// rollback, rather than leaving an owner-less "ghost" tenant behind.
	// ============================================================

	app.Post(
		"/api/auth/register",
		func(c *fiber.Ctx) error {
			var req struct {
				CompanyName string `json:"company_name"`
				Email       string `json:"email"`
				Password    string `json:"password"`
				Plan        string `json:"plan"`
				Interval    string `json:"interval"`
			}

			if err := c.BodyParser(&req); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_body",
				})
			}

			req.CompanyName = strings.TrimSpace(req.CompanyName)
			req.Email = strings.TrimSpace(req.Email)

			if req.CompanyName == "" || req.Email == "" || req.Password == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "company_name, email, and password are required",
				})
			}

			// New rule, specific to public signup — nothing enforced a
			// minimum before because only root-gated admin endpoints
			// created users. Adjust to your actual password policy.
			if len(req.Password) < 8 {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "password must be at least 8 characters",
				})
			}

			planTier := billing.PlanTier(req.Plan)
			if planTier != billing.PlanFree &&
				planTier != billing.PlanPro &&
				planTier != billing.PlanBusiness {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error":       "invalid_plan",
					"valid_plans": []string{"free", "pro", "business"},
				})
			}

			clientIP := c.IP()

			interval, okInterval := billing.ParseBillingInterval(req.Interval)
			if !okInterval {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error":           "invalid_interval",
					"valid_intervals": []string{"monthly", "annual"},
				})
			}
			// IP-only check FIRST: catches an attacker cycling through
			// many different emails from the same IP, which the
			// IP+email limiter below cannot catch on its own.
			if !stuffingLimiter.Allow(clientIP) {
				c.Set("Retry-After", "60")
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"error": "too_many_attempts",
				})
			}
			stuffingLimiter.RecordAttempt(clientIP)

			// Reuses the same IP+email brute-force limiter as
			// /api/auth/login — a public endpoint that hashes a
			// password and writes to three stores per request needs
			// abuse protection at least as much as login does.
			if !loginLimiter.Allow(clientIP, req.Email) {
				c.Set("Retry-After", "60")
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"error": "too_many_attempts",
				})
			}

			if _, err := userStore.FindByEmail(req.Email); err == nil {
				loginLimiter.RecordFailure(clientIP, req.Email)
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"error": "email_already_registered",
				})
			}

			// Derive a unique tenant ID from the company name. Collisions
			// are expected to be rare; retry a few times with a random
			// suffix rather than failing the signup outright.
			base := slugifyCompanyName(req.CompanyName)
			tenantID := base

			var newTenant *tenant.Tenant
			var provisionErr error
			for attempt := 0; attempt < 5; attempt++ {
				newTenant, provisionErr = tenant.ProvisionTenant(
					tenantStore, roleStore, tenantID, req.CompanyName,
				)
				if provisionErr == nil {
					break
				}
				if provisionErr.Error() != "tenant_already_exists" {
					break
				}
				tenantID = base + "-" + randomTenantSuffix()
			}
			if provisionErr != nil {
				loginLimiter.RecordFailure(clientIP, req.Email)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "signup_failed",
				})
			}

			hash, err := auth.HashPassword(req.Password)
			if err != nil {
				_, _ = tenant.DeprovisionTenant(tenantStore, roleStore, userStore, tenantID)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "hash_failed",
				})
			}

			user := &auth.User{
				ID:           newUserID(),
				TenantID:     tenantID,
				Email:        req.Email,
				PasswordHash: hash,
				RoleID:       "owner",
				Status:       auth.UserStatusActive,
			}

			if err := userStore.Put(user); err != nil {
				// Best-effort rollback — see ATOMICITY NOTE above.
				_, _ = tenant.DeprovisionTenant(tenantStore, roleStore, userStore, tenantID)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "signup_failed",
				})
			}

			// Kirim email verifikasi — non-fatal: akun tetap dibuat
			// walau pengiriman gagal, user bisa resend nanti.
			if rawToken, vErr := auth.GenerateEmailVerificationToken(user.ID, emailVerificationStore); vErr == nil {
				_ = mailer.SendVerificationEmail(user.Email, rawToken)
			}

			loginLimiter.Reset(clientIP, req.Email)

			// Anti free-tier abuse: no auto-login before email is
			// verified. Tenant + user already exist; caller must
			// verify then call /api/auth/login separately.
			resp := fiber.Map{
				"status":  "verification_required",
				"message": "Account created. Check your email to verify before logging in.",
				"tenant": fiber.Map{
					"id":   newTenant.ID,
					"name": newTenant.Name,
				},
			}

			if planTier == billing.PlanFree {
				resp["redirect"] = "/login"
				return c.JSON(resp)
			}

			checkoutURL, err := billingService.CreateCheckoutSessionInterval(tenantID, planTier, interval)
			if err != nil {
				resp["redirect"] = "/login"
				resp["checkout_error"] = "checkout_session_failed"
				return c.Status(fiber.StatusCreated).JSON(resp)
			}

			resp["checkout_url"] = checkoutURL
			resp["redirect"] = checkoutURL
			return c.JSON(resp)
		},
	)

	// ============================================================
	// POST /api/auth/login
	// ============================================================

	app.Post(
		"/api/auth/login",
		func(c *fiber.Ctx) error {
			var req struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}

			if err := c.BodyParser(&req); err != nil {
				return c.Status(
					fiber.StatusBadRequest,
				).JSON(fiber.Map{
					"error": "invalid_body",
				})
			}

			clientIP := c.IP()
			email := req.Email

			// IP-only check FIRST — same rationale as
			// /api/auth/register: catches many different emails
			// tried from one IP, which loginLimiter's IP+email
			// keying cannot catch on its own.
			if !stuffingLimiter.Allow(clientIP) {
				c.Set("Retry-After", "60")
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"error": "too_many_login_attempts",
				})
			}
			stuffingLimiter.RecordAttempt(clientIP)

			if !loginLimiter.Allow(clientIP, email) {
				c.Set(
					"Retry-After",
					"60",
				)

				return c.Status(
					fiber.StatusTooManyRequests,
				).JSON(fiber.Map{
					"error": "too_many_login_attempts",
				})
			}

			if accountLockout.IsLocked(email) {
				audit.RecordPlatformLoginAttempt(auditStore, clientIP, email,
					string(c.Request().Header.Peek("User-Agent")), "account_locked")
				return c.Status(
					fiber.StatusForbidden,
				).JSON(fiber.Map{
					"error": "account_locked",
				})
			}

			user, err := userStore.FindByEmail(req.Email)
			if err != nil {
				loginLimiter.RecordFailure(
					clientIP,
					email,
				)
				accountLockout.RecordFailure(email)
				audit.RecordPlatformLoginAttempt(auditStore, clientIP, email,
					string(c.Request().Header.Peek("User-Agent")), "unknown_email")

				return c.Status(
					fiber.StatusUnauthorized,
				).JSON(fiber.Map{
					"error": "invalid_credentials",
				})
			}

			if err := auth.VerifyPassword(
				user.PasswordHash,
				req.Password,
			); err != nil {
				loginLimiter.RecordFailure(
					clientIP,
					email,
				)
				accountLockout.RecordFailure(email)

				if saveErr := audit.RecordEvent(
					auditStore,
					user.TenantID,
					user.ID,
					audit.ActionLoginFailed,
					"/api/auth/login",
					audit.OutcomeFailure,
					map[string]string{
						"method":     c.Method(),
						"path":       c.Path(),
						"status":     "401",
						"ip":         clientIP,
						"user_agent": string(c.Request().Header.Peek("User-Agent")),
					},
				); saveErr != nil {
					log.Printf("WARN: audit logging failed for login_failed (tenant %s): %v", user.TenantID, saveErr)
				}

				return c.Status(
					fiber.StatusUnauthorized,
				).JSON(fiber.Map{
					"error": "invalid_credentials",
				})
			}

			if user.DeletedAt != nil {
				loginLimiter.RecordFailure(
					clientIP,
					email,
				)
				audit.RecordLoginBlocked(auditStore, user.TenantID, user.ID, clientIP,
					string(c.Request().Header.Peek("User-Agent")), "account_deleted")
				return c.Status(
					fiber.StatusForbidden,
				).JSON(fiber.Map{
					"error": "account_deleted",
				})
			}
			if user.Status != auth.UserStatusActive {
				loginLimiter.RecordFailure(
					clientIP,
					email,
				)
				audit.RecordLoginBlocked(auditStore, user.TenantID, user.ID, clientIP,
					string(c.Request().Header.Peek("User-Agent")), "user_suspended")

				return c.Status(
					fiber.StatusForbidden,
				).JSON(fiber.Map{
					"error": "user_suspended",
				})
			}

			if user.EmailVerifiedAt == nil {
				loginLimiter.RecordFailure(clientIP, email)
				audit.RecordLoginBlocked(auditStore, user.TenantID, user.ID, clientIP,
					string(c.Request().Header.Peek("User-Agent")), "email_not_verified")
				return c.Status(
					fiber.StatusForbidden,
				).JSON(fiber.Map{
					"error": "email_not_verified",
				})
			}

			loginLimiter.Reset(
				clientIP,
				email,
			)
			accountLockout.Reset(email)

			accessToken, err := auth.GenerateAccessToken(
				user.ID,
				user.TenantID,
				user.RoleID,
				[]byte(signingKey),
			)
			if err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "token_generation_failed",
				})
			}

			refreshToken, err := auth.GenerateRefreshToken(
				user.ID,
				refreshStore,
			)
			if err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "refresh_token_generation_failed",
				})
			}

			setAccessCookie(c, accessToken, auth.AccessTokenTTL)

			setRefreshCookie(c, refreshToken, auth.RefreshTokenTTL)
			return c.JSON(fiber.Map{
				"access_token":  accessToken,
				"refresh_token": refreshToken,
				"token_type":    "Bearer",
				"expires_in":    int(auth.AccessTokenTTL.Seconds()),
			})
		},
	)

	app.Get("/api/auth/verify-email", func(c *fiber.Ctx) error {
		token := c.Query("token")
		userID, err := auth.VerifyEmailToken(token, emailVerificationStore)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		user, err := userStore.GetByID(userID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user_not_found"})
		}
		now := time.Now()
		user.EmailVerifiedAt = &now
		if err := userStore.Put(user); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "verify_failed"})
		}
		return c.JSON(fiber.Map{"status": "verified"})
	})

	app.Post("/api/auth/resend-verification", func(c *fiber.Ctx) error {
		var req struct {
			Email string `json:"email"`
		}
		_ = c.BodyParser(&req)
		if !stuffingLimiter.Allow(c.IP()) {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "too_many_attempts"})
		}
		stuffingLimiter.RecordAttempt(c.IP())
		user, err := userStore.FindByEmail(req.Email)
		if err == nil && user.EmailVerifiedAt == nil {
			if rawToken, vErr := auth.GenerateEmailVerificationToken(user.ID, emailVerificationStore); vErr == nil {
				_ = mailer.SendVerificationEmail(user.Email, rawToken)
			}
		}
		return c.JSON(fiber.Map{"status": "ok"}) // generic — no leak
	})

	app.Post("/api/auth/forgot-password", func(c *fiber.Ctx) error {
		var req struct {
			Email string `json:"email"`
		}
		_ = c.BodyParser(&req)
		if !stuffingLimiter.Allow(c.IP()) {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "too_many_attempts"})
		}
		stuffingLimiter.RecordAttempt(c.IP())
		user, err := userStore.FindByEmail(req.Email)
		if err == nil {
			if rawToken, gErr := auth.GeneratePasswordResetToken(user.ID, c.IP(), passwordResetStore); gErr == nil {
				_ = mailer.SendPasswordResetEmail(user.Email, rawToken)
			}
		}
		return c.JSON(fiber.Map{"status": "ok"}) // always 200 — no leak
	})

	app.Post("/api/auth/reset-password", func(c *fiber.Ctx) error {
		var req struct {
			Token       string `json:"token"`
			NewPassword string `json:"new_password"`
		}
		if err := c.BodyParser(&req); err != nil || len(req.NewPassword) < 8 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
		}
		userID, err := auth.ConsumePasswordResetToken(req.Token, passwordResetStore)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		user, err := userStore.GetByID(userID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user_not_found"})
		}
		hash, err := auth.HashPassword(req.NewPassword)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "hash_failed"})
		}
		user.PasswordHash = hash
		if err := userStore.Put(user); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "reset_failed"})
		}
		_ = refreshStore.RevokeAllForUser(userID) // butuh method dari patch sebelumnya
		return c.JSON(fiber.Map{"status": "password_reset"})
	})

	// ============================================================
	// OAUTH ROUTES (2A.5 tahap 2b/2d/2e) — account-matching + JWT
	// issuance + redirect setelah OAuth resolve sukses.
	// ============================================================

	oauthCfg := auth.LoadOAuthConfigFromEnv()
	oauthProviders := auth.NewProviders(oauthCfg)
	oauthStates := auth.NewOAuthStateStore()
	oauthLimiter := auth.NewCredentialStuffingLimiter()

	newTenant := func(ownerEmail string) (string, string, error) {
		localPart := ownerEmail
		if at := strings.Index(ownerEmail, "@"); at != -1 {
			localPart = ownerEmail[:at]
		}
		tenantID := slugifyCompanyName(localPart)
		var t *tenant.Tenant
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			t, err = tenant.ProvisionTenant(tenantStore, roleStore, tenantID, ownerEmail)
			if err == nil {
				break
			}
			if err.Error() != "tenant_already_exists" {
				break
			}
			tenantID = tenantID + "-" + randomTenantSuffix()
		}
		if err != nil {
			return "", "", err
		}
		return t.ID, "owner", nil
	}

	app.Get(
		"/api/auth/oauth/:provider/start",
		OAuthRateLimit(oauthLimiter),
		OAuthStartHandler(oauthProviders, oauthStates),
	)

	app.Get(
		"/api/auth/oauth/:provider/callback",
		OAuthRateLimit(oauthLimiter),
		OAuthCallbackHandler(oauthProviders, oauthStates, userStore, userStore, refreshStore, []byte(signingKey), newTenant, auditStore),
	)

	// ============================================================
	// POST /api/auth/refresh
	// ============================================================

	app.Post(
		"/api/auth/refresh",
		func(c *fiber.Ctx) error {
			var req struct {
				RefreshToken string `json:"refresh_token"`
			}

			// Body opsional: browser SPA mengirim lewat cookie httpOnly.
			_ = c.BodyParser(&req)
			if req.RefreshToken == "" {
				req.RefreshToken = c.Cookies(refreshCookieName)
			}
			if req.RefreshToken == "" {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "refresh_token_required",
				})
			}

			newRefreshToken, userID, err :=
				auth.RotateRefreshTokenForAnyUser(
					req.RefreshToken,
					refreshStore,
				)
			if err != nil {
				// Token yang sudah dipakai ditukarkan lagi = rantai refresh
				// bocor. Cabut seluruh sesi pemiliknya; menolak satu token
				// saja membiarkan token hasil rotasi penyerang tetap hidup.
				auth.RevokeAllSessionsIfTokenReused(req.RefreshToken, refreshStore, err)
				clearRefreshCookie(c)
				return c.Status(
					fiber.StatusUnauthorized,
				).JSON(fiber.Map{
					"error": err.Error(),
				})
			}

			user, err := userStore.GetByID(userID)
			if err != nil {
				return c.Status(
					fiber.StatusUnauthorized,
				).JSON(fiber.Map{
					"error": "user_not_found",
				})
			}

			if user.Status != auth.UserStatusActive {
				return c.Status(
					fiber.StatusForbidden,
				).JSON(fiber.Map{
					"error": "user_suspended",
				})
			}

			accessToken, err :=
				auth.GenerateAccessToken(
					user.ID,
					user.TenantID,
					user.RoleID,
					[]byte(signingKey),
				)
			if err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "token_generation_failed",
				})
			}

			setAccessCookie(c, accessToken, auth.AccessTokenTTL)
			clearLegacyRefreshCookie(c)
			setRefreshCookie(c, newRefreshToken, auth.RefreshTokenTTL)

			return c.JSON(fiber.Map{
				"access_token":  accessToken,
				"refresh_token": newRefreshToken,
				"token_type":    "Bearer",
				"expires_in":    int(auth.AccessTokenTTL.Seconds()),
			})
		},
	)

	// GET /api/auth/session — dipakai halaman publik (pricing.html, nav.js) untuk cek
	// apakah user sedang login + tenant_id-nya, tanpa expose token ke JS.
	// Reuse requireAuth + portalTenantID yang sudah dipakai /overview dkk.
	//
	// user_id/email/role_id — Phase 1, ROADMAP §6:
	//   - user_id, role_id: dari JWT claims (D1). Access token hidup 15 menit,
	//     jadi tanpa query tambahan dan lag maksimal terbatas.
	//   - email: dari userStore.GetByID(claims.UserID). GetByID menyembunyikan
	//     user yang soft-deleted (ErrUserNotFound), jadi user yang dihapus
	//     di tengah sesi otomatis dapat 401 walau JWT belum kedaluwarsa.
	app.Get("/api/auth/session", requireAuth, func(c *fiber.Ctx) error {
		tenantID, err := portalTenantID(c)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "not_authenticated"})
		}

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok || claims == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "not_authenticated"})
		}

		user, err := userStore.GetByID(claims.UserID)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "not_authenticated"})
		}

		return c.JSON(fiber.Map{
			"tenant_id": tenantID,
			"user_id":   claims.UserID,
			"email":     user.Email,
			"role_id":   claims.RoleID,
		})
	})

	// ============================================================
	// POST /api/auth/logout
	// ============================================================

	app.Post(
		"/api/auth/logout",
		requireAuth,
		func(c *fiber.Ctx) error {
			claims, ok :=
				middleware.ClaimsFromFiberCtx(c)

			if !ok || claims == nil {
				return c.Status(
					fiber.StatusUnauthorized,
				).JSON(fiber.Map{
					"error": "unauthenticated_context",
				})
			}

			if claims.ID == "" {
				return c.Status(
					fiber.StatusUnauthorized,
				).JSON(fiber.Map{
					"error": "invalid_token",
				})
			}

			if claims.ExpiresAt == nil {
				return c.Status(
					fiber.StatusUnauthorized,
				).JSON(fiber.Map{
					"error": "invalid_token",
				})
			}

			clearAccessCookie(c)

			clearRefreshCookie(c)
			clearLegacyRefreshCookie(c)

			_ = refreshStore.RevokeAllForUser(claims.UserID)

			if err :=
				revocationStore.Revoke(
					claims.ID,
					claims.ExpiresAt.Time,
				); err != nil {
				return c.Status(
					fiber.StatusInternalServerError,
				).JSON(fiber.Map{
					"error": "logout_failed",
				})
			}

			return c.JSON(fiber.Map{
				"status": "logged_out",
			})
		},
	)

	return app, billingService, nil
}
