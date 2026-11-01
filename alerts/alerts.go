// alerts/alerts.go
package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============================================================
// CRITICAL ALERTS — webhook (Slack ATAU Discord).
//
// Dipakai untuk kejadian yang butuh perhatian manusia SEGERA:
//   - hash chain event terdeteksi rusak (integritas produk inti)
//   - tenant/project kena quota_exceeded berulang
//   - panic yang di-recover di production
//
// Desain sama seperti observability: package-level singleton yang
// di-Init() sekali di startup (env var), lalu dipanggil dari mana
// saja lewat NotifyCritical TANPA perlu thread dependency ke setiap
// fungsi (events.Append, VerifyHandler, dst tidak perlu ganti
// signature untuk menerima *Notifier).
// ============================================================

// Provider menentukan bentuk payload JSON yang dikirim.
type Provider string

const (
	ProviderSlack   Provider = "slack"
	ProviderDiscord Provider = "discord"
	// ProviderNone — belum dikonfigurasi, semua Notify jadi no-op
	// TAPI tetap di-log ke stdout supaya operator masih bisa lihat
	// di server log walau webhook belum diset.
	ProviderNone Provider = "none"
)

type notifier struct {
	provider   Provider
	webhookURL string
	httpClient *http.Client

	// cooldown mencegah "alert storm": satu insiden yang berulang
	// ratusan kali per detik (mis. quota_exceeded pada endpoint yang
	// dipanggil terus-menerus) hanya mengirim SATU webhook per key
	// per window, sisanya di-drop secara sengaja.
	mu       sync.Mutex
	lastSent map[string]time.Time
	cooldown time.Duration
}

var (
	mu      sync.RWMutex
	current *notifier
)

// Init membaca konfigurasi dari environment:
//
//	ALERT_WEBHOOK_URL      — URL webhook (Slack incoming webhook, atau
//	                          Discord webhook URL)
//	ALERT_WEBHOOK_PROVIDER — "slack" (default) atau "discord"
//	ALERT_COOLDOWN_SECONDS — default 300 (5 menit) per alert key
//
// URL kosong artinya alerts dimatikan (log-only) — aman dipanggil
// di semua environment termasuk test.
func Init() {
	InitWithConfig(
		os.Getenv("ALERT_WEBHOOK_URL"),
		os.Getenv("ALERT_WEBHOOK_PROVIDER"),
	)
}

func InitWithConfig(webhookURL, providerRaw string) {
	mu.Lock()
	defer mu.Unlock()

	provider := ProviderSlack
	switch strings.ToLower(strings.TrimSpace(providerRaw)) {
	case "discord":
		provider = ProviderDiscord
	case "slack", "":
		provider = ProviderSlack
	}

	cooldown := parseCooldownEnv()

	if webhookURL == "" {
		current = &notifier{provider: ProviderNone, lastSent: map[string]time.Time{}, cooldown: cooldown}
		return
	}

	current = &notifier{
		provider:   provider,
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		lastSent:   map[string]time.Time{},
		cooldown:   cooldown,
	}

	log.Printf("alerts: critical alerts enabled via %s webhook", provider)
}

func parseCooldownEnv() time.Duration {
	const defaultSeconds = 300 // 5 menit

	raw := os.Getenv("ALERT_COOLDOWN_SECONDS")
	if raw == "" {
		return defaultSeconds * time.Second
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultSeconds * time.Second
	}

	return time.Duration(seconds) * time.Second
}

// NotifyCritical mengirim satu alert kritikal. fields ditampilkan
// sebagai key-value tambahan (mis. tenant_id, project_id, sequence).
//
// dedupeKey MENENTUKAN cooldown bucket — pakai sesuatu yang stabil
// per jenis insiden + subjek (mis. "quota_exceeded:"+projectID),
// BUKAN unik per-call (kalau unik, cooldown tidak pernah kena dan
// spam tetap terjadi).
func NotifyCritical(dedupeKey, title, message string, fields map[string]string) {
	mu.RLock()
	n := current
	mu.RUnlock()

	// Selalu log ke stdout dulu — ini yang membuat alert tetap
	// terlihat di server log walau webhook belum dikonfigurasi
	// (ProviderNone) atau sedang kena cooldown.
	log.Printf("ALERT[critical] %s: %s (%s)", title, message, fields)

	if n == nil || n.provider == ProviderNone {
		return
	}

	if n.isCooling(dedupeKey) {
		return
	}

	go n.send(title, message, fields)
}

func (n *notifier) isCooling(key string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now()
	if last, ok := n.lastSent[key]; ok && now.Sub(last) < n.cooldown {
		return true
	}
	n.lastSent[key] = now
	return false
}

func (n *notifier) send(title, message string, fields map[string]string) {
	var payload any

	switch n.provider {
	case ProviderDiscord:
		payload = discordPayload(title, message, fields)
	default:
		payload = slackPayload(title, message, fields)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("WARN: alerts: failed to marshal payload: %v", err)
		return
	}

	resp, err := n.httpClient.Post(n.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("WARN: alerts: failed to deliver webhook: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("WARN: alerts: webhook returned status %d", resp.StatusCode)
	}
}

// sortedFieldLines menghasilkan output deterministik (berguna untuk
// test, dan enak dibaca — selalu urutan yang sama di setiap alert).
func sortedFieldLines(fields map[string]string) []string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("*%s*: %s", k, fields[k]))
	}
	return lines
}

func slackPayload(title, message string, fields map[string]string) map[string]any {
	text := fmt.Sprintf(":rotating_light: *%s*\n%s", title, message)
	if lines := sortedFieldLines(fields); len(lines) > 0 {
		text += "\n" + strings.Join(lines, "\n")
	}
	return map[string]any{"text": text}
}

// discordPayload — Discord webhook menerima bentuk yang berbeda dari
// Slack ({"content": ...} bukan {"text": ...}). Kalau webhook Discord
// diberi akhiran "/slack", Discord sebenarnya menerima payload Slack
// apa adanya — tapi kita tidak mengasumsikan itu supaya juga bekerja
// dengan URL webhook Discord biasa.
func discordPayload(title, message string, fields map[string]string) map[string]any {
	desc := message
	if lines := sortedFieldLines(fields); len(lines) > 0 {
		desc += "\n" + strings.Join(lines, "\n")
	}
	return map[string]any{
		"embeds": []map[string]any{
			{
				"title":       title,
				"description": desc,
				"color":       15158332, // merah — insiden kritikal
			},
		},
	}
}

// Enabled melaporkan apakah webhook alert benar-benar terkonfigurasi
// (bukan sekadar log-only).
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current != nil && current.provider != ProviderNone
}