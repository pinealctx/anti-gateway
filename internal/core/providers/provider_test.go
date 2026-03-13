package providers

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SilkageNet/anti-gateway/internal/models"
)

// ============================================================
// Mock provider for registry tests
// ============================================================

type mockProvider struct {
	name    string
	healthy bool
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) ChatCompletion(_ context.Context, _ *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
	return &models.ChatCompletionResponse{}, nil
}
func (m *mockProvider) StreamCompletion(_ context.Context, _ *models.ChatCompletionRequest, stream chan<- StreamChunk) error {
	close(stream)
	return nil
}
func (m *mockProvider) RefreshToken(_ context.Context) error { return nil }
func (m *mockProvider) IsHealthy(_ context.Context) bool     { return m.healthy }

// ============================================================
// Registry.Register + Get
// ============================================================

func TestRegistry_Register_Get(t *testing.T) {
	reg := NewRegistry("default")
	p := &mockProvider{name: "test", healthy: true}
	reg.Register(p)

	got, ok := reg.Get("test")
	if !ok || got.Name() != "test" {
		t.Errorf("Get('test') failed")
	}
}

func TestRegistry_Get_Fallback(t *testing.T) {
	reg := NewRegistry("fallback")
	reg.Register(&mockProvider{name: "fallback", healthy: true})

	got, ok := reg.Get("nonexistent")
	if !ok || got.Name() != "fallback" {
		t.Errorf("should fall back, got ok=%v name=%v", ok, got)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	reg := NewRegistry("missing")
	_, ok := reg.Get("anything")
	if ok {
		t.Error("should return false when no providers")
	}
}

// ============================================================
// Registry.Resolve (model routing)
// ============================================================

func TestResolve_SingleProvider_AnyModel(t *testing.T) {
	reg := NewRegistry("p1")
	reg.Register(&mockProvider{name: "p1", healthy: true})

	got, ok := reg.Resolve("gpt-4")
	if !ok || got.Name() != "p1" {
		t.Error("single provider should handle any model")
	}
}

func TestResolve_ModelSpecific(t *testing.T) {
	reg := NewRegistry("fallback")
	reg.RegisterWithConfig(&mockProvider{name: "openai", healthy: true}, 1, []string{"gpt-4", "gpt-4o"})
	reg.RegisterWithConfig(&mockProvider{name: "kiro", healthy: true}, 1, []string{"claude-opus-4.6"})

	got, ok := reg.Resolve("gpt-4")
	if !ok || got.Name() != "openai" {
		t.Errorf("gpt-4 should route to openai, got %v", got.Name())
	}

	got2, ok := reg.Resolve("claude-opus-4.6")
	if !ok || got2.Name() != "kiro" {
		t.Errorf("claude should route to kiro, got %v", got2.Name())
	}
}

func TestResolve_UnknownModel_FallsToWildcard(t *testing.T) {
	reg := NewRegistry("wildcard")
	reg.RegisterWithConfig(&mockProvider{name: "specific", healthy: true}, 1, []string{"gpt-4"})
	reg.RegisterWithConfig(&mockProvider{name: "wildcard", healthy: true}, 1, nil) // handles all

	got, ok := reg.Resolve("unknown-model")
	if !ok || got.Name() != "wildcard" {
		t.Errorf("unknown model should fall to wildcard, got %v", got.Name())
	}
}

func TestResolve_UnhealthyProvider_Excluded(t *testing.T) {
	reg := NewRegistry("fallback")
	reg.RegisterWithConfig(&mockProvider{name: "sick", healthy: false}, 1, nil)
	reg.RegisterWithConfig(&mockProvider{name: "healthy", healthy: true}, 1, nil)
	reg.SetHealthy("sick", false)

	// Multiple calls should never return sick provider
	for i := 0; i < 20; i++ {
		got, ok := reg.Resolve("any")
		if !ok {
			t.Fatal("should resolve")
		}
		if got.Name() == "sick" {
			t.Fatal("should not route to unhealthy provider")
		}
	}
}

func TestResolve_AllUnhealthy_ReturnsNotFound(t *testing.T) {
	reg := NewRegistry("fb")
	reg.Register(&mockProvider{name: "fb", healthy: true})
	reg.SetHealthy("fb", false)

	// When all providers (including fallback) are unhealthy, Resolve returns false
	_, ok := reg.Resolve("any")
	if ok {
		t.Error("all-unhealthy scenario should return false")
	}
}

// ============================================================
// Weighted selection distribution
// ============================================================

func TestResolve_WeightedDistribution(t *testing.T) {
	reg := NewRegistry("a")
	reg.RegisterWithConfig(&mockProvider{name: "heavy", healthy: true}, 9, nil)
	reg.RegisterWithConfig(&mockProvider{name: "light", healthy: true}, 1, nil)

	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		p, _ := reg.Resolve("any")
		counts[p.Name()]++
	}

	// heavy should get ~90% (allow 75-98% range)
	heavyPct := float64(counts["heavy"]) / 1000.0
	if heavyPct < 0.75 || heavyPct > 0.98 {
		t.Errorf("heavy got %.0f%%, expected ~90%%", heavyPct*100)
	}
}

