package auth

import (
	"sync"
	"testing"
	"time"
)

func TestOAuthState_GenerateAndConsume_Success(t *testing.T) {
	s := NewOAuthStateStore()
	state, err := s.Generate("github")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if !s.Consume("github", state) {
		t.Fatal("expected Consume to succeed on valid state")
	}
}

// SECURITY: state adalah pertahanan CSRF utama — harus single-use.
func TestOAuthState_Consume_IsSingleUse_ReplayRejected(t *testing.T) {
	s := NewOAuthStateStore()
	state, _ := s.Generate("github")

	if !s.Consume("github", state) {
		t.Fatal("first consume should succeed")
	}
	if s.Consume("github", state) {
		t.Fatal("SECURITY: replayed state must be rejected on second use")
	}
}

// SECURITY: state yang diterbitkan untuk provider A tidak boleh valid
// dipakai pada callback provider B (mencegah cross-provider confusion).
func TestOAuthState_ProviderMismatch_Rejected(t *testing.T) {
	s := NewOAuthStateStore()
	state, _ := s.Generate("github")

	if s.Consume("google", state) {
		t.Fatal("SECURITY: state generated for github must not validate for google")
	}
}

func TestOAuthState_UnknownState_Rejected(t *testing.T) {
	s := NewOAuthStateStore()
	if s.Consume("github", "never-generated-state") {
		t.Fatal("unknown state must be rejected")
	}
}

// SECURITY: state kadaluarsa tidak boleh diterima walau formatnya benar
// dan belum pernah dipakai — mencegah replay dari link lama/log lama.
func TestOAuthState_Expired_Rejected(t *testing.T) {
	s := NewOAuthStateStore()
	s.ttl = 1 * time.Millisecond
	state, _ := s.Generate("github")

	time.Sleep(5 * time.Millisecond)

	if s.Consume("github", state) {
		t.Fatal("SECURITY: expired state must be rejected")
	}
}

func TestOAuthState_TwoStates_AreUnique(t *testing.T) {
	s := NewOAuthStateStore()
	a, _ := s.Generate("github")
	b, _ := s.Generate("github")
	if a == b {
		t.Fatal("SECURITY: two generated states must not collide")
	}
}

// Concurrency: Generate/Consume dipakai lintas goroutine request HTTP.
func TestOAuthState_ConcurrentAccess_NoRace(t *testing.T) {
	s := NewOAuthStateStore()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := s.Generate("github")
			if err != nil {
				t.Errorf("generate failed: %v", err)
				return
			}
			s.Consume("github", state)
		}()
	}
	wg.Wait()
}
