package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// ============================================================
// EVENT
// ============================================================

type Event struct {
	ID         string          `json:"id"`
	ProjectID  string          `json:"project_id"`
	TenantID   string          `json:"tenant_id"`
	Sequence   int64           `json:"sequence"`
	EventType  string          `json:"event_type"`
	ActorID    string          `json:"actor_id"`
	Payload    json.RawMessage `json:"payload"`
	PrevHash   string          `json:"prev_hash,omitempty"`
	Hash       string          `json:"hash"`
	OccurredAt time.Time       `json:"occurred_at"`
	CreatedAt  time.Time       `json:"created_at"`
}

// IngestRequest — bentuk yang diterima dari client di POST /v1/events.
// OccurredAt opsional (default: waktu server terima request) supaya
// client yang tidak sinkron jam-nya tidak bisa memalsukan urutan
// chain — sequence tetap ditentukan SERVER, bukan dari OccurredAt.
type IngestRequest struct {
	EventType  string          `json:"event_type"`
	ActorID    string          `json:"actor_id"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt *time.Time      `json:"occurred_at,omitempty"`
}

var (
	ErrEventTypeRequired = errors.New("event_type_required")
	ErrChainBroken       = errors.New("hash_chain_broken")
)

func (r *IngestRequest) Validate() error {
	if r.EventType == "" {
		return ErrEventTypeRequired
	}
	if len(r.Payload) == 0 {
		r.Payload = json.RawMessage("{}")
	}
	return nil
}

// ============================================================
// HASH CHAIN — PURE LOGIC
//
// Dipisah dari storage layer SENGAJA supaya bisa di-unit-test
// tanpa Postgres sungguhan (test hash chain logic vs test
// concurrent write DB adalah dua concern berbeda).
// ============================================================

// canonicalJSON menormalisasi payload JSON sebelum digunakan
// sebagai input hash.
//
// Database menggunakan PostgreSQL JSONB. JSONB tidak mempertahankan
// formatting/whitespace JSON asli dan dapat melakukan normalisasi
// representasi object.
//
// Contoh:
//
//	{"seq":3}
//	{"seq": 3}
//
// secara semantik adalah JSON yang sama dan JSONB akan menyimpannya
// dalam bentuk yang dinormalisasi.
//
// Dengan canonicalJSON(), hash yang dibuat sebelum INSERT dan hash
// yang dihitung ulang setelah payload dibaca dari PostgreSQL akan
// menggunakan representasi JSON yang konsisten.
func canonicalJSON(payload json.RawMessage) []byte {
	var value any

	if err := json.Unmarshal(payload, &value); err != nil {
		// Payload yang invalid seharusnya sudah ditolak oleh layer
		// request/database. Fallback ini menjaga helper tetap
		// deterministic untuk pemanggilan internal.
		return payload
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		return payload
	}

	return canonical
}

// ComputeHash menghitung hash satu event berdasarkan seluruh field
// yang immutable-nya penting untuk integritas chain, DITAMBAH
// prev_hash — inilah yang membuat perubahan pada SATU event lama
// akan mengubah hash SEMUA event setelahnya (properti hash chain).
//
// Payload di-canonicalize terlebih dahulu karena kolom PostgreSQL
// events.payload menggunakan JSONB. Dengan begitu representasi
// payload saat INSERT dan saat READ/VERIFY tetap konsisten.
func ComputeHash(
	projectID string,
	sequence int64,
	occurredAt time.Time,
	eventType string,
	actorID string,
	payload json.RawMessage,
	prevHash string,
) string {
	h := sha256.New()

	h.Write([]byte(projectID))
	h.Write([]byte("|"))
	h.Write([]byte(int64ToBytes(sequence)))
	h.Write([]byte("|"))
	h.Write([]byte(occurredAt.UTC().Format(time.RFC3339Nano)))
	h.Write([]byte("|"))
	h.Write([]byte(eventType))
	h.Write([]byte("|"))
	h.Write([]byte(actorID))
	h.Write([]byte("|"))
	h.Write(canonicalJSON(payload))
	h.Write([]byte("|"))
	h.Write([]byte(prevHash))

	return hex.EncodeToString(h.Sum(nil))
}

func int64ToBytes(n int64) []byte {
	return []byte(
		// representasi decimal string — deterministik lintas platform,
		// lebih gampang di-debug dibanding binary encoding.
		formatInt64(n),
	)
}

func formatInt64(n int64) string {
	if n == 0 {
		return "0"
	}

	neg := n < 0
	if neg {
		n = -n
	}

	var buf [20]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	if neg {
		i--
		buf[i] = '-'
	}

	return string(buf[i:])
}

// VerifyChain memeriksa satu rentang event BERURUTAN (sudah harus
// terurut ascending by Sequence oleh pemanggil — store layer yang
// menjamin ini lewat ORDER BY sequence ASC).
//
// expectedPrevHash: hash event TERAKHIR SEBELUM rentang ini (kosong
// string kalau verifikasi mulai dari event pertama project).
//
// Return: (valid, brokenAtEventID, error). brokenAtEventID diisi
// event ID PERTAMA yang hash-nya tidak cocok — ini yang ditunjukkan
// ke customer untuk investigasi, bukan sekadar true/false.
func VerifyChain(
	evts []*Event,
	expectedPrevHash string,
) (valid bool, brokenAtEventID string, err error) {
	prevHash := expectedPrevHash

	for _, e := range evts {
		want := ComputeHash(
			e.ProjectID,
			e.Sequence,
			e.OccurredAt,
			e.EventType,
			e.ActorID,
			e.Payload,
			prevHash,
		)

		if want != e.Hash {
			return false, e.ID, nil
		}

		if e.PrevHash != prevHash {
			return false, e.ID, nil
		}

		prevHash = e.Hash
	}

	return true, "", nil
}
