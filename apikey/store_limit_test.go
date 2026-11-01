package apikey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRow struct{ scan func(dest ...any) error }

func (r fakeRow) Scan(dest ...any) error { return r.scan(dest...) }

type fakeKeyDB struct {
	active, inserts, revokes, counts int
	racers                           int // key lain yang masuk bersamaan dengan INSERT kita
}

func (f *fakeKeyDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	switch {
	case strings.Contains(sql, "count(*)"):
		f.counts++
		return fakeRow{func(dest ...any) error {
			*(dest[0].(*int)) = f.active
			return nil
		}}
	case strings.Contains(sql, "INSERT INTO api_keys"):
		f.inserts++
		f.active += 1 + f.racers
		return fakeRow{func(dest ...any) error {
			for _, d := range dest {
				switch p := d.(type) {
				case *string:
					*p = "key-new"
				case *time.Time:
					*p = time.Now()
				}
			}
			return nil
		}}
	}
	return fakeRow{func(dest ...any) error { return errors.New("unexpected query") }}
}

func (f *fakeKeyDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "revoked_at") {
		f.revokes++
		f.active--
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.NewCommandTag(""), nil
}

func (f *fakeKeyDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("not used")
}

func newLimitKey() *APIKey {
	return &APIKey{ProjectID: "p1", TenantID: "t1", Name: "k", KeyHash: "h", KeyPrefix: "tsk_", Scope: ScopeRead}
}

func TestCreateWithLimit_BelowLimit(t *testing.T) {
	db := &fakeKeyDB{}
	if err := NewPostgresStore(db).CreateWithLimit(context.Background(), newLimitKey(), 1); err != nil {
		t.Fatalf("seharusnya lolos: %v", err)
	}
	if db.inserts != 1 || db.active != 1 {
		t.Errorf("inserts=%d active=%d", db.inserts, db.active)
	}
}

func TestCreateWithLimit_AtLimitBlocked(t *testing.T) {
	db := &fakeKeyDB{active: 1}
	err := NewPostgresStore(db).CreateWithLimit(context.Background(), newLimitKey(), 1)
	if !errors.Is(err, ErrKeyLimitReached) {
		t.Fatalf("err = %v, want ErrKeyLimitReached", err)
	}
	if db.inserts != 0 {
		t.Errorf("tidak boleh insert, inserts=%d", db.inserts)
	}
}

// Setelah downgrade: tenant punya 3 key aktif, batas 1. Pembuatan baru diblok,
// key lama tidak disentuh.
func TestCreateWithLimit_OverLimitAfterDowngrade(t *testing.T) {
	db := &fakeKeyDB{active: 3}
	err := NewPostgresStore(db).CreateWithLimit(context.Background(), newLimitKey(), 1)
	if !errors.Is(err, ErrKeyLimitReached) {
		t.Fatalf("err = %v", err)
	}
	if db.revokes != 0 || db.active != 3 {
		t.Errorf("key lama tidak boleh berubah: revokes=%d active=%d", db.revokes, db.active)
	}
}

func TestCreateWithLimit_ZeroMeansUnlimited(t *testing.T) {
	db := &fakeKeyDB{active: 50}
	if err := NewPostgresStore(db).CreateWithLimit(context.Background(), newLimitKey(), 0); err != nil {
		t.Fatalf("tanpa batas harus lolos: %v", err)
	}
	if db.counts != 0 {
		t.Errorf("tanpa batas tidak perlu menghitung, counts=%d", db.counts)
	}
}

func TestCreateWithLimit_RaceRevokesNewKey(t *testing.T) {
	db := &fakeKeyDB{racers: 1}
	err := NewPostgresStore(db).CreateWithLimit(context.Background(), newLimitKey(), 1)
	if !errors.Is(err, ErrKeyLimitReached) {
		t.Fatalf("err = %v, want ErrKeyLimitReached", err)
	}
	if db.revokes != 1 {
		t.Errorf("key baru harus di-revoke, revokes=%d", db.revokes)
	}
}