// ============================================================
// Health tracking
// ============================================================

func TestSetHealthy(t *testing.T) {
	reg := NewRegistry("p")
	reg.Register(&mockProvider{name: "p", healthy: true})

	if !reg.IsHealthy("p") {
		t.Error("should be healthy initially")
	}

	reg.SetHealthy("p", false)
	if reg.IsHealthy("p") {
		t.Error("should be unhealthy after SetHealthy(false)")
	}

	reg.SetHealthy("p", true)
	if !reg.IsHealthy("p") {
		t.Error("should be healthy after SetHealthy(true)")
	}
}

func TestIsHealthy_Unknown(t *testing.T) {
	reg := NewRegistry("x")
	if reg.IsHealthy("nonexistent") {
		t.Error("nonexistent provider should report unhealthy")
	}
}

// ============================================================
// All
// ============================================================

func TestAll(t *testing.T) {
	reg := NewRegistry("a")
	reg.Register(&mockProvider{name: "a", healthy: true})
	reg.Register(&mockProvider{name: "b", healthy: true})

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("expected 2, got %d", len(all))
	}
}

// ============================================================
// Concurrency safety
// ============================================================

func TestRegistry_Concurrent(t *testing.T) {
	reg := NewRegistry("p")
	reg.Register(&mockProvider{name: "p", healthy: true})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			reg.Resolve("model")
		}()
		go func() {
			defer wg.Done()
			reg.SetHealthy("p", true)
		}()
		go func() {
			defer wg.Done()
			reg.All()
		}()
	}
	wg.Wait()
}

// ============================================================
// StartHealthCheck
// ============================================================

func TestStartHealthCheck_UpdatesStatus(t *testing.T) {
	mp := &mockProvider{name: "p", healthy: false}
	reg := NewRegistry("p")
	reg.Register(mp)
	reg.SetHealthy("p", false)

	// Now make the provider healthy
	mp.healthy = true
	reg.StartHealthCheck(50 * time.Millisecond)

	// Wait for at least one health check cycle
	time.Sleep(150 * time.Millisecond)

	if !reg.IsHealthy("p") {
		t.Error("health check should have detected provider is now healthy")
	}
}

// ============================================================
// RegisterWithConfig details
// ============================================================

func TestRegisterWithConfig_MinWeight(t *testing.T) {
	reg := NewRegistry("p")
	reg.RegisterWithConfig(&mockProvider{name: "p", healthy: true}, 0, nil) // weight < 1

	// Should still be registered and resolvable
	_, ok := reg.Resolve("any")
	if !ok {
		t.Error("weight=0 should be clamped to 1")
	}
}

func TestRegisterWithConfig_EmptyModels_HandlesAll(t *testing.T) {
	reg := NewRegistry("p")
	reg.RegisterWithConfig(&mockProvider{name: "p", healthy: true}, 1, nil)

	for _, model := range []string{"gpt-4", "claude-opus-4.6", "anything"} {
		_, ok := reg.Resolve(model)
		if !ok {
			t.Errorf("empty model list should handle %q", model)
		}
	}
}

func TestResolve_ModelSpecific_NoFalsePositive(t *testing.T) {
	reg := NewRegistry("")
	reg.RegisterWithConfig(&mockProvider{name: "only-gpt", healthy: true}, 1, []string{"gpt-4"})

	// This model is not listed and there's no wildcard/fallback
	_, ok := reg.Resolve("claude-opus-4.6")
	if ok {
		t.Error("should not resolve a model not in provider's list without fallback")
	}
}

// ============================================================
// ParseModelPrefix
// ============================================================

