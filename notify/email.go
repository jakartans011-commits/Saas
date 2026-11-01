// notify/email.go
package notify

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// ============================================================
// EMAIL QUOTA NOTIFICATIONS
//
// FASE INI: interface + queue durable SAJA. TIDAK ADA provider
// email (SMTP/SendGrid/dst) yang benar-benar mengirim — keputusan
// eksplisit supaya tidak menebak provider yang salah. Sender asli
// tinggal diimplementasikan sebagai EmailSender baru dan dipasang
// lewat SetSender() — TIDAK ADA call site lain (events package,
// dsb) yang perlu berubah saat itu terjadi.
//
// Kenapa queue, bukan kirim langsung dari request path:
//   - Append() ada di hot path ingest event — TIDAK BOLEH menunggu
//     network provider email.
//   - Dedup per (tenant, template, period) mencegah tenant yang
//     stuck di 95% quota selama sebulan menerima ratusan email
//     "kamu di 80%" yang sama berulang-ulang.
// ============================================================

// Template — jenis notifikasi. String literal (bukan iota) supaya
// stabil dipakai sebagai bagian dedupe key yang disimpan di bbolt.
type Template string

const (
	TemplateQuota80     Template = "quota_80_percent"
	TemplateQuota100    Template = "quota_100_percent"
	TemplateChainBroken Template = "chain_integrity_broken"
)

