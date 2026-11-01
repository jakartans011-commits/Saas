package billing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"	
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stripe/stripe-go/v78"
	portalsession "github.com/stripe/stripe-go/v78/billingportal/session"
	"github.com/stripe/stripe-go/v78/checkout/session"
	"github.com/stripe/stripe-go/v78/webhook"

	"trailseal/audit"
	"trailseal/tenant"
	"trailseal/rbac"
)

// ProjectPlanApplier is the subset of project.PostgresStore that the
// billing service needs: pushing a resolved plan's entitlements
// (event limit, retention) down to every active project of a tenant.
//
// Defined here (rather than depending on the concrete
// *project.PostgresStore) so tests can substitute a fake instead of
// requiring a real Postgres connection — same pattern as
// tenant.TenantStore. *project.PostgresStore satisfies this
// interface as-is; no change needed on the project package side.
type ProjectPlanApplier interface {
	ApplyPlanToTenant(ctx context.Context, tenantID string, monthlyEventLimit int64, retentionDays int) error
}

// WebhookEventClaimer is the subset of *WebhookEventStore the
// service needs for idempotency. Defined as an interface for the
// same reason as ProjectPlanApplier: *WebhookEventStore (backed by
// Postgres) satisfies it as-is, and tests can substitute a fake.
type WebhookEventClaimer interface {
	Claim(ctx context.Context, eventID string, eventType string) (bool, error)
	// Release melepas klaim ketika pemrosesan gagal, supaya retry Stripe
	// masih bisa memproses event yang sama. Lihat webhook_event_release.go.
	Release(ctx context.Context, eventID string) error
}
// AuditLogger is the subset of audit.Store the billing service needs
// to record billing lifecycle events (checkout, subscription change,
// cancellation, payment failure). Defined as an interface for the
// same reason as ProjectPlanApplier/WebhookEventClaimer above — tests
// can substitute a fake, and *audit.BoltAuditStore satisfies this
// as-is with no change needed on the audit package side.
type AuditLogger interface {
	Save(event *audit.AuditEvent) error
}

// RoleProvisioner is the subset of rbac.RoleAdminStore the billing
// service needs to provision Admin/Member roles on plan upgrade.
// Same decoupling pattern as ProjectPlanApplier/WebhookEventClaimer.
type RoleProvisioner interface {
	PutRole(role *rbac.Role) error
}

// SetRoleStore configures where Admin/Member roles are provisioned when
// a tenant upgrades to Pro/Business. Same pattern as SetAuditLogger —
// existing NewService(...) call site does not need to change. Safe to
// leave unset: provisionRolesForPlan nil-checks before writing, so
// upgrades still succeed even if this isn't wired up (degrades to the
// pre-existing behavior, not a new failure mode).
func (s *Service) SetRoleStore(store RoleProvisioner) {
	s.roleStore = store
}

// RoleDeleter adalah interface OPSIONAL pada RoleProvisioner (type assertion),
// supaya interface RoleProvisioner dan semua mock tidak berubah.
type RoleDeleter interface {
	DeleteRole(tenantID, roleID string) error
}

// revokePaidRoles mencabut role admin/member saat tenant turun ke Free.
// Gagal hanya dicatat; tidak boleh membatalkan downgrade (webhook Stripe di-retry).
func (s *Service) revokePaidRoles(tenantID string) {
	if s.roleStore == nil {
		return
	}
	d, ok := s.roleStore.(RoleDeleter)
	if !ok {
		return
	}
	for _, roleID := range []string{"admin", "member"} {
		if err := d.DeleteRole(tenantID, roleID); err != nil {
			s.logBillingEvent(tenantID, "billing.role_revoke_failed", "role:"+roleID, audit.OutcomeFailure, map[string]string{
				"error": err.Error(),
			})
		}
	}
}

