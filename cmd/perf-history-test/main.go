// cmd/perf-history-test/main.go
//
// Alat pengukuran performa untuk ROADMAP §3D — item terbuka kedua
// (bersama §2D, alat terpisah di cmd/perf-verify-test) di track
// dashboard backend. TIDAK dipanggil server, TIDAK didaftarkan
// sebagai route.
//
// PENTING — ISOLASI DATA:
// Program ini membuka file bbolt SENDIRI (default: data/perf-history-test.db),
// BUKAN data/saas-api.db yang dipakai server. bbolt cuma boleh dibuka
// 1 proses sekaligus (exclusive lock), dan 1 juta event palsu TIDAK
// boleh mencemari data produksi/dev Anda. JANGAN arahkan -bolt-path
// ke file server yang sedang jalan.
//
// Yang diukur: usage.UsageHistoryForTenant(tenantID, 30) — method
// Phase 3A yang sudah live di endpoint
// GET /api/tenants/:id/usage/history. Dipakai APA ADANYA, tidak diubah.
//
// Cara pakai:
//   go run ./cmd/perf-history-test -events=1000000
//
// Untuk mengukur ulang tanpa seed ulang:
//   go run ./cmd/perf-history-test -skip-seed -tenant-id=<id>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"runtime"
	"time"

	bolt "go.etcd.io/bbolt"

	"trailseal/usage"
)

// usageEventsBucket — HARUS identik usage/event_store_bbolt.go:31.
// Duplikasi sengaja: nama bucket unexported, lintas package. Kalau
// nama bucket berubah di sana, update juga di sini.
var usageEventsBucket = []byte("usage_events")

func main() {
	eventCount := flag.Int("events", 1_000_000, "jumlah event yang di-seed (default: kuota plan Business, BUSINESS_MODEL.md §3)")
	boltPath := flag.String("bolt-path", "data/perf-history-test.db", "path bbolt KHUSUS test ini — jangan arahkan ke db server")
	skipSeed := flag.Bool("skip-seed", false, "lewati seeding, langsung ukur file yang sudah ada")
	tenantIDFlag := flag.String("tenant-id", "", "tenant ID (wajib bila -skip-seed)")
	flag.Parse()

	tenantID := *tenantIDFlag
	if tenantID == "" {
		tenantID = fmt.Sprintf("perf-test-tenant-%d", time.Now().Unix())
	}

	db, err := bolt.Open(*boltPath, 0600, &bolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		log.Fatalf("FATAL: gagal buka bbolt di %q: %v\n"+
			"Kalau ini file server yang sedang jalan, itu memang akan gagal "+
			"(exclusive lock) — pakai -bolt-path lain.", *boltPath, err)
	}
	defer db.Close()

	store, err := usage.NewBoltEventStore(db)
	if err != nil {
		log.Fatalf("FATAL: gagal init store: %v", err)
	}

	if !*skipSeed {
		fmt.Printf("Seeding %d event (60%% billable / 40%% api_request) untuk tenant %s...\n", *eventCount, tenantID)
		seedStart := time.Now()
		if err := seedUsageEvents(db, tenantID, *eventCount); err != nil {
			log.Fatalf("FATAL: seeding gagal: %v", err)
		}
		fmt.Printf("Seeding selesai dalam %s\n", time.Since(seedStart))
	} else {
		fmt.Printf("Melewati seeding, memakai file %s, tenant %s\n", *boltPath, tenantID)
	}

	runtime.GC()
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	start := time.Now()
	result, err := store.UsageHistoryForTenant(tenantID, 30)
	elapsed := time.Since(start)

	runtime.ReadMemStats(&memAfter)

	if err != nil {
		log.Fatalf("FATAL: UsageHistoryForTenant error: %v", err)
	}

	totalBilled := 0
	for _, p := range result.Series {
		totalBilled += p.Billed
	}

	fmt.Println("\n=== UsageHistoryForTenant(tenantID, 30) ===")
	fmt.Printf("  waktu: %s\n", elapsed)
	fmt.Printf("  memori (heap alloc, delta bertanda): %s\n", memDeltaMB(memBefore, memAfter))
	fmt.Printf("  from=%s to=%s titik_series=%d total_billed=%d\n",
		result.From, result.To, len(result.Series), totalBilled)
	fmt.Printf("  breakdown by_api_key=%d entri, by_event_type=%d entri\n",
		len(result.ByAPIKey), len(result.ByEventType))

	fmt.Println("\n=== Kriteria §3D ===")
	fmt.Println("  Tidak ada target waktu baku di roadmap — tentukan sendiri.")
	fmt.Println("  Lambat → langkah pertama: cache in-memory ±60 detik per tenant+days.")
	fmt.Println("  Tabel agregat harian HANYA bila cache terbukti tidak cukup — jangan")
	fmt.Println("  dibangun sebelum terukur perlu.")
	fmt.Printf("\nTenant ID (buat -skip-seed run ulang): %s\n", tenantID)
	fmt.Printf("Bolt path: %s\n", *boltPath)
}

