package main

import (
	"context"
	"testing"

	"trailseal/apikey"
)

type fakeChecker struct {
	allowed   bool
	called    bool
	gotLimit  int
	gotTenant string
}

func (f *fakeChecker) IsAmongOldestActive(_ context.Context, tenantID, _ string, limit int) (bool, error) {
	f.called = true
	f.gotLimit = limit
	f.gotTenant = tenantID
	return f.allowed, nil
}

func TestAPIKeyPlanGate_UnlimitedSkipsCheck(t *testing.T) {
	fc := &fakeChecker{allowed: false}
	gate := newAPIKeyPlanGate(fc, func(string) int { return 0 })
	ok, err := gate(context.Background(), &apikey.APIKey{ID: "k1", TenantID: "t1"})
	if err != nil || !ok {
		t.Fatalf("plan tanpa batas harus lolos, got ok=%v err=%v", ok, err)
	}
	if fc.called {
		t.Fatal("plan tanpa batas tidak boleh query store")
	}
}

func TestAPIKeyPlanGate_FreeSuspendsNewerKey(t *testing.T) {
	fc := &fakeChecker{allowed: false}
	gate := newAPIKeyPlanGate(fc, func(string) int { return 1 })
	ok, err := gate(context.Background(), &apikey.APIKey{ID: "k2", TenantID: "t1"})
	if err != nil || ok {
		t.Fatalf("key bukan yang terlama harus ditolak, got ok=%v err=%v", ok, err)
	}
	if fc.gotLimit != 1 || fc.gotTenant != "t1" {
		t.Fatalf("argumen salah: limit=%d tenant=%s", fc.gotLimit, fc.gotTenant)
	}
}

func TestAPIKeyPlanGate_FreeAllowsOldestKey(t *testing.T) {
	fc := &fakeChecker{allowed: true}
	gate := newAPIKeyPlanGate(fc, func(string) int { return 1 })
	ok, err := gate(context.Background(), &apikey.APIKey{ID: "k1", TenantID: "t1"})
	if err != nil || !ok {
		t.Fatalf("key terlama harus lolos, got ok=%v err=%v", ok, err)
	}
}

func TestAPIKeyPlanGate_UpgradeRestores(t *testing.T) {
	fc := &fakeChecker{allowed: false}
	limit := 1
	gate := newAPIKeyPlanGate(fc, func(string) int { return limit })
	key := &apikey.APIKey{ID: "k2", TenantID: "t1"}
	if ok, _ := gate(context.Background(), key); ok {
		t.Fatal("harus ditangguhkan saat Free")
	}
	limit = 0 // upgrade ke Pro
	if ok, _ := gate(context.Background(), key); !ok {
		t.Fatal("harus pulih setelah upgrade")
	}
}
