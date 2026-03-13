package providers

import (
	"context"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/SilkageNet/anti-gateway/internal/models"
)

// StreamChunk represents one piece of a streaming response.
type StreamChunk struct {
	Content      string
	ToolCalls    []models.ToolCall
	FinishReason string
	Error        error
}

// AIProvider is the core interface every upstream channel must implement.
type AIProvider interface {
	// Name returns the provider identifier (e.g. "kiro", "openai", "gemini").
	Name() string

	// ChatCompletion performs a non-streaming chat completion.
	ChatCompletion(ctx context.Context, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error)

	// StreamCompletion performs a streaming chat completion, sending chunks to the channel.
	// The provider MUST close the channel when done.
	StreamCompletion(ctx context.Context, req *models.ChatCompletionRequest, stream chan<- StreamChunk) error

	// RefreshToken refreshes tokens if the provider requires short-lived credentials.
	RefreshToken(ctx context.Context) error

	// IsHealthy checks if the provider is ready to serve requests.
	IsHealthy(ctx context.Context) bool
}

// EmbeddingProvider is an optional interface for providers that support embeddings.
type EmbeddingProvider interface {
	CreateEmbedding(ctx context.Context, req *models.EmbeddingRequest) (*models.EmbeddingResponse, error)
}

// ProviderEntry wraps a provider with routing metadata.
type ProviderEntry struct {
	Provider AIProvider
	Weight   int
	Models   map[string]bool // models this provider handles; empty = all
	healthy  bool
	Stats    *ProviderStats // runtime statistics for smart LB
}

// Registry holds all registered providers with model routing and health tracking.
type Registry struct {
	mu       sync.RWMutex
	entries  map[string]*ProviderEntry
	fallback string // default provider name
	rng      *rand.Rand
	strategy LBStrategy
	rrIndex  uint64 // round-robin counter
}

func NewRegistry(fallback string) *Registry {
	return &Registry{
		entries:  make(map[string]*ProviderEntry),
		fallback: fallback,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
		strategy: LBWeightedRandom,
	}
}

// NewRegistryWithStrategy creates a registry with a specific LB strategy.
func NewRegistryWithStrategy(fallback string, strategy LBStrategy) *Registry {
	return &Registry{
		entries:  make(map[string]*ProviderEntry),
		fallback: fallback,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
		strategy: strategy,
	}
}

// Register adds a provider with default weight=1 and no model filter (handles all).
func (r *Registry) Register(p AIProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[p.Name()] = &ProviderEntry{
		Provider: p,
		Weight:   1,
		Models:   nil,
		healthy:  true,
		Stats:    &ProviderStats{},
	}
}

// RegisterWithConfig adds a provider with routing configuration.
func (r *Registry) RegisterWithConfig(p AIProvider, weight int, modelList []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	models := make(map[string]bool, len(modelList))
	for _, m := range modelList {
		models[m] = true
	}
	if weight < 1 {
		weight = 1
	}
	r.entries[p.Name()] = &ProviderEntry{
		Provider: p,
		Weight:   weight,
		Models:   models,
		healthy:  true,
		Stats:    &ProviderStats{},
	}
}

// Unregister removes a provider from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, name)
}

// Get returns a specific provider by name.
func (r *Registry) Get(name string) (AIProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.entries[name]; ok {
		return e.Provider, true
	}
	if e, ok := r.entries[r.fallback]; ok {
		return e.Provider, true
	}
	return nil, false
}

// ParseModelPrefix splits a "provider/model" string into (provider, model).
// If no prefix, returns ("", original).
func ParseModelPrefix(raw string) (providerHint, model string) {
	if idx := strings.Index(raw, "/"); idx > 0 && idx < len(raw)-1 {
		return raw[:idx], raw[idx+1:]
	}
	return "", raw
}

// Resolve finds the best provider for a given model.
// It uses weighted random selection among healthy providers that handle the model.
func (r *Registry) Resolve(model string) (AIProvider, bool) {
	return r.ResolveWithHint(model, "")
}