// memDeltaMB — selisih heap bertanda dalam MB. HeapAlloc adalah
// "bytes hidup sekarang", bukan kumulatif, jadi bisa turun antara dua
// pembacaan (GC jalan di tengah). Pengurangan uint64 polos akan
// underflow jadi angka raksasa kalau itu terjadi — perbaikan dari
// versi awal alat ini.
func memDeltaMB(before, after runtime.MemStats) string {
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	return fmt.Sprintf("%+.2f MB", float64(delta)/1024/1024)
}

// seedUsageEvents menulis batch ke bucket usage_events dengan key
// format identik reverseTimestampKey di usage/event_store_bbolt.go:48-51
// (disalin sebagai reverseTimestampKeyLocal di bawah). Ditulis manual
// per batch — BUKAN lewat store.Save() satu-per-satu — karena Save()
// = 1 transaksi bolt per event (1 juta fsync terpisah = tidak
// praktis untuk fase seeding). Ini TIDAK mengubah bentuk data
// tersimpan: key dan JSON value persis sama seperti lewat Save().
// Fase seeding ini sendiri BUKAN bagian yang diukur §3D — yang diukur
// hanya UsageHistoryForTenant setelah seeding selesai.
//
// Campuran mengikuti definisi billable di usage/event.go (TIDAK
// didefinisikan ulang — string literal di bawah harus sama persis
// dengan billableEventTypes: tools/call, resources/read, prompts/get).
// 60% billable / 40% api_request meniru §3D: "termasuk campuran event
// non-billable, karena scan tetap membaca dan men-decode semuanya".
func seedUsageEvents(db *bolt.DB, tenantID string, total int) error {
	billableTypes := []usage.EventType{
		usage.EventType("tools/call"),
		usage.EventType("resources/read"),
		usage.EventType("prompts/get"),
	}

	now := time.Now().UTC()
	windowStart := now.AddDate(0, 0, -30)
	spanNanos := now.Sub(windowStart).Nanoseconds()

	batchSize := 20_000
	written := 0

	for written < total {
		n := batchSize
		if remaining := total - written; remaining < n {
			n = remaining
		}

		err := db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(usageEventsBucket)
			for j := 0; j < n; j++ {
				i := written + j
				// Sebar rata di 30 hari terakhir supaya semua titik
				// harian di series terisi, bukan menumpuk di satu titik.
				frac := float64(i) / float64(total)
				ts := windowStart.Add(time.Duration(frac * float64(spanNanos)))

				var eventType usage.EventType
				metadata := map[string]string{}
				if i%5 < 3 { // 60% billable
					eventType = billableTypes[i%len(billableTypes)]
					metadata["api_key"] = fmt.Sprintf("perf-test-key-%d", i%50)
				} else { // 40% non-billable, jalur dashboard (D11: metadata user_id)
					eventType = usage.EventTypeAPIRequest
					metadata["user_id"] = "perf-test-user"
				}

				ev := usage.UsageEvent{
					ID:        fmt.Sprintf("perf-usage-%010d", i),
					TenantID:  tenantID,
					EventType: eventType,
					Metadata:  metadata,
					Timestamp: ts,
				}
				data, err := json.Marshal(ev)
				if err != nil {
					return err
				}
				key := reverseTimestampKeyLocal(tenantID, ts, ev.ID)
				if err := b.Put(key, data); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("seed batch at %d: %w", written, err)
		}
		written += n
		if written%200_000 == 0 || written == total {
			fmt.Printf("  ...%d/%d\n", written, total)
		}
	}
	return nil
}

// reverseTimestampKeyLocal — SALINAN PERSIS usage/event_store_bbolt.go:48-51.
// Duplikasi sengaja (fungsi aslinya unexported, lintas package). Ini
// cuma format sort-key (bukan logika keamanan seperti hash chain) —
// risiko duplikasi rendah, tapi tetap: kalau format aslinya berubah,
// update juga di sini, atau urutan seed tidak akan cocok data asli.
func reverseTimestampKeyLocal(tenantID string, ts time.Time, eventID string) []byte {
	reverse := math.MaxInt64 - ts.UnixNano()
	return []byte(fmt.Sprintf("%s:%020d:%s", tenantID, reverse, eventID))
}
