// cmd/notify-worker/main.go
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"

	"trailseal/auth"
	"trailseal/notify"
	"trailseal/tenant"
)

// notify-worker — proses TERPISAH, dijalankan berkala (cron/systemd
// timer), pola sama seperti cmd/retention-job: satu eksekusi = satu
// drain, BUKAN service long-running.
//
// SENDER: ResendSender kalau RESEND_API_KEY di-set, fallback ke
// LoggingSender kalau tidak (aman untuk lingkungan yang belum
// pasang provider).
func main() {
	dbPath := os.Getenv("SAAS_API_DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join("data", "saas-api.db")
	}

	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		log.Fatalf("notify-worker: failed to open db at %s: %v", dbPath, err)
	}
	defer db.Close()

	queue, err := notify.NewBoltEmailQueue(db)
	if err != nil {
		log.Fatalf("notify-worker: failed to init email queue: %v", err)
	}

	userStore, err := auth.NewBoltUserStore(db)
	if err != nil {
		log.Fatalf("notify-worker: failed to init user store: %v", err)
	}

	tenantStore, err := tenant.NewBoltTenantStore(db)
	if err != nil {
		log.Fatalf("notify-worker: failed to init tenant store: %v", err)
	}

	var sender notify.EmailSender = notify.LoggingSender{}
	if apiKey := os.Getenv("RESEND_API_KEY"); apiKey != "" {
		from := os.Getenv("NOTIFY_FROM_EMAIL")
		if from == "" {
			from = "TrailSeal <notifications@trailseal.dev>" // ganti sesuai domain terverifikasi kamu di Resend
		}
		sender = notify.NewResendSender(apiKey, from, resolveNotificationEmail(tenantStore, userStore))
	} else {
		log.Println("notify-worker: RESEND_API_KEY not set, falling back to LoggingSender")
	}

	sent, failed, err := notify.DrainOnce(queue, sender, 200)
	if err != nil {
		log.Fatalf("notify-worker: drain failed: %v", err)
	}

	log.Printf("notify-worker: drain complete — sent=%d failed=%d", sent, failed)
}

// resolveNotificationEmail — prioritaskan Tenant.NotificationEmail kalau
// diisi (lihat tenant/tenant.go); fallback ke email user dengan
// RoleID == "owner" kalau kosong.
func resolveNotificationEmail(tenantStore *tenant.BoltTenantStore, userStore *auth.BoltUserStore) func(tenantID string) (string, error) {
	return func(tenantID string) (string, error) {
		if t, err := tenantStore.Get(tenantID); err == nil && t.NotificationEmail != "" {
			return t.NotificationEmail, nil
		}

		users, err := userStore.ListAllForTenant(tenantID)
		if err != nil {
			return "", err
		}
		for _, u := range users {
			if u.RoleID == "owner" {
				return u.Email, nil
			}
		}
		return "", fmt.Errorf("no notification email or owner found for tenant %s", tenantID)
	}
}