// provisionRolesForPlan ensures "admin" and "member" roles exist for
// tenantID when it's on a plan tier that includes them (Pro/Business,
// per pricing-decisions.md §1). PutRole overwrites by key
// (tenantID+roleID, see rbac/role.go), so calling this repeatedly on
// every subscription.updated webhook is idempotent — safe to call
// unconditionally rather than tracking "was this already provisioned".
//
// NOTE: capability grants below are a reasonable default (Admin = full
// tenant access like Owner; Member = read-only across core resources),
// NOT taken from an existing spec — pricing-decisions.md defines the
// tier names but not per-role capability lists. Adjust the two
// CapabilityGrants slices below if your intended permissions differ.
func (s *Service) provisionRolesForPlan(tenantID string, tier PlanTier) {
	if s.roleStore == nil {
		return
	}
	if tier != PlanPro && tier != PlanBusiness {
		return
	}

	_ = s.roleStore.PutRole(&rbac.Role{
		ID:       "admin",
		TenantID: tenantID,
		Name:     "admin",
		// Operational full access — deliberately EXCLUDES billing.write
		// and compliance.* (delete/restore/purge). Those stay Owner-only:
		// an Admin should never be able to change plan, cancel
		// subscription, swap payment method, or delete/restore the
		// tenant. This is the standard SaaS pattern (Stripe, Vercel,
		// GitHub, Slack all separate "operational admin" from "billing
		// owner"). See ROADMAP.md Backlog for the review that led here.
		CapabilityGrants: []string{
			"tenant." + tenantID + ".users.create",
			"tenant." + tenantID + ".users.read",
			"tenant." + tenantID + ".usage.read",
			"tenant." + tenantID + ".audit.read",
			"tenant." + tenantID + ".audit.export",
			"tenant." + tenantID + ".agent.read",
			"tenant." + tenantID + ".agent.write",
			"tenant." + tenantID + ".tool.read",
			"tenant." + tenantID + ".tool.write",
			"tenant." + tenantID + ".tool.execute",
			"tenant." + tenantID + ".mcp.query",
			// BARU — Prompts feature (MCP). Admin boleh READ (built-in +
			// custom prompt tenant), TIDAK boleh WRITE (create custom
			// prompt tetap Owner-only, ditegakkan langsung di
			// mcp/server.go lewat cek roleID=="owner", bukan lewat
			// CapabilityGrants — jadi TIDAK menambah "prompts.write" di
			// sini bukan lupa, itu keputusan produk yang disengaja).
			"tenant." + tenantID + ".prompts.read",
			"tenant." + tenantID + ".projects.read",
			"tenant." + tenantID + ".projects.write",
			"tenant." + tenantID + ".apikeys.read",
			"tenant." + tenantID + ".apikeys.write",
		},
	})

	_ = s.roleStore.PutRole(&rbac.Role{
		ID:       "member",
		TenantID: tenantID,
		Name:     "member",
		// Read-only across resources, plus tool.execute and mcp.query so
		// a Member can actually use the product day-to-day (run tools,
		// query MCP) without being able to create/modify anything,
		// manage the team, manage API keys, or touch billing/compliance.
		CapabilityGrants: []string{
			"tenant." + tenantID + ".usage.read",
			"tenant." + tenantID + ".audit.read",
			"tenant." + tenantID + ".agent.read",
			"tenant." + tenantID + ".tool.read",
			"tenant." + tenantID + ".tool.execute",
			"tenant." + tenantID + ".mcp.query",
			// BARU — sama alasan seperti di Admin: read-only, write tetap
			// Owner-only lewat mcp/server.go, bukan lewat grant ini.
			"tenant." + tenantID + ".prompts.read",
			"tenant." + tenantID + ".projects.read",
		},
	})
}

func newAuditEventID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "audit-billing-" + hex.EncodeToString(b)
}

