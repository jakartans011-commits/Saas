package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ============================================================
// PEMOTONGAN EKOR RANTAI
//
// VerifyChain hanya memastikan baris yang ADA saling tersambung.
// Menghapus event TERAKHIR tidak memutus sambungan apa pun: sisanya
// tetap utuh dari awal sampai akhir, hanya lebih pendek. Tanpa
// pemeriksaan di bawah, seseorang dengan akses database bisa menghapus
// jejak terbaru dan verifikasi tetap melaporkan valid — justru skenario
// yang paling dikhawatirkan pembeli audit log.
//
// Pembandingnya sudah tersimpan. project_chain_state.last_sequence
// mencatat nomor urut tertinggi yang pernah ditulis, dan baris itu tidak
// ikut terhapus saat event dihapus.
//
// Retention BUKAN pemotongan. Job retensi menghapus event TERTUA, bukan
// terbaru; kalau semua event sudah lewat masa simpan, tabelnya memang
// jadi kosong secara sah. Yang membedakan: purge selalu meninggalkan
// jejak di event_retention_checkpoints. Jadi pembandingnya adalah yang
// tertinggi antara event yang masih ada dan purged_through_sequence —
// bukan sekadar jumlah baris yang tersisa.
// ============================================================

// chainTailTruncated melaporkan apakah ujung rantai hilang: ada event
// yang pernah ditulis tapi sekarang tidak ada di tabel events dan juga
// tidak tercatat sebagai dipurge oleh retention.
//
// evts harus terurut menaik berdasarkan sequence (seperti yang
// dikembalikan query VerifyRange), karena elemen terakhirnya dipakai
// sebagai ujung rantai yang teramati.
func (s *PostgresStore) chainTailTruncated(
	ctx context.Context,
	projectID string,
	evts []*Event,
) (bool, error) {
	var lastSequence int64

	err := s.pool.QueryRow(ctx, `
		SELECT last_sequence
		FROM project_chain_state
		WHERE project_id = $1
	`, projectID).Scan(&lastSequence)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Project belum pernah ingest event lewat Append — tidak ada
		// ujung rantai yang tercatat untuk dibandingkan. Ini juga yang
		// terjadi pada fixture test yang menulis event langsung ke tabel.
		return false, nil

	case err != nil:
		return false, fmt.Errorf(
			"failed to load chain state for truncation check: %w",
			err,
		)
	}

	if lastSequence == 0 {
		return false, nil
	}

	// Ujung tertinggi yang bisa dipertanggungjawabkan: event terakhir
	// yang masih ada, atau sequence terakhir yang dipurge retention —
	// mana pun yang lebih tinggi.
	var highestAccountedFor int64
	if n := len(evts); n > 0 {
		highestAccountedFor = evts[n-1].Sequence
	}

	var purgedThrough int64
	purgeErr := s.pool.QueryRow(ctx, `
		SELECT purged_through_sequence
		FROM event_retention_checkpoints
		WHERE project_id = $1
	`, projectID).Scan(&purgedThrough)

	switch {
	case purgeErr == nil:
		if purgedThrough > highestAccountedFor {
			highestAccountedFor = purgedThrough
		}

	case errors.Is(purgeErr, pgx.ErrNoRows):
		// Belum pernah ada purge; tidak ada yang perlu ditambahkan.

	default:
		return false, fmt.Errorf(
			"failed to load retention checkpoint for truncation check: %w",
			purgeErr,
		)
	}

	return lastSequence > highestAccountedFor, nil
}