// ResolveWithHint finds a provider with explicit routing priority:
//  1. Model prefix: "openai/gpt-4o" → strip prefix, force provider "openai"
//  2. providerHint (e.g. from API Key's DefaultProvider)
//  3. Model-based weighted random selection (default behavior)
func (r *Registry) ResolveWithHint(rawModel, providerHint string) (AIProvider, bool) {
	// Priority 1: model prefix overrides everything
	prefix, model := ParseModelPrefix(rawModel)
	if prefix != "" {
		providerHint = prefix
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Priority 2: explicit provider hint
	if providerHint != "" {
		if e, ok := r.entries[providerHint]; ok && e.healthy {
			return e.Provider, true
		}
		// Hint provider unavailable, fall through to weighted selection
	}

	// Priority 3: strategy-based selection among healthy candidates
	var candidates []*ProviderEntry
	var totalWeight int
	for _, e := range r.entries {
		if !e.healthy {
			continue
		}
		if len(e.Models) > 0 && !e.Models[model] {
			continue
		}
		candidates = append(candidates, e)
		totalWeight += e.Weight
	}

	if len(candidates) == 0 {
		// Fall back to default provider regardless of model filter
		if e, ok := r.entries[r.fallback]; ok && e.healthy {
			return e.Provider, true
		}
		return nil, false
	}

	if len(candidates) == 1 {
		return candidates[0].Provider, true
	}

	return r.selectByStrategy(candidates, totalWeight), true
}

// selectByStrategy picks a provider from candidates using the configured strategy.
func (r *Registry) selectByStrategy(candidates []*ProviderEntry, totalWeight int) AIProvider {
	switch r.strategy {
	case LBRoundRobin:
		idx := r.rrIndex % uint64(len(candidates))
		r.rrIndex++
		return candidates[idx].Provider

	case LBLeastUsed:
		var best *ProviderEntry
		var bestCount int64 = -1
		for _, e := range candidates {
			count := e.Stats.RequestCount.Load()
			if bestCount < 0 || count < bestCount {
				bestCount = count
				best = e
			}
		}
		return best.Provider

	case LBPriority:
		// Pick highest weight; on tie, first registered wins
		var best *ProviderEntry
		for _, e := range candidates {
			if best == nil || e.Weight > best.Weight {
				best = e
			}
		}
		return best.Provider

	case LBSmart:
		// Score-based: weight * 100 / (1 + recent429s*10 + avgLatencyMs/100)
		var best *ProviderEntry
		var bestScore float64 = -1
		for _, e := range candidates {
			score := e.Stats.Score(e.Weight, 5*time.Minute)
			if score > bestScore {
				bestScore = score
				best = e
			}
		}
		return best.Provider

	default: // LBWeightedRandom
		pick := r.rng.Intn(totalWeight)
		for _, e := range candidates {
			pick -= e.Weight
			if pick < 0 {
				return e.Provider
			}
		}
		return candidates[0].Provider
	}
}

// GetStats returns stats for a named provider.
func (r *Registry) GetStats(name string) *ProviderStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.entries[name]; ok {
		return e.Stats
	}
	return nil
}

// Strategy returns the current LB strategy.
func (r *Registry) Strategy() LBStrategy {
	return r.strategy
}

// All returns all providers (for health endpoint).
func (r *Registry) All() map[string]AIProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]AIProvider, len(r.entries))
	for name, e := range r.entries {
		result[name] = e.Provider
	}
	return result
}

// SetHealthy updates the health status of a provider.
func (r *Registry) SetHealthy(name string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[name]; ok {
		e.healthy = healthy
	}
}

// IsHealthy returns the health status of a provider.
func (r *Registry) IsHealthy(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.entries[name]; ok {
		return e.healthy
	}
	return false
}

// StartHealthCheck runs periodic health checks on all providers.
func (r *Registry) StartHealthCheck(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			r.mu.RLock()
			entries := make(map[string]*ProviderEntry, len(r.entries))
			for k, v := range r.entries {
				entries[k] = v
			}
			r.mu.RUnlock()

			for name, entry := range entries {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				healthy := entry.Provider.IsHealthy(ctx)
				cancel()
				r.SetHealthy(name, healthy)
			}
		}
	}()
}

// Entries returns all provider entries (for admin/debug).
func (r *Registry) Entries() map[string]*ProviderEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]*ProviderEntry, len(r.entries))
	for k, v := range r.entries {
		result[k] = v
	}
	return result
}
