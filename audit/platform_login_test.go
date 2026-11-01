package audit

import (
	"testing"
	"time"
)

func TestPlatformThrottle_PerIP(t *testing.T) {
	th := &platformThrottleState{perIP: make(map[string]time.Time)}
	now := time.Now()

	if !th.allow("1.1.1.1", now) {
		t.Fatal("first attempt should be allowed")
	}
	if th.allow("1.1.1.1", now.Add(time.Minute)) {
		t.Fatal("same IP inside window should be throttled")
	}
	if !th.allow("2.2.2.2", now.Add(time.Minute)) {
		t.Fatal("different IP should be allowed")
	}
	if !th.allow("1.1.1.1", now.Add(platformPerIPWindow+time.Second)) {
		t.Fatal("same IP after window should be allowed")
	}
}

func TestPlatformThrottle_GlobalCap(t *testing.T) {
	th := &platformThrottleState{perIP: make(map[string]time.Time)}
	now := time.Now()

	for i := 0; i < platformGlobalMax; i++ {
		ip := "10.0.0." + string(rune('a'+i%26)) + string(rune('a'+i/26))
		if !th.allow(ip, now) {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}
	if th.allow("9.9.9.9", now) {
		t.Fatal("global cap should block further entries")
	}
}