// lockTenant returns the mutex for a given tenant ID, creating it on
// first use. Callers must call the returned unlock func when done.
func (s *Service) lockTenant(tenantID string) func() {
muIface, _ := s.tenantLocks.LoadOrStore(tenantID, &sync.Mutex{})
mu := muIface.(*sync.Mutex)
mu.Lock()
return mu.Unlock
}

type Service struct {
	tenantStore       tenant.TenantStore
	projectStore      ProjectPlanApplier
	webhookEventStore WebhookEventClaimer
	plans             *Registry
	webhookSecret     string
	successURL        string
	cancelURL         string
	portalReturnURL   string
	auditLogger       AuditLogger // opsional, nil-safe
	roleStore         RoleProvisioner // opsional, nil-safe — sama pola seperti auditLogger

	// subscriptionFetcher — opsional, nil-safe. Dipakai rekonsiliasi harian
	// (stripe_reconcile.go). nil = rekonsiliasi mati, dan ReconcileStripe
	// mengembalikan error alih-alih laporan bersih palsu.
	subscriptionFetcher SubscriptionFetcher

// tenantLocks — serializes webhook read-modify-write cycles per
// tenant. Stripe can deliver multiple events for the same tenant
// within the same second (e.g. checkout.session.completed +
// customer.subscription.updated), each handled on its own Fiber
// goroutine. Without this, two handlers can both Get() the tenant
// before either Put()s, and whichever writes last silently
// clobbers fields the other one set (observed: StripeCustomerID
// reverting to "" after a subscription.updated event overwrote a
// checkout.session.completed write). sync.Map avoids needing to
// pre-register every tenant ID up front.
tenantLocks sync.Map // map[string]*sync.Mutex
 }

func NewService(
	tenantStore tenant.TenantStore,
	projectStore ProjectPlanApplier,
	webhookEventStore WebhookEventClaimer,
	plans *Registry,
	webhookSecret, successURL, cancelURL string,
) *Service {
	return &Service{
		tenantStore:       tenantStore,
		projectStore:      projectStore,
		webhookEventStore: webhookEventStore,
		plans:             plans,
		webhookSecret:     webhookSecret,
		successURL:        successURL,
		cancelURL:         cancelURL,
	}
}

// SetPortalReturnURL configures the URL Stripe sends the user back to
// after they close the Billing Portal. Kept as a setter (rather than a
// NewService param) so the existing constructor call in app.go does not
// need to change — only one new line is needed to wire this up.
func (s *Service) SetPortalReturnURL(url string) {
	s.portalReturnURL = url
}

// SetAuditLogger configures where billing lifecycle events are
// recorded. Kept as a setter (same pattern as SetPortalReturnURL) so
// the existing NewService(...) call site in app.go does not need to
// change signature — only one new line is needed to wire this up.
// Safe to leave unset: logBillingEvent nil-checks before writing.
func (s *Service) SetAuditLogger(a AuditLogger) {
	s.auditLogger = a
}

func (s *Service) logBillingEvent(tenantID, action, resource string, outcome audit.Outcome, metadata map[string]string) {
	if s.auditLogger == nil {
		return
	}
	_ = s.auditLogger.Save(&audit.AuditEvent{
		ID:        newAuditEventID(),
		TenantID:  tenantID,
		Action:    action,
		Resource:  resource,
		Outcome:   outcome,
		Timestamp: time.Now(),
		Metadata:  metadata,
	})
}

// CreateCheckoutSession builds a Stripe Checkout Session for the given
// tenant + plan and returns its hosted URL.
//
// Extracted out of CreateCheckoutHandler (which is now a thin fiber.Handler
// wrapper around this) so callers outside the HTTP layer can reuse the
// exact same Stripe wiring — specifically the new self-serve signup flow
// (POST /api/auth/register), which needs to create a Checkout Session for
// a tenant it just created in the same request, not from an existing
// :id path param.
// CreateCheckoutSession: checkout bulanan (pemanggil lama tidak berubah).
func (s *Service) CreateCheckoutSession(tenantID string, planTier PlanTier) (string, error) {
	return s.CreateCheckoutSessionInterval(tenantID, planTier, IntervalMonthly)
}

