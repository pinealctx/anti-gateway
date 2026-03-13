package providers

import (
	"sync"
	"sync/atomic"
	"time"
)

// LBStrategy defines the load balancing strategy.
type LBStrategy string

const (
	LBWeightedRandom LBStrategy = "weighted"    // Default: weighted random selection
	LBRoundRobin     LBStrategy = "round-robin" // Simple round-robin
	LBLeastUsed      LBStrategy = "least-used"  // Route to least-used provider
	LBPriority       LBStrategy = "priority"    // Use highest-weight first, failover on error
	LBSmart          LBStrategy = "smart"       // 429-aware scoring: combines weight + recent errors + latency
)

// ProviderStats tracks per-provider runtime statistics for smart load balancing.
type ProviderStats struct {
	RequestCount   atomic.Int64
	ErrorCount     atomic.Int64
	RateLimitCount atomic.Int64 // 429 errors specifically
	TotalLatencyMs atomic.Int64 // cumulative latency in ms

	// Sliding window for recent 429s
	recentRateLimits []time.Time
	mu               sync.Mutex
}

// RecordRequest records a successful request with latency.
func (s *ProviderStats) RecordRequest(latency time.Duration) {
	s.RequestCount.Add(1)
	s.TotalLatencyMs.Add(latency.Milliseconds())
}

// RecordError records an error (non-429).
func (s *ProviderStats) RecordError() {
	s.ErrorCount.Add(1)
}

// RecordRateLimit records a 429 error.
func (s *ProviderStats) RecordRateLimit() {
	s.RateLimitCount.Add(1)
	s.mu.Lock()
	s.recentRateLimits = append(s.recentRateLimits, time.Now())
	s.mu.Unlock()
}

// RecentRateLimits returns the count of 429s in the last window.
func (s *ProviderStats) RecentRateLimits(window time.Duration) int {
	cutoff := time.Now().Add(-window)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prune old entries
	kept := s.recentRateLimits[:0]
	for _, t := range s.recentRateLimits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	s.recentRateLimits = kept
	return len(kept)
}

// AvgLatencyMs returns average latency in milliseconds (0 if no requests).
func (s *ProviderStats) AvgLatencyMs() float64 {
	count := s.RequestCount.Load()
	if count == 0 {
		return 0
	}
	return float64(s.TotalLatencyMs.Load()) / float64(count)
}

// Score computes a smart score: higher is better.
// Formula: weight * 100 / (1 + recent429s*10 + avgLatencyMs/100)
func (s *ProviderStats) Score(weight int, window time.Duration) float64 {
	recent429 := float64(s.RecentRateLimits(window))
	avgLat := s.AvgLatencyMs()
	return float64(weight) * 100.0 / (1.0 + recent429*10.0 + avgLat/100.0)
}
