package coord

import (
	"math"
	"sync"
	"time"
)

// RateLimiter implements a per-key token bucket rate limiter.
type RateLimiter struct {
	mu              sync.Mutex
	buckets         map[string]*tokenBucket
	rate            float64 // tokens per second
	burst           float64 // max bucket size
	cleanupInterval time.Duration
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
}

// RateLimitHourly creates a limiter that allows `perHour` requests per hour.
// Each call to Allow consumes one token; tokens refill at rate perHour/3600 per second.
// Burst size equals the full hourly rate (i.e., a cold start can use all tokens at once).
func RateLimitHourly(perHour int) *RateLimiter {
	rate := float64(perHour) / 3600.0
	rl := &RateLimiter{
		buckets:         make(map[string]*tokenBucket),
		rate:            rate,
		burst:           float64(perHour),
		cleanupInterval: 10 * time.Minute,
	}
	go rl.cleanup()
	return rl
}

// Allow consumes one token for the given key. Returns (allowed, remaining).
// Remaining is the integer number of full tokens left in the bucket.
func (rl *RateLimiter) Allow(key string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: rl.burst, lastTime: now}
		rl.buckets[key] = b
	}

	// Refill.
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastTime = now

	if b.tokens < 1.0 {
		return false, int(math.Floor(b.tokens))
	}
	b.tokens--
	return true, int(math.Floor(b.tokens))
}

// Remaining returns the number of remaining tokens for a key without consuming.
func (rl *RateLimiter) Remaining(key string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		return int(rl.burst)
	}
	elapsed := time.Since(b.lastTime).Seconds()
	tokens := b.tokens + elapsed*rl.rate
	if tokens > rl.burst {
		tokens = rl.burst
	}
	return int(math.Floor(tokens))
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		threshold := rl.burst * 0.9 // keep buckets near full capacity
		for k, b := range rl.buckets {
			elapsed := time.Since(b.lastTime).Seconds()
			tokens := b.tokens + elapsed*rl.rate
			if tokens >= threshold {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}