// CreateCheckoutSessionInterval: interval monthly atau annual. Tahunan yang
// belum dikonfigurasi ditolak (errIntervalNotAvailable), tidak jatuh ke bulanan.
func (s *Service) CreateCheckoutSessionInterval(tenantID string, planTier PlanTier, interval BillingInterval) (string, error) {
	priceID, ok := s.plans.PriceIDFor(planTier, interval)
	if !ok {
		if interval == IntervalAnnual {
			if _, monthlyOK := s.plans.PriceIDFor(planTier, IntervalMonthly); monthlyOK {
				return "", errIntervalNotAvailable
			}
		}
		return "", errInvalidPlan
	}

	t, err := s.tenantStore.Get(tenantID)
	if err != nil {
		return "", errTenantNotFoundForCheckout
	}

	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		ClientReferenceID: stripe.String(t.ID),
		SuccessURL:        stripe.String(s.successURL),
		CancelURL:         stripe.String(s.cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: map[string]string{"tenant_id": t.ID, "interval": string(interval)},
		},
	}
	if t.StripeCustomerID != "" {
		params.Customer = stripe.String(t.StripeCustomerID)
	}

	sess, err := session.New(params)
	if err != nil {
		return "", errCheckoutSessionFailed
	}

	return sess.URL, nil
}

// CreatePortalSession builds a Stripe Billing Portal Session for the
// tenant's existing Stripe customer, returning the hosted URL. This is
// how Free/Pro/Business tenants manage payment methods, view invoices,
// and cancel/downgrade — instead of TrailSeal building any of that UI
// itself (Section 1: do not build custom billing-detail screens).
//
// Requires the tenant to already have a StripeCustomerID — i.e. they
// have completed Checkout at least once. A tenant still on Free with no
// Stripe customer yet has nothing to manage, so this returns
// errNoStripeCustomer rather than silently creating a customer record.
func (s *Service) CreatePortalSession(tenantID string) (string, error) {
	t, err := s.tenantStore.Get(tenantID)
	if err != nil {
		return "", errTenantNotFoundForCheckout
	}
	if t.StripeCustomerID == "" {
		return "", errNoStripeCustomer
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(t.StripeCustomerID),
		ReturnURL: stripe.String(s.portalReturnURL),
	}

	sess, err := portalsession.New(params)
	if err != nil {
		return "", errPortalSessionFailed
	}

	return sess.URL, nil
}

var (
	errInvalidPlan               = fmt.Errorf("invalid_plan")
	errTenantNotFoundForCheckout = fmt.Errorf("tenant_not_found")
	errCheckoutSessionFailed     = fmt.Errorf("checkout_session_failed")
	errNoStripeCustomer          = fmt.Errorf("no_stripe_customer")
	errPortalSessionFailed       = fmt.Errorf("portal_session_failed")
	errIntervalNotAvailable      = fmt.Errorf("interval_not_available")
)

