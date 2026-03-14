package providers

import (
	"testing"
	"time"
)

// ============================================================
// ProviderStats basic recording
// ============================================================

func TestProviderStats_RecordRequest(t *testing.T) {
	s := &ProviderStats{}
	s.RecordRequest(100 * time.Millisecond)
	s.RecordRequest(200 * time.Millisecond)

	if got := s.RequestCount.Load(); got != 2 {
		t.Errorf("RequestCount = %d, want 2", got)
	}
	if got := s.TotalLatencyMs.Load(); got != 300 {
		t.Errorf("TotalLatencyMs = %d, want 300", got)
	}
}

func TestProviderStats_RecordError(t *testing.T) {
	s := &ProviderStats{}
	s.RecordError()
	s.RecordError()

	if got := s.ErrorCount.Load(); got != 2 {
		t.Errorf("ErrorCount = %d, want 2", got)
	}
}

func TestProviderStats_RecordRateLimit(t *testing.T) {
	s := &ProviderStats{}
	s.RecordRateLimit()
	s.RecordRateLimit()
	s.RecordRateLimit()

	if got := s.RateLimitCount.Load(); got != 3 {
		t.Errorf("RateLimitCount = %d, want 3", got)
	}
}

// ============================================================
// AvgLatencyMs
// ============================================================

func TestProviderStats_AvgLatencyMs_NoRequests(t *testing.T) {
	s := &ProviderStats{}
	if got := s.AvgLatencyMs(); got != 0 {
		t.Errorf("AvgLatencyMs = %f, want 0", got)
	}
}

func TestProviderStats_AvgLatencyMs(t *testing.T) {
	s := &ProviderStats{}
	s.RecordRequest(100 * time.Millisecond)
	s.RecordRequest(300 * time.Millisecond)

	avg := s.AvgLatencyMs()
	if avg != 200 {
		t.Errorf("AvgLatencyMs = %f, want 200", avg)
	}
}

// ============================================================
// RecentRateLimits sliding window
// ============================================================

func TestProviderStats_RecentRateLimits(t *testing.T) {
	s := &ProviderStats{}
	s.RecordRateLimit()
	s.RecordRateLimit()
	s.RecordRateLimit()

	// All within the last minute
	count := s.RecentRateLimits(1 * time.Minute)
	if count != 3 {
		t.Errorf("RecentRateLimits(1m) = %d, want 3", count)
	}
}

func TestProviderStats_RecentRateLimits_Expired(t *testing.T) {
	s := &ProviderStats{}
	// Manually add old timestamps
	s.mu.Lock()
	s.recentRateLimits = append(s.recentRateLimits,
		time.Now().Add(-10*time.Minute),
		time.Now().Add(-5*time.Minute),
		time.Now(), // only this one is recent
	)
	s.mu.Unlock()

	count := s.RecentRateLimits(1 * time.Minute)
	if count != 1 {
		t.Errorf("RecentRateLimits(1m) = %d, want 1 (old entries should be pruned)", count)
	}
}

func TestProviderStats_RecentRateLimits_AllExpired(t *testing.T) {
	s := &ProviderStats{}
	s.mu.Lock()
	s.recentRateLimits = append(s.recentRateLimits,
		time.Now().Add(-10*time.Minute),
		time.Now().Add(-5*time.Minute),
	)
	s.mu.Unlock()

	count := s.RecentRateLimits(1 * time.Minute)
	if count != 0 {
		t.Errorf("RecentRateLimits = %d, want 0", count)
	}
}

// ============================================================
// Score
// ============================================================

func TestProviderStats_Score_Clean(t *testing.T) {
	s := &ProviderStats{}
	// Weight=5, no errors, no latency → 5*100/(1+0+0) = 500
	score := s.Score(5, 5*time.Minute)
	if score != 500.0 {
		t.Errorf("Score = %f, want 500", score)
	}
}

func TestProviderStats_Score_WithRateLimits(t *testing.T) {
	s := &ProviderStats{}
	s.RecordRateLimit()
	s.RecordRateLimit()

	// Weight=1, 2 recent 429s → 1*100/(1+20+0) = 100/21 ≈ 4.76
	score := s.Score(1, 5*time.Minute)
	expected := 100.0 / 21.0
	if score < expected-0.1 || score > expected+0.1 {
		t.Errorf("Score = %f, want ≈%f", score, expected)
	}
}

func TestProviderStats_Score_WithLatency(t *testing.T) {
	s := &ProviderStats{}
	s.RecordRequest(500 * time.Millisecond)

	// Weight=1, no 429s, avgLatency=500ms → 1*100/(1+0+5) = 100/6 ≈ 16.67
	score := s.Score(1, 5*time.Minute)
	expected := 100.0 / 6.0
	if score < expected-0.1 || score > expected+0.1 {
		t.Errorf("Score = %f, want ≈%f", score, expected)
	}
}

