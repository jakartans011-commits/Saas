package auth

import "testing"

func TestCredentialStuffingLimiter(t *testing.T) {
	limiter := NewCredentialStuffingLimiter()

	ip := "10.10.10.10"

	for i := 0; i < CredentialStuffingMaxAttempts; i++ {
		if !limiter.Allow(ip) {
			t.Fatalf("attempt %d unexpectedly blocked", i+1)
		}
		limiter.RecordAttempt(ip)
	}

	if limiter.Allow(ip) {
		t.Fatal("expected IP-wide credential stuffing protection to block")
	}

	t.Log("OK: credential stuffing attempts across multiple accounts are blocked")
}

func TestCredentialStuffingLimiterSeparatesIPs(t *testing.T) {
	limiter := NewCredentialStuffingLimiter()

	ip1 := "10.10.10.10"
	ip2 := "10.10.10.11"

	for i := 0; i < CredentialStuffingMaxAttempts; i++ {
		limiter.RecordAttempt(ip1)
	}

	if limiter.Allow(ip1) {
		t.Fatal("expected exhausted IP to be blocked")
	}

	if !limiter.Allow(ip2) {
		t.Fatal("expected separate IP to remain allowed")
	}

	t.Log("OK: credential stuffing limiter isolates IPs")
}

func TestAccountLockout(t *testing.T) {
	lockout := NewAccountLockout()

	email := "victim@example.com"

	for i := 0; i < AccountLockoutFailures-1; i++ {
		lockout.RecordFailure(email)

		if lockout.IsLocked(email) {
			t.Fatalf("account locked too early at failure %d", i+1)
		}
	}

	lockout.RecordFailure(email)

	if !lockout.IsLocked(email) {
		t.Fatal("expected account to be locked after threshold")
	}

	t.Log("OK: account lockout activates after maximum failures")
}

func TestAccountLockoutSeparatesAccounts(t *testing.T) {
	lockout := NewAccountLockout()

	victim := "victim@example.com"
	other := "other@example.com"

	for i := 0; i < AccountLockoutFailures; i++ {
		lockout.RecordFailure(victim)
	}

	if !lockout.IsLocked(victim) {
		t.Fatal("expected victim account to be locked")
	}

	if lockout.IsLocked(other) {
		t.Fatal("other account must not be locked")
	}

	t.Log("OK: account lockout isolates accounts")
}

func TestAccountLockoutResetAfterSuccess(t *testing.T) {
	lockout := NewAccountLockout()

	email := "victim@example.com"

	for i := 0; i < AccountLockoutFailures-1; i++ {
		lockout.RecordFailure(email)
	}

	lockout.Reset(email)

	if lockout.IsLocked(email) {
		t.Fatal("account should not remain locked after reset")
	}

	lockout.RecordFailure(email)

	if lockout.IsLocked(email) {
		t.Fatal("counter was not reset after successful login")
	}

	t.Log("OK: successful login resets account lockout state")
}

func TestSessionRefreshLimiter(t *testing.T) {
	limiter := NewSessionRefreshLimiter()

	ip := "10.20.30.40"
	userID := "user-123"

	for i := 0; i < SessionRefreshMaxAttempts; i++ {
		if !limiter.Allow(ip, userID) {
			t.Fatalf("refresh attempt %d unexpectedly blocked", i+1)
		}

		limiter.Record(ip, userID)
	}

	if limiter.Allow(ip, userID) {
		t.Fatal("expected excessive refresh activity to be blocked")
	}

	t.Log("OK: session refresh abuse is throttled")
}

func TestSessionRefreshLimiterSeparatesSessions(t *testing.T) {
	limiter := NewSessionRefreshLimiter()

	ip := "10.20.30.40"

	for i := 0; i < SessionRefreshMaxAttempts; i++ {
		limiter.Record(ip, "user-a")
	}

	if limiter.Allow(ip, "user-a") {
		t.Fatal("user-a should be throttled")
	}

	if !limiter.Allow(ip, "user-b") {
		t.Fatal("user-b should not inherit user-a's refresh limit")
	}

	t.Log("OK: refresh limiter isolates user sessions")
}
