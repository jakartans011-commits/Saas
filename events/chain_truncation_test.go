package events

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// ============================================================
// PEMOTONGAN EKOR RANTAI
//
// Rantai hash TrailSeal mendeteksi event yang DIUBAH: setiap event
// menyimpan hash pendahulunya, jadi satu byte yang diganti memutus
// sambungannya dan VerifyChain menunjuk persis di mana.
//
// Tapi menghapus event TERAKHIR tidak memutus apa pun. Yang tersisa
// tetap tersambung sempurna dari awal sampai akhir — hanya saja
// "akhir"-nya sekarang lebih pendek. VerifyRange menanyakan
//
//	SELECT ... FROM events WHERE sequence >= $2 ORDER BY sequence ASC
//
// lalu memverifikasi baris yang KEBETULAN ADA. Ia tidak pernah
// bertanya "seharusnya ada berapa".
//
// Itu justru skenario yang dibayangkan pembeli B2B: orang dalam yang
// menghapus jejak paling baru, bukan mengedit jejak lama.
//
// Dan jawabannya sudah tersimpan. project_chain_state.last_sequence
// mencatat nomor urut tertinggi yang pernah ditulis, dan baris itu
// tidak ikut terhapus saat event dihapus. Bandingkan keduanya dan
// pemotongan ekor ketahuan seketika.
//
// Test ini membuat rantai lima event yang sah lewat helper yang sama
// dengan test retention, memastikan ia valid, lalu menghapus dua event
// terakhir TANPA menyentuh project_chain_state — persis yang bisa
// dilakukan seseorang dengan akses database.
//
//	go test ./events -run VerifyRange_DetectsTruncated -v
// ============================================================

func TestVerifyRange_DetectsTruncatedChainTail(t *testing.T) {
	pool := retentionTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		tenantID   = "retention-test-tenant"
		totalSeq   = 5
		deleteFrom = 4 // hapus sequence 4 dan 5
	)

	projectID := createRetentionTestProject(t, ctx, pool, 365)

	// ---- Bangun rantai lima event yang sah ----

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin fixture transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC().Truncate(time.Microsecond)

	prevHash := ""
	lastHash := ""
	for seq := int64(1); seq <= totalSeq; seq++ {
		_, hash := insertRetentionTestEvent(
			t,
			ctx,
			tx,
			projectID,
			tenantID,
			seq,
			now.AddDate(0, 0, -int(totalSeq+1-seq)),
			"chain.truncation.test",
			"actor-1",
			json.RawMessage(fmt.Sprintf(`{"seq":%d}`, seq)),
			prevHash,
		)
		prevHash = hash
		lastHash = hash
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit fixture transaction: %v", err)
	}

	// project_chain_state seperti yang ditulis Append di produksi: ia
	// mencatat ujung rantai, dan ia TIDAK ikut terhapus kalau seseorang
	// menghapus baris di tabel events.
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_chain_state (project_id, last_sequence, last_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (project_id)
		DO UPDATE SET last_sequence = $2, last_hash = $3, updated_at = now()
	`, projectID, int64(totalSeq), lastHash); err != nil {
		t.Fatalf("failed to seed project_chain_state: %v", err)
	}

	store := NewPostgresStore(pool)

	// ---- Dasar: rantai utuh harus valid ----

	valid, _, checked, err := store.VerifyRange(ctx, projectID, 1)
	if err != nil {
		t.Fatalf("verify rantai utuh gagal: %v", err)
	}
	if !valid {
		t.Fatal("rantai utuh harus valid — fixture-nya yang salah, bukan kodenya")
	}
	if checked != totalSeq {
		t.Fatalf("checkedCount=%d, harusnya %d", checked, totalSeq)
	}

	// ---- Potong ekornya ----

	res, err := pool.Exec(ctx, `
		DELETE FROM events
		WHERE project_id = $1
		  AND sequence >= $2
	`, projectID, int64(deleteFrom))
	if err != nil {
		t.Fatalf("failed to truncate chain tail: %v", err)
	}
	if n := res.RowsAffected(); n != totalSeq-deleteFrom+1 {
		t.Fatalf("terhapus %d baris, harusnya %d", n, totalSeq-deleteFrom+1)
	}

	// ---- Harus ketahuan ----

	valid, _, checked, err = store.VerifyRange(ctx, projectID, 1)

	if err == nil && valid {
		t.Fatalf(
			"PEMOTONGAN EKOR TIDAK TERDETEKSI.\n"+
				"  project_chain_state.last_sequence = %d\n"+
				"  event yang tersisa di tabel        = %d\n"+
				"  VerifyRange melaporkan valid=true, checkedCount=%d\n"+
				"\n"+
				"Dua event terakhir dihapus dan rantai tetap dinyatakan utuh. "+
				"Seseorang dengan akses database bisa menghapus jejak terbaru "+
				"tanpa satu pun alarm berbunyi — padahal justru itu yang "+
				"dijual TrailSeal. Angka pembandingnya sudah tersimpan di "+
				"project_chain_state.last_sequence; VerifyRange hanya belum "+
				"pernah membacanya.",
			totalSeq,
			deleteFrom-1,
			checked,
		)
	}

	// Setelah diperbaiki: chain dilaporkan tidak valid (atau error yang
	// menjelaskan pemotongan). checkedCount tetap melaporkan berapa yang
	// sempat diperiksa, supaya operator tahu seberapa jauh rantainya utuh.
	if err == nil && checked != deleteFrom-1 {
		t.Errorf("checkedCount=%d, harusnya %d (jumlah event yang tersisa)",
			checked, deleteFrom-1)
	}
}
