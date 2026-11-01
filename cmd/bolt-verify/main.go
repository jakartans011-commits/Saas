// Command bolt-verify membuktikan sebuah berkas bbolt benar-benar bisa
// dibuka dan berisi data, lalu mencetak jumlah kunci per bucket.
//
// Dipakai setelah restore. Berkas backup yang berukuran benar belum tentu
// bisa dibuka — salinan mentah dari database yang sedang ditulis sering
// terlihat utuh tapi rusak di dalamnya. Satu-satunya pembuktian adalah
// membukanya dan menghitung isinya.
//
// Dijalankan sebagai proses terpisah, jadi HANYA boleh dipakai pada
// berkas yang tidak sedang dipegang server: hasil restore, atau salinan
// backup. bbolt mengunci berkas secara eksklusif; menjalankan ini pada
// database produksi yang sedang hidup akan menggantung sampai timeout.
//
// Pemakaian:
//
//	bolt-verify -db path\ke\saas-api.db
//	bolt-verify -db restored.db -expect-buckets tenants,users
//
// Keluar dengan kode 1 bila berkas tidak bisa dibuka, kosong, atau
// bucket yang diwajibkan tidak ada — supaya bisa dipakai di skrip dan CI.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

func main() {
	var (
		dbPath   = flag.String("db", "", "path berkas bbolt yang diperiksa (wajib)")
		expect   = flag.String("expect-buckets", "", "daftar bucket wajib, dipisah koma")
		minKeys  = flag.Int("min-keys", 1, "jumlah kunci minimum di seluruh database")
		timeout  = flag.Duration("timeout", 10*time.Second, "batas waktu menunggu kunci berkas")
		quietSum = flag.Bool("quiet", false, "hanya cetak ringkasan, tanpa daftar bucket")
	)
	flag.Parse()

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "ERROR: -db wajib diisi")
		flag.Usage()
		os.Exit(2)
	}

	info, err := os.Stat(*dbPath)
	if err != nil {
		fail("berkas tidak bisa dibaca: %v", err)
	}
	if info.Size() == 0 {
		fail("berkas berukuran 0 byte — backup gagal atau terpotong")
	}

	// ReadOnly: jangan pernah memodifikasi berkas yang sedang diperiksa.
	// Timeout: tanpa ini, berkas yang masih dipegang proses lain membuat
	// perintah ini menggantung selamanya tanpa pesan apa pun.
	db, err := bolt.Open(*dbPath, 0400, &bolt.Options{
		ReadOnly: true,
		Timeout:  *timeout,
	})
	if err != nil {
		fail("TIDAK BISA DIBUKA sebagai database bbolt: %v\n"+
			"       (kalau berkas ini sedang dipakai server, hentikan dulu "+
			"atau periksa salinannya saja)", err)
	}
	defer db.Close()

	type bucketCount struct {
		name string
		keys int
	}

	var buckets []bucketCount
	total := 0

	err = db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			n := 0
			if err := b.ForEach(func(k, v []byte) error {
				n++
				return nil
			}); err != nil {
				return err
			}
			buckets = append(buckets, bucketCount{string(name), n})
			total += n
			return nil
		})
	})
	if err != nil {
		fail("gagal membaca isi database: %v", err)
	}

	sort.Slice(buckets, func(i, j int) bool { return buckets[i].name < buckets[j].name })

	fmt.Printf("BERKAS   : %s\n", *dbPath)
	fmt.Printf("UKURAN   : %.2f MB\n", float64(info.Size())/(1024*1024))
	fmt.Printf("BUCKET   : %d\n", len(buckets))
	fmt.Printf("TOTAL KEY: %d\n", total)

	if !*quietSum && len(buckets) > 0 {
		fmt.Println()
		for _, b := range buckets {
			fmt.Printf("  %-32s %8d\n", b.name, b.keys)
		}
		fmt.Println()
	}

	problems := []string{}

	if len(buckets) == 0 {
		problems = append(problems, "database tidak punya bucket sama sekali")
	}
	if total < *minKeys {
		problems = append(problems, fmt.Sprintf(
			"total kunci %d di bawah minimum %d", total, *minKeys))
	}

	if *expect != "" {
		have := map[string]bool{}
		for _, b := range buckets {
			have[b.name] = true
		}
		for _, want := range strings.Split(*expect, ",") {
			want = strings.TrimSpace(want)
			if want == "" {
				continue
			}
			if !have[want] {
				problems = append(problems, "bucket wajib tidak ada: "+want)
			}
		}
	}

	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "\nGAGAL:")
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "  - "+p)
		}
		os.Exit(1)
	}

	fmt.Println("OK — berkas bisa dibuka dan berisi data.")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "GAGAL: "+format+"\n", args...)
	os.Exit(1)
}
