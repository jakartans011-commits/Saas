// observability/sentry.go
package observability

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// ============================================================
// SENTRY ERROR TRACKING — klien HTTP minimal, TIDAK menambah
// dependency eksternal baru (net/http std lib saja).
//
// Kenapa tidak pakai github.com/getsentry/sentry-go: repo ini
// tidak punya akses jaringan untuk `go get` + regenerate go.sum
// saat kode ini ditulis. Endpoint "store" Sentry API cukup
// sederhana (satu POST JSON) sehingga klien manual di sini realistis
// dan cukup untuk kebutuhan production error tracking dasar:
// capture error + tags + stack trace, tanpa breadcrumbs/session
// tracking penuh dari SDK resmi.
//
// Kalau nanti mau upgrade ke SDK resmi, cukup ganti isi Client di
// file ini — semua caller memakai CaptureError/CapturePanic package-level,
// jadi tidak ada call site lain yang perlu berubah.
// ============================================================

// dsn — hasil parse SENTRY_DSN, format:
//
//	https://<public_key>[:<secret_key>]@<host>/<project_id>
//	https://<public_key>[:<secret_key>]@<host>/<path>/<project_id>  (self-hosted dengan path prefix)
type dsn struct {
	publicKey string
	secretKey string // opsional — DSN modern hanya punya public key
	storeURL  string // endpoint lengkap "…/api/<project_id>/store/"
}

func parseDSN(raw string) (*dsn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid SENTRY_DSN: %w", err)
	}
	if u.User == nil {
		return nil, fmt.Errorf("invalid SENTRY_DSN: missing public key")
	}

	projectID := strings.Trim(u.Path, "/")
	if projectID == "" {
		return nil, fmt.Errorf("invalid SENTRY_DSN: missing project id")
	}

	secret, _ := u.User.Password()

	storeURL := fmt.Sprintf("%s://%s/api/%s/store/", u.Scheme, u.Host, projectID)

	return &dsn{
		publicKey: u.User.Username(),
		secretKey: secret,
		storeURL:  storeURL,
	}, nil
}

// Client — thread-safe, aman dipakai dari banyak goroutine/handler
// fiber secara bersamaan.
type Client struct {
	dsn        *dsn
	httpClient *http.Client
	env        string
	release    string
	serverName string
}

var (
	mu      sync.RWMutex
	current *Client // nil == observability tidak dikonfigurasi (no-op)
)

// Init membaca SENTRY_DSN dari environment dan mengaktifkan error
// tracking. DSN kosong ATAU tidak valid membuat semua CaptureError/
// CapturePanic menjadi no-op (aman dipanggil di test dan environment
// tanpa Sentry) — TIDAK fatal, hanya di-log sekali.
func Init() {
	InitWithDSN(os.Getenv("SENTRY_DSN"))
}

