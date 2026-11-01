package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	// PlatformTenantID adalah "tenant" khusus platform untuk event yang
	// tidak bisa dikaitkan ke tenant mana pun (mis. login dengan email
	// tidak terdaftar). Tidak ada FK di audit_events.tenant_id, jadi
	// tidak perlu migrasi.
	PlatformTenantID        = "_platform"
	PlatformAnonymousUserID = "anonymous"

	platformPerIPWindow   = 5 * time.Minute // maks 1 entri per IP per jendela
	platformGlobalWindow  = time.Minute
	platformGlobalMax     = 60 // batas total entri _platform per menit
	platformMaxTrackedIPs = 10000
	platformMaxUALen      = 200
)

type platformThrottleState struct {
	mu          sync.Mutex
	perIP       map[string]time.Time
	globalStart time.Time
	globalCount int
}

var platformThrottle = &platformThrottleState{perIP: make(map[string]time.Time)}

func (t *platformThrottleState) allow(ip string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if last, ok := t.perIP[ip]; ok && now.Sub(last) < platformPerIPWindow {
		return false
	}

	if now.Sub(t.globalStart) >= platformGlobalWindow {
		t.globalStart = now
		t.globalCount = 0
	}
	if t.globalCount >= platformGlobalMax {
		return false
	}

	if len(t.perIP) >= platformMaxTrackedIPs {
		for k, v := range t.perIP {
			if now.Sub(v) >= platformPerIPWindow {
				delete(t.perIP, k)
			}
		}
		if len(t.perIP) >= platformMaxTrackedIPs {
			return false
		}
	}

	t.perIP[ip] = now
	t.globalCount++
	return true
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func emailHash(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])[:16]
}

// RecordPlatformLoginAttempt mencatat percobaan login yang tenant-nya
// tidak diketahui (email tidak terdaftar / akun terkunci) ke _platform.
// Email TIDAK disimpan mentah, hanya hash pendek. Throttled per IP dan
// global supaya audit store tidak bisa dibanjiri. Tidak pernah
// mengembalikan error; kegagalan hanya di-log.
func RecordPlatformLoginAttempt(store Store, ip, email, userAgent, reason string) {
	if !platformThrottle.allow(strings.TrimSpace(ip), time.Now()) {
		return
	}
	if err := RecordEvent(
		store,
		PlatformTenantID,
		PlatformAnonymousUserID,
		ActionLoginFailed,
		"/api/auth/login",
		OutcomeFailure,
		map[string]string{
			"reason":     reason,
			"ip":         ip,
			"email_hash": emailHash(email),
			"user_agent": truncate(userAgent, platformMaxUALen),
		},
	); err != nil {
		log.Printf("WARN: platform audit logging failed (%s): %v", reason, err)
	}
}

// RecordLoginBlocked mencatat login yang ditolak SETELAH password benar
// (akun dihapus / suspended / email belum verifikasi). Tenant diketahui,
// jadi masuk ke audit tenant tersebut (tab Security).
func RecordLoginBlocked(store Store, tenantID, userID, ip, userAgent, reason string) {
	if err := RecordEvent(
		store,
		tenantID,
		userID,
		ActionLoginFailed,
		"/api/auth/login",
		OutcomeFailure,
		map[string]string{
			"reason":     reason,
			"ip":         ip,
			"user_agent": truncate(userAgent, platformMaxUALen),
		},
	); err != nil {
		log.Printf("WARN: audit logging failed for login blocked (tenant %s, %s): %v", tenantID, reason, err)
	}
}