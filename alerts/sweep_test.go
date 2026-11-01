package alerts

import (
	"fmt"
	"testing"
	"time"
)

// ============================================================
// PENYAPUAN notifier.lastSent
//
//	go test ./alerts -run IsCooling_Sweep -v
// ============================================================

func newSweepTestNotifier(cooldown time.Duration) *notifier {
	return &notifier{
		provider: ProviderNone,
		lastSent: map[string]time.Time{},
		cooldown: cooldown,
	}
}

func TestIsCooling_SweepsCooledEntries(t *testing.T) {
	const cooldown = time.Minute
	n := newSweepTestNotifier(cooldown)

	cooled := time.Now().Add(-2 * cooldown)
	for i := 0; i < alertDedupeSweepThreshold+50; i++ {
		n.lastSent[fmt.Sprintf("quota_exceeded:proj-%d", i)] = cooled
	}

	before := len(n.lastSent)

	if n.isCooling("chain_broken:proj-baru") {
		t.Fatal("key yang belum pernah dipakai tidak boleh dianggap masih cooling")
	}

	after := len(n.lastSent)

	if after >= before {
		t.Fatalf(
			"penampung tidak menyusut: sebelum=%d sesudah=%d.\n"+
				"Semua entri yang ditanam cooldown-nya sudah lewat dan tidak berguna lagi.",
			before, after,
		)
	}
	if after != 1 {
		t.Errorf("tersisa %d entri, harusnya 1 (hanya key yang baru dicatat)", after)
	}
}

func TestIsCooling_SweepKeepsActiveCooldowns(t *testing.T) {
	const cooldown = time.Minute
	n := newSweepTestNotifier(cooldown)

	cooled := time.Now().Add(-2 * cooldown)
	active := time.Now()

	const activeCount = 30
	for i := 0; i < alertDedupeSweepThreshold; i++ {
		n.lastSent[fmt.Sprintf("quota_exceeded:lama-%d", i)] = cooled
	}
	for i := 0; i < activeCount; i++ {
		n.lastSent[fmt.Sprintf("chain_broken:aktif-%d", i)] = active
	}

	n.isCooling("quota_exceeded:pemicu")

	if got := len(n.lastSent); got != activeCount+1 {
		t.Fatalf("tersisa %d entri, harusnya %d", got, activeCount+1)
	}

	// Dan peredamannya harus tetap berlaku: inilah gunanya penampung ini.
	if !n.isCooling("chain_broken:aktif-0") {
		t.Error(
			"insiden yang masih dalam cooldown jadi lolos setelah penyapuan. " +
				"Penyapuan yang membuang entri aktif mengembalikan badai alert " +
				"yang justru dicegah penampung ini: satu insiden berulang akan " +
				"mengirim webhook berkali-kali lagi.",
		)
	}
}

func TestIsCooling_NoSweepBelowThreshold(t *testing.T) {
	const cooldown = time.Minute
	n := newSweepTestNotifier(cooldown)

	cooled := time.Now().Add(-2 * cooldown)
	for i := 0; i < 10; i++ {
		n.lastSent[fmt.Sprintf("quota_exceeded:lama-%d", i)] = cooled
	}

	n.isCooling("quota_exceeded:baru")

	if got := len(n.lastSent); got != 11 {
		t.Errorf("tersisa %d entri, harusnya 11 — tidak ada penyapuan di bawah ambang", got)
	}
}
