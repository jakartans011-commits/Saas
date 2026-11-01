package auth

import "testing"

func TestLoginAttemptLimiterBruteForce(t *testing.T) {
	limiter := NewLoginAttemptLimiter()

	ip := "10.0.0.1"
	email := "victim@example.com"

	for i := 0; i < LoginMaxFailures; i++ {
		if !limiter.Allow(ip, email) {
			t.Fatalf(
				"attempt %d should still be allowed",
				i+1,
			)
		}

		limiter.RecordFailure(ip, email)
	}

	if limiter.Allow(ip, email) {
		t.Fatal(
			"SECURITY FAILURE: brute-force attempt was still allowed after maximum failures",
		)
	}

	t.Log("OK: brute-force attempts blocked after maximum failures")
}

func TestLoginAttemptLimiterSeparatesAccounts(t *testing.T) {
	limiter := NewLoginAttemptLimiter()

	ip := "10.0.0.2"

	for i := 0; i < LoginMaxFailures; i++ {
		limiter.RecordFailure(ip, "victim1@example.com")
	}

	if limiter.Allow(ip, "victim1@example.com") {
		t.Fatal("expected victim1 to be rate limited")
	}

	if !limiter.Allow(ip, "victim2@example.com") {
		t.Fatal(
			"SECURITY FAILURE: rate limit for one account blocked another account",
		)
	}

	t.Log("OK: limiter isolates accounts")
}

func TestLoginAttemptLimiterResetAfterSuccess(t *testing.T) {
	limiter := NewLoginAttemptLimiter()

	ip := "10.0.0.3"
	email := "user@example.com"

	for i := 0; i < LoginMaxFailures; i++ {
		limiter.RecordFailure(ip, email)
	}

	if limiter.Allow(ip, email) {
		t.Fatal("expected account to be blocked before reset")
	}

	limiter.Reset(ip, email)

	if !limiter.Allow(ip, email) {
		t.Fatal(
			"SECURITY FAILURE: successful login did not reset failed attempts",
		)
	}

	t.Log("OK: successful login resets failed-attempt counter")
}