func (s *Service) CreateCheckoutHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		var req struct {
			Plan     string `json:"plan"`
            Interval string `json:"interval"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_body"})
		}

		interval, okInterval := ParseBillingInterval(req.Interval)
		if !okInterval {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":           "invalid_interval",
				"valid_intervals": []string{"monthly", "annual"},
			})
		}

		checkoutURL, err := s.CreateCheckoutSessionInterval(tenantID, PlanTier(req.Plan), interval)
		if err != nil {
			switch err {
				case errIntervalNotAvailable:
                   return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "interval_not_available"})
			case errInvalidPlan:
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error":       "invalid_plan",
					"valid_plans": []string{"pro", "business"},
				})
			case errTenantNotFoundForCheckout:
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "tenant_not_found"})
			default:
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "checkout_session_failed"})
			}
		}

		return c.JSON(fiber.Map{"checkout_url": checkoutURL})
	}
}

// CreatePortalHandler exposes CreatePortalSession over HTTP.
// POST /api/tenants/:id/billing/portal — same auth/capability guard as
// the checkout route (see app.go patch note below).
func (s *Service) CreatePortalHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		portalURL, err := s.CreatePortalSession(tenantID)
		if err != nil {
			switch err {
			case errNoStripeCustomer:
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error":   "no_stripe_customer",
					"message": "Upgrade to a paid plan first — billing management is available once you have an active subscription.",
				})
			case errTenantNotFoundForCheckout:
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "tenant_not_found"})
			default:
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "portal_session_failed"})
			}
		}

		return c.JSON(fiber.Map{"portal_url": portalURL})
	}
}

// ============================================================
// WEBHOOK PAYLOAD STRUCTS — SENGAJA TIDAK pakai stripe.CheckoutSession
// / stripe.Subscription penuh dari SDK.
//
// Alasan: field seperti "customer" dan "subscription" di payload
// webhook ASLI dari Stripe berisi ID STRING ("cus_xxx"), BUKAN objek
// penuh, kecuali di-expand eksplisit. Struct SDK (stripe.CheckoutSession)
// mendefinisikan field itu sebagai *stripe.Customer (objek), sehingga
// json.Unmarshal(rawPayload, &sess) bisa gagal atau menghasilkan field
// kosong karena mismatch tipe. Struct minimal di bawah ini HANYA
// mengambil apa yang benar-benar kita butuhkan, semuanya string/primitif,
// jadi tidak rentan masalah ini.
// ============================================================

type checkoutSessionPayload struct {
	ClientReferenceID string `json:"client_reference_id"`
	Customer          string `json:"customer"`
	Subscription      string `json:"subscription"`
}

type subscriptionPayload struct {
	ID       string            `json:"id"`
	Status   string            `json:"status"`
	Metadata map[string]string `json:"metadata"`
	Items    struct {
		Data []struct {
			Price struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

type invoicePayload struct {
	ID           string `json:"id"`
	Customer     string `json:"customer"`
	Subscription string `json:"subscription"`
	AttemptCount int    `json:"attempt_count"`

	LastFinalizationError *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"last_finalization_error"`
}

const invoiceFailureGracePeriod = 4 * 24 * time.Hour // titik tengah kebijakan grace period 3-5 hari