// EmailJob — satu notifikasi yang sedang menunggu dikirim.
type EmailJob struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	ProjectID string            `json:"project_id,omitempty"`
	To        string            `json:"to,omitempty"` // opsional — kalau kosong, sender resolve dari TenantID sendiri
	Template  Template          `json:"template"`
	Data      map[string]string `json:"data,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	SentAt    *time.Time        `json:"sent_at,omitempty"`
	Attempts  int               `json:"attempts"`
	LastError string            `json:"last_error,omitempty"`
}

// EmailSender — belum ada implementasi production di fase ini.
// LoggingSender di bawah memenuhi interface ini supaya queue bisa
// di-drain (dan diverifikasi lewat log/test) sebelum provider asli
// dipasang.
type EmailSender interface {
	Send(job *EmailJob) error
}

// LoggingSender hanya mencatat job ke log — placeholder yang aman
// dipakai di production sampai provider (SMTP/API) dipilih. TIDAK
// menandakan email benar-benar terkirim ke siapa pun.
type LoggingSender struct{}

func (LoggingSender) Send(job *EmailJob) error {
	log.Printf(
		"notify: [NO SENDER CONFIGURED] would send %q to tenant=%s project=%s data=%v",
		job.Template, job.TenantID, job.ProjectID, job.Data,
	)
	return nil
}

// ============================================================
// BOLT-BACKED QUEUE
// ============================================================

const (
	bucketPending = "notify_email_pending"
	bucketDedupe  = "notify_email_dedupe" // key: tenantID|template|period -> jobID
)

type BoltEmailQueue struct {
	db *bolt.DB
}

func NewBoltEmailQueue(db *bolt.DB) (*BoltEmailQueue, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketPending)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketDedupe)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init notify email queue buckets: %w", err)
	}
	return &BoltEmailQueue{db: db}, nil
}

// currentPeriod — sama seperti events.currentPeriod ('YYYY-MM' UTC):
// satu notifikasi per template per tenant per periode kuota bulanan.
func currentPeriod() string {
	return time.Now().UTC().Format("2006-01")
}

func dedupeKey(tenantID string, tmpl Template, period string) []byte {
	return []byte(tenantID + "|" + string(tmpl) + "|" + period)
}

// EnqueueQuotaThreshold mendaftarkan notifikasi "tenant menyentuh X%
// quota" untuk PERIODE BULAN INI SAJA (idempotent — panggilan
// berulang untuk tenant/template/period yang sama TIDAK menambah
// entri baru).
//
// Return (enqueued bool, err error): enqueued=false berarti sudah
// pernah didaftarkan bulan ini — BUKAN error, caller (mis. hot path
// ingest event) tidak perlu treat ini sebagai kegagalan.
func (q *BoltEmailQueue) EnqueueQuotaThreshold(tenantID, projectID string, tmpl Template, data map[string]string) (enqueued bool, err error) {
	if tenantID == "" {
		return false, nil
	}

	period := currentPeriod()
	dk := dedupeKey(tenantID, tmpl, period)

	job := &EmailJob{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		ProjectID: projectID,
		Template:  tmpl,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}

	jobBytes, err := json.Marshal(job)
	if err != nil {
		return false, fmt.Errorf("failed to marshal email job: %w", err)
	}

	err = q.db.Update(func(tx *bolt.Tx) error {
		dedupe := tx.Bucket([]byte(bucketDedupe))
		if existing := dedupe.Get(dk); existing != nil {
			// Sudah pernah dikirim/dijadwalkan untuk kombinasi ini
			// bulan ini — skip diam-diam.
			enqueued = false
			return nil
		}

		pending := tx.Bucket([]byte(bucketPending))
		if err := pending.Put([]byte(job.ID), jobBytes); err != nil {
			return err
		}
		if err := dedupe.Put(dk, []byte(job.ID)); err != nil {
			return err
		}

		enqueued = true
		return nil
	})

	return enqueued, err
}

// EnqueueChainBroken mendaftarkan notifikasi "Chain Integrity broken"
// untuk tenant. Dedup per (tenant, template, period) sama seperti
// quota threshold — mencegah spam kalau verifikasi berulang kali
// menemukan chain yang sama masih broken.
func (q *BoltEmailQueue) EnqueueChainBroken(tenantID, eventID string) (enqueued bool, err error) {
	data := map[string]string{"event_id": eventID}
	return q.EnqueueQuotaThreshold(tenantID, "", TemplateChainBroken, data)
}

func EnqueueChainBroken(tenantID, eventID string) {
	if globalQueue == nil {
		return
	}
	if _, err := globalQueue.EnqueueChainBroken(tenantID, eventID); err != nil {
		log.Printf("WARN: notify: failed to enqueue chain_broken for tenant %s: %v", tenantID, err)
	}
}

// Pending mengembalikan sampai `limit` job yang belum terkirim,
// urutan insertion order (bbolt cursor iterates keys in byte order —
// job.ID adalah UUID sehingga urutan TIDAK kronologis; cukup untuk
// draining queue, bukan untuk jaminan FIFO ketat).
func (q *BoltEmailQueue) Pending(limit int) ([]*EmailJob, error) {
	var out []*EmailJob

	err := q.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketPending))
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			if limit > 0 && len(out) >= limit {
				break
			}
			var job EmailJob
			if err := json.Unmarshal(v, &job); err != nil {
				continue // entri korup — skip, jangan gagalkan seluruh drain
			}
			if job.SentAt == nil && job.Attempts < MaxSendAttempts {
				out = append(out, &job)
			}
		}
		return nil
	})

	return out, err
}

// MaxSendAttempts: job yang gagal sebanyak ini diparkir (tetap di bucket
// untuk inspeksi) dan tidak lagi diambil Pending, supaya tidak menghabiskan
// batch dan menahan job sehat.
const MaxSendAttempts = 5

// MarkSent menghapus job dari antrian pending. Dedupe entry
// SENGAJA TIDAK dihapus — itu yang mencegah re-notifikasi di
// periode yang sama walau job sudah terkirim.
func (q *BoltEmailQueue) MarkSent(jobID string) error {
	return q.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketPending)).Delete([]byte(jobID))
	})
}

// MarkFailed mencatat kegagalan kirim TANPA menghapus job — job
// tetap di queue supaya worker berikutnya retry.
func (q *BoltEmailQueue) MarkFailed(jobID string, sendErr error) error {
	return q.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketPending))
		v := b.Get([]byte(jobID))
		if v == nil {
			return nil // sudah di-drain job lain, tidak apa-apa
		}
		var job EmailJob
		if err := json.Unmarshal(v, &job); err != nil {
			return err
		}
		job.Attempts++
		job.LastError = sendErr.Error()

		if job.Attempts == MaxSendAttempts {
			log.Printf("WARN: notify: job %s (tenant %s, template %s) parked after %d failed attempts: %s",
				job.ID, job.TenantID, job.Template, job.Attempts, job.LastError)
		}

		updated, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return b.Put([]byte(jobID), updated)
	})
}

// ============================================================
// PACKAGE-LEVEL SINGLETON — pola sama seperti observability/alerts:
// dipasang sekali di startup lewat SetQueue(), dipanggil dari mana
// saja (events package, dsb) TANPA mengubah signature fungsi yang
// sudah ada/di-test.
// ============================================================

var globalQueue *BoltEmailQueue

// SetQueue dipanggil sekali dari app.go saat startup.
func SetQueue(q *BoltEmailQueue) {
	globalQueue = q
}

// EnqueueQuotaThreshold — versi package-level, no-op aman kalau
// SetQueue belum pernah dipanggil (mis. di test yang tidak
// menyentuh notify sama sekali).
func EnqueueQuotaThreshold(tenantID, projectID string, tmpl Template, data map[string]string) {
	if globalQueue == nil {
		return
	}
	if _, err := globalQueue.EnqueueQuotaThreshold(tenantID, projectID, tmpl, data); err != nil {
		log.Printf("WARN: notify: failed to enqueue %s for tenant %s: %v", tmpl, tenantID, err)
	}
}

// ============================================================
// WORKER — drain loop sederhana. Dipanggil dari cmd/notify-worker
// (proses terpisah, pola sama seperti cmd/retention-job) SAMPAI
// provider email sungguhan dipasang, worker ini memakai LoggingSender
// secara default.
// ============================================================

// DrainOnce memproses semua job pending SEKALI (bukan long-running
// loop) — cocok dipanggil dari cron/scheduled task, konsisten
// dengan pola RunOnce() di events.RetentionJob.
func DrainOnce(q *BoltEmailQueue, sender EmailSender, batchSize int) (sent int, failed int, err error) {
	jobs, err := q.Pending(batchSize)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to load pending email jobs: %w", err)
	}

	for _, job := range jobs {
		if sendErr := sender.Send(job); sendErr != nil {
			failed++
			if markErr := q.MarkFailed(job.ID, sendErr); markErr != nil {
				log.Printf("WARN: notify: failed to record send failure for job %s: %v", job.ID, markErr)
			}
			continue
		}

		if markErr := q.MarkSent(job.ID); markErr != nil {
			log.Printf("WARN: notify: failed to mark job %s sent: %v", job.ID, markErr)
			continue
		}
		sent++
	}

	return sent, failed, nil
}