// InitWithDSN — variant eksplisit, dipakai test supaya tidak
// bergantung ke environment variable proses.
func InitWithDSN(rawDSN string) {
	mu.Lock()
	defer mu.Unlock()

	if rawDSN == "" {
		current = nil
		return
	}

	d, err := parseDSN(rawDSN)
	if err != nil {
		log.Printf("WARN: observability.Init: %v — error tracking disabled", err)
		current = nil
		return
	}

	serverName := os.Getenv("HOSTNAME")
	if serverName == "" {
		serverName, _ = os.Hostname()
	}

	current = &Client{
		dsn:        d,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		env:        envOrDefault("TRAILSEAL_ENV", "production"),
		release:    os.Getenv("TRAILSEAL_RELEASE"),
		serverName: serverName,
	}

	log.Printf("observability: Sentry error tracking enabled (project store: %s)", d.storeURL)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newEventID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sentryEvent — subset dari Sentry event schema yang relevan
// (https://develop.sentry.dev/sdk/event-payloads/).
type sentryEvent struct {
	EventID     string            `json:"event_id"`
	Timestamp   string            `json:"timestamp"`
	Level       string            `json:"level"`
	Platform    string            `json:"platform"`
	ServerName  string            `json:"server_name,omitempty"`
	Environment string            `json:"environment,omitempty"`
	Release     string            `json:"release,omitempty"`
	Message     string            `json:"message,omitempty"`
	Exception   *sentryException  `json:"exception,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Extra       map[string]any    `json:"extra,omitempty"`
}

type sentryException struct {
	Values []sentryExceptionValue `json:"values"`
}

type sentryExceptionValue struct {
	Type       string `json:"type"`
	Value      string `json:"value"`
	Stacktrace *struct {
		Frames []sentryFrame `json:"frames"`
	} `json:"stacktrace,omitempty"`
}

type sentryFrame struct {
	Function string `json:"function"`
	Raw      string `json:"raw,omitempty"`
}

// CaptureError mengirim error ke Sentry sebagai event level "error".
// tags dipakai untuk filtering di Sentry UI (mis. tenant_id, project_id,
// route). Non-blocking: pengiriman dilakukan di goroutine terpisah
// supaya request path caller tidak ikut menunggu network Sentry.
//
// No-op (aman dipanggil) kalau observability belum di-Init atau
// err == nil.
func CaptureError(err error, tags map[string]string) {
	if err == nil {
		return
	}
	captureMessage("error", err.Error(), stackFrames(nil), tags)
}

// CapturePanic dipakai dari recover middleware — recovered adalah
// nilai yang dikembalikan oleh recover(), stack adalah hasil
// debug.Stack() yang diambil SAAT masih di dalam defer/recover
// (stack trace hilang kalau diambil belakangan).
func CapturePanic(recovered any, stack []byte, tags map[string]string) {
	msg := fmt.Sprintf("panic: %v", recovered)
	captureMessage("fatal", msg, stackFramesFromTrace(stack), tags)
}

func stackFrames(raw []byte) []sentryFrame {
	if raw == nil {
		raw = debug.Stack()
	}
	return stackFramesFromTrace(raw)
}

func stackFramesFromTrace(raw []byte) []sentryFrame {
	lines := strings.Split(string(raw), "\n")
	frames := make([]sentryFrame, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		frames = append(frames, sentryFrame{Raw: l})
	}
	// Sentry menampilkan frame TERBAWAH (paling dekat ke exception)
	// paling akhir di list — debug.Stack() sudah urut dari
	// pemanggil terdekat, jadi tidak perlu dibalik untuk tujuan kita
	// (readability di dashboard, bukan symbolication presisi).
	//
	// AMAN dari data race: raw di sini SELALU berasal dari
	// debug.Stack() (CaptureError, lewat stackFrames) atau dari stack
	// []byte yang caller ambil sendiri lewat debug.Stack() di dalam
	// defer/recover (CapturePanic). debug.Stack() mengalokasikan
	// buffer baru setiap kali dipanggil — tidak pernah menunjuk ke
	// buffer pooled fasthttp — jadi strings.Split di sini tidak perlu
	// di-clone lagi.
	return frames
}

func captureMessage(level, message string, frames []sentryFrame, tags map[string]string) {
	mu.RLock()
	c := current
	mu.RUnlock()

	if c == nil {
		return
	}

	// ============================================================
	// DATA RACE FIX (found by `go test ./... -race`,
	// TestToolExecution_OnlyAllowlistedTypes):
	//
	// tags — and in principle message — commonly originate from
	// fiber.Ctx accessors such as c.Path() and c.Method() (see
	// observability/middleware.go's ErrorReporting). Fiber/fasthttp
	// backs those strings with a pooled byte buffer that gets reused
	// for the NEXT incoming request the instant this handler returns.
	//
	// c.send(evt) below runs in its own goroutine on purpose — a slow
	// Sentry call must never block the request path — but that means
	// it can still be reading those bytes (via json.Marshal) after
	// fasthttp has already started overwriting the same buffer with a
	// new request. That is a genuine data race, not a test artifact:
	// under concurrent load in production this is the same failure
	// mode, just less likely to be *caught* without the race
	// detector's instrumentation forcing the interleaving.
	//
	// Fix: force a real copy of every string that crosses into the
	// goroutine, at this single choke point all callers funnel
	// through — rather than trusting every current and future call
	// site to remember to clone its own tags. strings.Clone
	// guarantees a fresh allocation (unlike a plain string(b)
	// conversion, which can still alias the same backing array).
	// ============================================================

	safeMessage := strings.Clone(message)

	var safeTags map[string]string
	if len(tags) > 0 {
		safeTags = make(map[string]string, len(tags))
		for k, v := range tags {
			safeTags[strings.Clone(k)] = strings.Clone(v)
		}
	}

	evt := &sentryEvent{
		EventID:     newEventID(),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Level:       level,
		Platform:    "go",
		ServerName:  c.serverName,
		Environment: c.env,
		Release:     c.release,
		Message:     safeMessage,
		Tags:        safeTags,
	}

	if len(frames) > 0 {
		evt.Exception = &sentryException{
			Values: []sentryExceptionValue{
				{
					Type:  "error",
					Value: safeMessage,
					Stacktrace: &struct {
						Frames []sentryFrame `json:"frames"`
					}{Frames: frames},
				},
			},
		}
	}

	// Fire-and-forget: kegagalan mengirim ke Sentry TIDAK BOLEH
	// mempengaruhi request yang sedang diproses. Semua data di evt
	// sekarang sudah owned copies (lihat blok DATA RACE FIX di atas),
	// jadi goroutine ini aman baca evt kapan pun setelah ini.
	go c.send(evt)
}

func (c *Client) send(evt *sentryEvent) {
	body, err := json.Marshal(evt)
	if err != nil {
		log.Printf("WARN: observability: failed to marshal event: %v", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, c.dsn.storeURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("WARN: observability: failed to build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	auth := fmt.Sprintf(
		"Sentry sentry_version=7, sentry_client=trailseal-go/1.0, sentry_key=%s",
		c.dsn.publicKey,
	)
	if c.dsn.secretKey != "" {
		auth += fmt.Sprintf(", sentry_secret=%s", c.dsn.secretKey)
	}
	req.Header.Set("X-Sentry-Auth", auth)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("WARN: observability: failed to send event to Sentry: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("WARN: observability: Sentry returned status %d", resp.StatusCode)
	}
}

// Enabled melaporkan apakah error tracking sedang aktif — dipakai
// oleh /health atau startup log, BUKAN untuk gate business logic.
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current != nil
}