func (s *Service) WebhookHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {

		// Secret kosong = HMAC dengan kunci kosong: siapa pun bisa memalsukan
		// webhook "valid". Tolak, jangan diproses.
		if s.webhookSecret == "" {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "webhook_not_configured"})
		}

		// IgnoreAPIVersionMismatch: true — akun Stripe kamu berjalan di
		// API version lebih baru (mis. "2026-07-29.dahlia") daripada yang
		// di-hardcode di stripe-go v78 ("2024-04-10"). Signature tetap
		// diverifikasi penuh (keamanan tidak berkurang) — opsi ini HANYA
		// mematikan pengecekan STRING versi API, bukan verifikasi HMAC.
		// Field yang kita baca dari payload (checkoutSessionPayload,
		// subscriptionPayload) semuanya field dasar yang stabil lintas
		// versi API, jadi aman dari risiko "objects may be incorrectly
		// deserialized" yang disebut di pesan error SDK.
		event, err := webhook.ConstructEventWithOptions(
			c.Body(),
			c.Get("Stripe-Signature"),
			s.webhookSecret,
			webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true},
		)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_signature"})
		}

		if s.webhookEventStore == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(
				fiber.Map{"error": "webhook_event_store_unavailable"},
			)
		}

		claimed, err := s.webhookEventStore.Claim(
			context.Background(),
			event.ID,
			string(event.Type),
		)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(
				fiber.Map{"error": "webhook_event_claim_failed"},
			)
		}

		if !claimed {
			// Stripe retry / duplicate delivery.
			// Event sudah pernah diproses atau sedang diproses oleh request lain.
			return c.JSON(fiber.Map{
				"received":  true,
				"duplicate": true,
			})
		}
		ctx := context.Background()
		var handleErr error

		switch event.Type {
		case "checkout.session.completed":
			var sess checkoutSessionPayload
			if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_payload", "detail": err.Error(),
				})
			}
			handleErr = s.handleCheckoutCompleted(ctx, sess)

		case "customer.subscription.updated", "customer.subscription.created":
			var sub subscriptionPayload
			if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_payload", "detail": err.Error(),
				})
			}
			handleErr = s.handleSubscriptionUpdated(ctx, sub)

		case "customer.subscription.deleted":
			var sub subscriptionPayload
			if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_payload", "detail": err.Error(),
				})
			}
			handleErr = s.handleSubscriptionDeleted(ctx, sub)

		case "invoice.payment_failed":
			var inv invoicePayload
			if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid_payload", "detail": err.Error(),
				})
			}
			handleErr = s.handleInvoicePaymentFailed(ctx, inv)

		default:
			// Event lain sengaja diabaikan.
		}

			if handleErr != nil {
			// Kita membalas 5xx supaya Stripe retry. Klaim harus dilepas
			// lebih dulu, kalau tidak retry berikutnya dianggap duplikat
			// dan dibalas 200 — pelanggan sudah dibayar tapi plannya tidak
			// pernah naik, tanpa satu pun tanda di log.
			s.releaseWebhookClaim(event.ID, string(event.Type))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": handleErr.Error()})
		}
		return c.JSON(fiber.Map{"received": true})
	}
}

func (s *Service) handleCheckoutCompleted(ctx context.Context, sess checkoutSessionPayload) error {
	tenantID := sess.ClientReferenceID
	if tenantID == "" {
		return fmt.Errorf("checkout session missing client_reference_id")
	}
	unlock := s.lockTenant(tenantID)
    defer unlock()

	t, err := s.tenantStore.Get(tenantID)
	if err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	if sess.Customer != "" {
		t.StripeCustomerID = sess.Customer
	}
	if sess.Subscription != "" {
		t.StripeSubscriptionID = sess.Subscription
	}

	if err := s.tenantStore.Put(t); err != nil {
		return err
	}

	s.logBillingEvent(tenantID, "billing.checkout_completed", "subscription:"+sess.Subscription, audit.OutcomeSuccess, nil)
	return nil
}

func (s *Service) handleSubscriptionUpdated(ctx context.Context, sub subscriptionPayload) error {
	tenantID := sub.Metadata["tenant_id"]
	if tenantID == "" {
		return fmt.Errorf("subscription missing tenant_id metadata")
	}
	unlock := s.lockTenant(tenantID)
	defer unlock()

	t, err := s.tenantStore.Get(tenantID)
	if err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	if len(sub.Items.Data) == 0 {
		return fmt.Errorf("subscription has no items")
	}

	priceID := sub.Items.Data[0].Price.ID
	if priceID == "" {
		return fmt.Errorf("subscription item has empty price ID")
	}

	plan, ok := s.plans.ResolveByPriceID(priceID)
	if !ok {
		s.logBillingEvent(tenantID, "billing.subscription_updated", "price:"+priceID, audit.OutcomeFailure, map[string]string{
			"reason": "unknown_price_id",
		})
		return fmt.Errorf("unknown Stripe price ID: %s", priceID)
	}

	// Pembayaran pertama belum selesai (atau kedaluwarsa): JANGAN memberi
	// entitlement plan berbayar. Plan tenant tidak diubah; plan baru berlaku
	// ketika Stripe mengirim status "active" setelah pembayaran berhasil.
	if sub.Status == "incomplete" || sub.Status == "incomplete_expired" {
		s.logBillingEvent(tenantID, "billing.subscription_updated", "subscription:"+sub.ID, audit.OutcomeFailure, map[string]string{
			"reason": "subscription_not_paid",
			"status": sub.Status,
		})
		return nil
	}

	t.PlanTier = string(plan.Tier)
	s.provisionRolesForPlan(tenantID, plan.Tier)
	t.SubscriptionStatus = sub.Status
	t.StripeSubscriptionID = sub.ID

	if sub.Status == "active" {
		t.PaymentFailureCount = 0
		t.LastPaymentFailureAt = nil
		t.PaymentFailureReason = ""
		t.PastDueSince = nil
		t.GracePeriodDeadline = nil
		t.MonthlyEventLimit = plan.MonthlyEventLimit
	}

	if err := s.tenantStore.Put(t); err != nil {
		return fmt.Errorf("failed to update tenant plan: %w", err)
	}

	if err := s.projectStore.ApplyPlanToTenant(ctx, tenantID, plan.MonthlyEventLimit, plan.RetentionDays); err != nil {
		return fmt.Errorf("failed to apply plan to projects: %w", err)
	}

	s.logBillingEvent(tenantID, "billing.subscription_updated", "subscription:"+sub.ID, audit.OutcomeSuccess, map[string]string{
		"plan_tier": string(plan.Tier),
		"status":    sub.Status,
	})
	return nil
}

