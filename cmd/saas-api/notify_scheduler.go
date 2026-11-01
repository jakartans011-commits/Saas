package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"

	"trailseal/auth"
	"trailseal/notify"
	"trailseal/tenant"
)

// ============================================================
// P0-1: pengiriman antrean email notifikasi (kuota 80/100%, chain
// rusak) dari DALAM proses saas-api.
//
// Kenapa bukan cmd/notify-worker: bbolt mengunci file secara
// eksklusif, jadi proses terpisah tidak bisa membuka
// data/saas-api.db selama saas-api hidup (lihat
// notify/bolt_exclusive_lock_test.go). Satu proses = satu pemegang.
//
// Scheduler HANYA jalan bila RESEND_API_KEY diisi. Tanpa key, antrean
// dibiarkan utuh: LoggingSender akan menandai job "terkirim" lalu
// membuangnya, dan email yang seharusnya terkirim hilang.
// ============================================================

const (
	notifyDrainInterval = time.Minute
	notifyDrainBatch    = 200
)

func notifyRecipientResolver(tenantStore *tenant.BoltTenantStore, userStore *auth.BoltUserStore) func(tenantID string) (string, error) {
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

// drainNotifyOnce menguras satu batch antrean. Panic tidak boleh
// mematikan proses saas-api.
func drainNotifyOnce(queue *notify.BoltEmailQueue, sender notify.EmailSender) (sent int, failed int) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("notify scheduler: recovered from panic: %v", rec)
		}
	}()

	sent, failed, err := notify.DrainOnce(queue, sender, notifyDrainBatch)
	if err != nil {
		log.Printf("notify scheduler: drain failed: %v", err)
	}
	if sent > 0 || failed > 0 {
		log.Printf("notify scheduler: sent=%d failed=%d", sent, failed)
	}
	return sent, failed
}

// startNotifyScheduler memulai goroutine pengirim. Error dikembalikan
// bila RESEND_API_KEY kosong (pemanggil cukup me-log, bukan fatal).
func startNotifyScheduler(db *bolt.DB) (func(), error) {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("RESEND_API_KEY is empty; queued notification emails will NOT be sent")
	}

	queue, err := notify.NewBoltEmailQueue(db)
	if err != nil {
		return nil, fmt.Errorf("email queue: %w", err)
	}
	userStore, err := auth.NewBoltUserStore(db)
	if err != nil {
		return nil, fmt.Errorf("user store: %w", err)
	}
	tenantStore, err := tenant.NewBoltTenantStore(db)
	if err != nil {
		return nil, fmt.Errorf("tenant store: %w", err)
	}

	from := os.Getenv("NOTIFY_FROM_EMAIL")
	if from == "" {
		from = "TrailSeal <notifications@trailseal.dev>" // domain HARUS terverifikasi di Resend
	}
	sender := notify.NewResendSender(apiKey, from, notifyRecipientResolver(tenantStore, userStore))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(notifyDrainInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				drainNotifyOnce(queue, sender)
			}
		}
	}()

	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			log.Printf("notify scheduler: stop timed out")
		}
	}
	return stop, nil
}
