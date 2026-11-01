# Spec Eksekusi — 2B.4: First Value Moment

Goal: setelah generate API key (2B.3 selesai), user bisa langsung kirim
1 event nyata dan LIHAT hasilnya (usage naik, audit event tercatat) —
tanpa baca dokumentasi terpisah, tanpa tanya developer.

Backend terkait (`events/`, `mcp/`) **Locked** — tidak boleh diubah.
Semua kerjaan di sini murni UI/dokumentasi + wiring baca-saja ke data yang sudah ada.

---

## 0. Hasil Investigasi (Konfirmasi)

Berdasarkan investigasi, berikut adalah detail teknis yang digunakan untuk implementasi:

1.  **Endpoint yang benar:** `POST /v1/events`
2.  **Auth:** Menggunakan header `X-API-Key` (BUKAN JWT).
    - Snippet cURL: `-H "X-API-Key: YOUR_API_KEY_HERE"`
3.  **Project ID:** TIDAK diperlukan di body atau URL (otomatis dari API key).
4.  **Request Body Minimal:**
    ```json
    {
      "event_type": "test.first_event"
    }
    ```
5.  **Expected Response (201 Created):**
    ```json
    {
      "id": "evt_...",
      "sequence": 1,
      "hash": "...",
      "event_type": "test.first_event",
      "received_at": "2023-10-27T10:00:00Z"
    }
    ```

---

## 1. Implementasi UI: `dashboard.html` (Opsi A)

Section baru di `dashboard.html` yang muncul HANYA kalau `TotalRequestsRecorded == 0` (tenant belum pernah kirim event apa pun).

**Isi (DoD):**

- 1 cURL snippet siap-copy:
  ```bash
  curl -X POST https://api.saas-agent.com/v1/events \
    -H "X-API-Key: YOUR_API_KEY_HERE" \
    -H "Content-Type: application/json" \
    -d '{"event_type": "test.first_event"}'
  ```
- Tombol "Copy command"
- Expected response ditampilkan sebagai contoh JSON statis.
- Link "Refresh this page after sending your first event to see it appear in Usage & Audit".

## 2. Troubleshooting section (statis, di halaman yang sama)

Daftar error umum sesuai temuan investigasi:

- **401 Unauthorized (`invalid_api_key` / `api_key_revoked`):** API Key salah atau sudah tidak aktif.
- **403 Forbidden (`insufficient_scope`):** API Key tidak memiliki izin untuk melakukan ingest (butuh scope ingest/write).
- **429 Too Many Requests (`quota_exceeded`):** Kuota event tenant sudah habis.
- **400 Bad Request (`event_type_required`):** Field `event_type` wajib diisi di request body.

## 3. Batas — JANGAN dikerjakan

- ❌ Live "waiting for your first event" indicator dengan polling — cukup manual refresh.
- ❌ Ubah endpoint ingest/MCP itu sendiri.
- ❌ MCP quickstart — fokus HANYA event ingest dulu.

## 4. Bukti setelah selesai

- `go build ./...` sukses.
- `go test ./cmd/saas-api/...` sukses.
- UI `dashboard.html` menampilkan section "Getting Started" saat data kosong.