func (s *Service) handleSubscriptionDeleted(ctx context.Context, sub subscriptionPayload) error {
	tenantID := sub.Metadata["tenant_id"]
	if tenantID == "" {
		return fmt.Errorf("subscription missing tenant_id metadata")
	}
	unlock := s.lockTenant(tenantID)
    defer unlock()

	t, err := s.tenantStore.Get(tenantID)
	if err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	free := s.plans.Free()
	t.PlanTier = string(free.Tier)
	t.SubscriptionStatus = "canceled"
	t.MonthlyEventLimit = free.MonthlyEventLimit
	if err := s.tenantStore.Put(t); err != nil {
		return fmt.Errorf("failed to downgrade tenant to free: %w", err)
	}

	if err := s.projectStore.ApplyPlanToTenant(ctx, tenantID, free.MonthlyEventLimit, free.RetentionDays); err != nil {
		return fmt.Errorf("failed to apply free plan to projects: %w", err)
	}

	s.revokePaidRoles(tenantID)
	
	s.logBillingEvent(tenantID, "billing.subscription_canceled", "subscription:"+sub.ID, audit.OutcomeSuccess, nil)
	return nil
}

func (s *Service) handleInvoicePaymentFailed(ctx context.Context, inv invoicePayload) error {
	if inv.Customer == "" {
		return fmt.Errorf("invoice missing customer id")
	}

	lookup, ok := s.tenantStore.(tenant.TenantByStripeLookup)
	if !ok {
		return fmt.Errorf("tenant_store_does_not_support_stripe_lookup")
	}

	t, err := lookup.FindByStripeCustomerID(inv.Customer)
	if err != nil {
		return fmt.Errorf("tenant not found for stripe customer %s: %w", inv.Customer, err)
	}

	unlock := s.lockTenant(t.ID)
    defer unlock()
	
	reason := "unknown"
	if inv.LastFinalizationError != nil && inv.LastFinalizationError.Code != "" {
		reason = inv.LastFinalizationError.Code
	}

	now := time.Now()
	t.PaymentFailureCount++
	t.LastPaymentFailureAt = &now
	t.PaymentFailureReason = reason
	t.SubscriptionStatus = "past_due"

	if t.PastDueSince == nil {
		deadline := now.Add(invoiceFailureGracePeriod)
		t.PastDueSince = &now
		t.GracePeriodDeadline = &deadline
	}

	if err := s.tenantStore.Put(t); err != nil {
		return err
	}

	s.logBillingEvent(t.ID, "billing.payment_failed", "invoice:"+inv.ID, audit.OutcomeFailure, map[string]string{
		"reason": reason,
	})
	return nil
}