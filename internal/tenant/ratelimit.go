package tenant

import (
	"sync"
	"time"
)

// RateLimiter provides per-key QPM and TPM rate limiting using sliding window counters.
type RateLimiter struct {
	mu      sync.Mutex
	windows map[string]*keyWindow
}

type keyWindow struct {
	requests []time.Time
	tokens   []tokenEntry
}

type tokenEntry struct {
	time   time.Time
	tokens int
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		windows: make(map[string]*keyWindow),
	}
}

// AllowRequest checks if a request is allowed under QPM limit.
// Returns true if allowed, false if rate limited.
func (rl *RateLimiter) AllowRequest(keyID string, qpm int) bool {
	if qpm <= 0 {
		return true // No limit
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	w := rl.getOrCreate(keyID)
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	// Prune old entries
	w.requests = pruneTime(w.requests, cutoff)

	if len(w.requests) >= qpm {
		return false
	}

	w.requests = append(w.requests, now)
	return true
}

// AllowTokens checks if additional tokens are allowed under TPM limit.
// Returns true if allowed, false if rate limited.
func (rl *RateLimiter) AllowTokens(keyID string, tpm int, count int) bool {
	if tpm <= 0 {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	w := rl.getOrCreate(keyID)
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	// Prune old entries
	w.tokens = pruneTokens(w.tokens, cutoff)

	total := count
	for _, t := range w.tokens {
		total += t.tokens
	}

	if total > tpm {
		return false
	}

	w.tokens = append(w.tokens, tokenEntry{time: now, tokens: count})
	return true
}

// RecordTokens records token usage for TPM tracking (post-response).
func (rl *RateLimiter) RecordTokens(keyID string, count int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	w := rl.getOrCreate(keyID)
	w.tokens = append(w.tokens, tokenEntry{time: time.Now(), tokens: count})
}

// RetryAfter returns the number of seconds until the next request is allowed.
func (rl *RateLimiter) RetryAfter(keyID string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	w, ok := rl.windows[keyID]
	if !ok || len(w.requests) == 0 {
		return 0
	}

	oldest := w.requests[0]
	until := time.Until(oldest.Add(time.Minute))
	if until <= 0 {
		return 0
	}
	return int(until.Seconds()) + 1
}

func (rl *RateLimiter) getOrCreate(keyID string) *keyWindow {
	w, ok := rl.windows[keyID]
	if !ok {
		w = &keyWindow{}
		rl.windows[keyID] = w
	}
	return w
}

func pruneTime(entries []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(entries) && entries[i].Before(cutoff) {
		i++
	}
	return entries[i:]
}

func pruneTokens(entries []tokenEntry, cutoff time.Time) []tokenEntry {
	i := 0
	for i < len(entries) && entries[i].time.Before(cutoff) {
		i++
	}
	return entries[i:]
}