func TestParseModelPrefix_WithPrefix(t *testing.T) {
	hint, model := ParseModelPrefix("openai/gpt-4o")
	if hint != "openai" || model != "gpt-4o" {
		t.Errorf("got (%q, %q), want (openai, gpt-4o)", hint, model)
	}
}

func TestParseModelPrefix_NoPrefix(t *testing.T) {
	hint, model := ParseModelPrefix("claude-sonnet-4")
	if hint != "" || model != "claude-sonnet-4" {
		t.Errorf("got (%q, %q), want (\"\", claude-sonnet-4)", hint, model)
	}
}

func TestParseModelPrefix_TrailingSlash(t *testing.T) {
	// "model/" — slash at end, no model part → no prefix
	hint, model := ParseModelPrefix("openai/")
	if hint != "" || model != "openai/" {
		t.Errorf("got (%q, %q), want (\"\", openai/)", hint, model)
	}
}

func TestParseModelPrefix_LeadingSlash(t *testing.T) {
	// "/gpt-4" — slash at start → no prefix
	hint, model := ParseModelPrefix("/gpt-4")
	if hint != "" || model != "/gpt-4" {
		t.Errorf("got (%q, %q), want (\"\", /gpt-4)", hint, model)
	}
}

func TestParseModelPrefix_MultiSlash(t *testing.T) {
	// "azure/openai/gpt-4" → first slash splits: "azure", "openai/gpt-4"
	hint, model := ParseModelPrefix("azure/openai/gpt-4")
	if hint != "azure" || model != "openai/gpt-4" {
		t.Errorf("got (%q, %q), want (azure, openai/gpt-4)", hint, model)
	}
}

// ============================================================
// ResolveWithHint
// ============================================================

func TestResolveWithHint_PrefixOverridesHint(t *testing.T) {
	reg := NewRegistry("")
	reg.Register(&mockProvider{name: "openai", healthy: true})
	reg.Register(&mockProvider{name: "kiro", healthy: true})

	// Prefix says "openai", hint says "kiro" → prefix wins
	p, ok := reg.ResolveWithHint("openai/gpt-4o", "kiro")
	if !ok || p.Name() != "openai" {
		t.Errorf("prefix should override hint, got %v", p)
	}
}

func TestResolveWithHint_HintOnly(t *testing.T) {
	reg := NewRegistry("")
	reg.Register(&mockProvider{name: "openai", healthy: true})
	reg.Register(&mockProvider{name: "kiro", healthy: true})

	// No prefix, hint = "kiro"
	p, ok := reg.ResolveWithHint("gpt-4o", "kiro")
	if !ok || p.Name() != "kiro" {
		t.Errorf("hint should select kiro, got %v", p)
	}
}

func TestResolveWithHint_HintUnhealthy_FallsThrough(t *testing.T) {
	reg := NewRegistry("")
	reg.RegisterWithConfig(&mockProvider{name: "openai", healthy: false}, 1, nil)
	reg.RegisterWithConfig(&mockProvider{name: "kiro", healthy: true}, 1, nil)
	reg.SetHealthy("openai", false) // Mark entry unhealthy in registry

	// Hint provider is unhealthy → falls through to weighted selection
	p, ok := reg.ResolveWithHint("gpt-4o", "openai")
	if !ok || p.Name() != "kiro" {
		t.Errorf("should fall through to kiro when hint is unhealthy, got %v", p)
	}
}

func TestResolveWithHint_NoHint_UsesWeighted(t *testing.T) {
	reg := NewRegistry("")
	reg.RegisterWithConfig(&mockProvider{name: "a", healthy: true}, 1, nil)

	// No prefix, no hint → normal weighted behavior
	p, ok := reg.ResolveWithHint("any-model", "")
	if !ok || p.Name() != "a" {
		t.Errorf("should use weighted selection, got %v", p)
	}
}

func TestResolveWithHint_PrefixStripped_ModelClean(t *testing.T) {
	reg := NewRegistry("")
	reg.RegisterWithConfig(&mockProvider{name: "openai", healthy: true}, 1, []string{"gpt-4o"})

	// "openai/gpt-4o" → prefix "openai", model "gpt-4o" → since model filter checks "gpt-4o", FindByHint matches
	p, ok := reg.ResolveWithHint("openai/gpt-4o", "")
	if !ok || p.Name() != "openai" {
		t.Errorf("prefix routing should work, got %v, ok=%v", p, ok)
	}
}
