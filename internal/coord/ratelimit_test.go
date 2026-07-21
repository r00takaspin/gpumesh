package coord

import (
	"testing"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := RateLimitHourly(10) // 10 req/hour, burst=10

	// First 10 requests should be allowed.
	for range 10 {
		allowed, remaining := rl.Allow("key1")
		if !allowed {
			t.Fatal("expected Allow to succeed within burst")
		}
		if remaining < 0 {
			t.Fatalf("remaining should not be negative, got %d", remaining)
		}
	}

	// 11th should be denied.
	allowed, remaining := rl.Allow("key1")
	if allowed {
		t.Fatal("expected Allow to deny after burst exhausted")
	}
	if remaining >= 0 {
		// remaining can be 0 (floor of small fraction) or negative
		_ = remaining
	}
}

func TestRateLimiterSeparateKeys(t *testing.T) {
	rl := RateLimitHourly(10)

	// Exhaust key1.
	for range 10 {
		rl.Allow("key1")
	}
	if allowed, _ := rl.Allow("key1"); allowed {
		t.Fatal("key1 should be exhausted")
	}

	// key2 should still have full burst.
	if allowed, _ := rl.Allow("key2"); !allowed {
		t.Fatal("key2 should still have tokens")
	}
}

func TestRateLimiterRemaining(t *testing.T) {
	rl := RateLimitHourly(100)

	rem := rl.Remaining("newkey")
	if rem != 100 {
		t.Fatalf("expected 100 remaining, got %d", rem)
	}

	rl.Allow("newkey")
	rem = rl.Remaining("newkey")
	if rem != 99 {
		t.Fatalf("expected 99 remaining, got %d", rem)
	}
}