func TestProviderStats_Score_HigherWeight_BetterScore(t *testing.T) {
	s := &ProviderStats{}
	lowScore := s.Score(1, 5*time.Minute)
	highScore := s.Score(10, 5*time.Minute)

	if highScore <= lowScore {
		t.Errorf("higher weight should produce higher score: %f vs %f", highScore, lowScore)
	}
}

// ============================================================
// Strategy-based selection
// ============================================================

func TestRegistry_RoundRobin(t *testing.T) {
	reg := NewRegistryWithStrategy("a", LBRoundRobin)
	reg.Register(&mockProvider{name: "a", healthy: true})
	reg.Register(&mockProvider{name: "b", healthy: true})

	// Round-robin should cycle through providers
	results := make(map[string]int)
	for i := 0; i < 10; i++ {
		p, ok := reg.Resolve("any")
		if !ok {
			t.Fatal("should resolve")
		}
		results[p.Name()]++
	}
	// Both should be selected
	if results["a"] == 0 || results["b"] == 0 {
		t.Errorf("round-robin should select both providers, got %v", results)
	}
}

func TestRegistry_LeastUsed(t *testing.T) {
	reg := NewRegistryWithStrategy("a", LBLeastUsed)
	reg.Register(&mockProvider{name: "a", healthy: true})
	reg.Register(&mockProvider{name: "b", healthy: true})

	// Simulate: a has 10 requests, b has 0
	statsA := reg.GetStats("a")
	for i := 0; i < 10; i++ {
		statsA.RecordRequest(10 * time.Millisecond)
	}

	// Should pick b (least used)
	p, ok := reg.Resolve("any")
	if !ok {
		t.Fatal("should resolve")
	}
	if p.Name() != "b" {
		t.Errorf("least-used should pick b (0 requests), got %s", p.Name())
	}
}

func TestRegistry_Priority(t *testing.T) {
	reg := NewRegistryWithStrategy("low", LBPriority)
	reg.RegisterWithConfig(&mockProvider{name: "low", healthy: true}, 1, nil)
	reg.RegisterWithConfig(&mockProvider{name: "high", healthy: true}, 10, nil)

	// Should always pick highest weight
	for i := 0; i < 20; i++ {
		p, ok := reg.Resolve("any")
		if !ok {
			t.Fatal("should resolve")
		}
		if p.Name() != "high" {
			t.Errorf("priority should pick highest weight, got %s", p.Name())
		}
	}
}

func TestRegistry_Smart_Prefers_Clean(t *testing.T) {
	reg := NewRegistryWithStrategy("a", LBSmart)
	reg.RegisterWithConfig(&mockProvider{name: "clean", healthy: true}, 5, nil)
	reg.RegisterWithConfig(&mockProvider{name: "dirty", healthy: true}, 5, nil)

	// dirty has many 429s
	dirtyStats := reg.GetStats("dirty")
	for i := 0; i < 10; i++ {
		dirtyStats.RecordRateLimit()
	}

	// clean has no errors
	p, ok := reg.Resolve("any")
	if !ok {
		t.Fatal("should resolve")
	}
	if p.Name() != "clean" {
		t.Errorf("smart should prefer clean provider, got %s", p.Name())
	}
}

func TestRegistry_Smart_WeightMatters(t *testing.T) {
	reg := NewRegistryWithStrategy("a", LBSmart)
	reg.RegisterWithConfig(&mockProvider{name: "heavy", healthy: true}, 100, nil)
	reg.RegisterWithConfig(&mockProvider{name: "light", healthy: true}, 1, nil)

	// Both clean — heavier weight should win
	p, ok := reg.Resolve("any")
	if !ok {
		t.Fatal("should resolve")
	}
	if p.Name() != "heavy" {
		t.Errorf("smart should prefer heavier weight when both clean, got %s", p.Name())
	}
}

func TestRegistry_GetStats(t *testing.T) {
	reg := NewRegistry("p")
	reg.Register(&mockProvider{name: "p", healthy: true})

	stats := reg.GetStats("p")
	if stats == nil {
		t.Fatal("stats should not be nil")
	}
	stats.RecordRequest(50 * time.Millisecond)
	if stats.RequestCount.Load() != 1 {
		t.Error("stats should be shared reference")
	}
}

func TestRegistry_GetStats_NotFound(t *testing.T) {
	reg := NewRegistry("p")
	stats := reg.GetStats("nonexistent")
	if stats != nil {
		t.Error("should return nil for unknown provider")
	}
}

func TestRegistry_Strategy(t *testing.T) {
	reg := NewRegistryWithStrategy("p", LBSmart)
	if reg.Strategy() != LBSmart {
		t.Errorf("Strategy() = %s, want smart", reg.Strategy())
	}
}

func TestNewRegistryWithStrategy_Default(t *testing.T) {
	reg := NewRegistryWithStrategy("p", LBWeightedRandom)
	if reg.Strategy() != LBWeightedRandom {
		t.Errorf("Strategy() = %s, want weighted", reg.Strategy())
	}
}
