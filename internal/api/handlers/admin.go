package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pinealctx/anti-gateway/internal/config"
	"github.com/pinealctx/anti-gateway/internal/core/providers"
	"github.com/pinealctx/anti-gateway/internal/tenant"
	"go.uber.org/zap"
)

// ProviderFactory creates an AIProvider from a ProviderConfig.
type ProviderFactory func(pc config.ProviderConfig, logger *zap.Logger) (providers.AIProvider, error)

// AdminHandler provides management endpoints for API keys, providers, and usage.
type AdminHandler struct {
	store    *tenant.Store
	registry *providers.Registry
	factory  ProviderFactory
	logger   *zap.Logger
}

func NewAdminHandler(store *tenant.Store, registry *providers.Registry, factory ProviderFactory, logger *zap.Logger) *AdminHandler {
	return &AdminHandler{store: store, registry: registry, factory: factory, logger: logger}
}

// adminError writes a standardized error response for Admin endpoints.
func adminError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{"message": message, "type": errType},
	})
}

// ============================================================
// POST /admin/keys — Create API key
// ============================================================

type createKeyRequest struct {
	Name             string   `json:"name" binding:"required"`
	AllowedModels    []string `json:"allowed_models"`
	AllowedProviders []string `json:"allowed_providers"`
	DefaultProvider  string   `json:"default_provider"`
	QPM              int      `json:"qpm"`
	TPM              int      `json:"tpm"`
}

func (h *AdminHandler) CreateKey(c *gin.Context) {
	var req createKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		adminError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	opts := []tenant.KeyOption{}
	if len(req.AllowedModels) > 0 {
		opts = append(opts, tenant.WithModels(req.AllowedModels))
	}
	if len(req.AllowedProviders) > 0 {
		opts = append(opts, tenant.WithProviders(req.AllowedProviders))
	}
	if req.DefaultProvider != "" {
		opts = append(opts, tenant.WithDefaultProvider(req.DefaultProvider))
	}
	if req.QPM > 0 {
		opts = append(opts, tenant.WithQPM(req.QPM))
	}
	if req.TPM > 0 {
		opts = append(opts, tenant.WithTPM(req.TPM))
	}

	key, err := h.store.CreateKey(req.Name, opts...)
	if err != nil {
		adminError(c, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	c.JSON(http.StatusCreated, key)
}

// ============================================================
// GET /admin/keys — List all keys
// ============================================================

func (h *AdminHandler) ListKeys(c *gin.Context) {
	keys := h.store.ListKeys()
	// Mask key tokens in list response
	type maskedKey struct {
		ID               string   `json:"id"`
		KeyPrefix        string   `json:"key_prefix"`
		Name             string   `json:"name"`
		Enabled          bool     `json:"enabled"`
		AllowedModels    []string `json:"allowed_models"`
		AllowedProviders []string `json:"allowed_providers"`
		DefaultProvider  string   `json:"default_provider"`
		QPM              int      `json:"qpm"`
		TPM              int      `json:"tpm"`
		CreatedAt        string   `json:"created_at"`
	}
	result := make([]maskedKey, len(keys))
	for i, k := range keys {
		prefix := k.Key
		if len(prefix) > 10 {
			prefix = prefix[:10] + "..."
		}
		result[i] = maskedKey{
			ID:               k.ID,
			KeyPrefix:        prefix,
			Name:             k.Name,
			Enabled:          k.Enabled,
			AllowedModels:    k.AllowedModels,
			AllowedProviders: k.AllowedProviders,
			DefaultProvider:  k.DefaultProvider,
			QPM:              k.QPM,
			TPM:              k.TPM,
			CreatedAt:        k.CreatedAt.Format(time.RFC3339),
		}
	}
	c.JSON(http.StatusOK, gin.H{"keys": result, "total": len(result)})
}

// ============================================================
// GET /admin/keys/:id — Get key detail
// ============================================================

func (h *AdminHandler) GetKey(c *gin.Context) {
	id := c.Param("id")
	key, err := h.store.GetKeyByID(id)
	if err != nil {
		adminError(c, http.StatusNotFound, "not_found", "key not found")
		return
	}
	c.JSON(http.StatusOK, key)
}

// ============================================================
// PUT /admin/keys/:id — Update key
// ============================================================

type updateKeyRequest struct {
	Name             *string  `json:"name"`
	Enabled          *bool    `json:"enabled"`
	AllowedModels    []string `json:"allowed_models"`
	AllowedProviders []string `json:"allowed_providers"`
	DefaultProvider  *string  `json:"default_provider"`
	QPM              *int     `json:"qpm"`
	TPM              *int     `json:"tpm"`
}

func (h *AdminHandler) UpdateKey(c *gin.Context) {
	id := c.Param("id")
	var req updateKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		adminError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	opts := []tenant.KeyOption{}
	if req.Name != nil {
		opts = append(opts, tenant.WithName(*req.Name))
	}
	if req.Enabled != nil {
		opts = append(opts, tenant.WithEnabled(*req.Enabled))
	}
	if req.AllowedModels != nil {
		opts = append(opts, tenant.WithModels(req.AllowedModels))
	}
	if req.AllowedProviders != nil {
		opts = append(opts, tenant.WithProviders(req.AllowedProviders))
	}
	if req.DefaultProvider != nil {
		opts = append(opts, tenant.WithDefaultProvider(*req.DefaultProvider))
	}
	if req.QPM != nil {
		opts = append(opts, tenant.WithQPM(*req.QPM))
	}
	if req.TPM != nil {
		opts = append(opts, tenant.WithTPM(*req.TPM))
	}

	key, err := h.store.UpdateKey(id, opts...)
	if err != nil {
		adminError(c, http.StatusNotFound, "not_found", err.Error())
		return
	}
	c.JSON(http.StatusOK, key)
}

// ============================================================
// DELETE /admin/keys/:id — Delete key
// ============================================================

func (h *AdminHandler) DeleteKey(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.DeleteKey(id); err != nil {
		adminError(c, http.StatusNotFound, "not_found", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ============================================================
// POST /admin/providers — Create provider
// ============================================================

type createProviderRequest struct {
	Name         string   `json:"name" binding:"required"`
	Type         string   `json:"type" binding:"required"`
	Weight       int      `json:"weight"`
	BaseURL      string   `json:"base_url"`
	APIKey       string   `json:"api_key"`
	GithubTokens []string `json:"github_tokens"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"default_model"`
}

func (h *AdminHandler) CreateProvider(c *gin.Context) {
	var req createProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		adminError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	// Validate type
	switch req.Type {
	case "kiro", "openai", "openai-compat", "copilot", "anthropic":
	default:
		adminError(c, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("unsupported provider type: %q", req.Type))
		return
	}

	// Check name uniqueness in DB
	if _, exists := h.store.GetProviderByName(req.Name); exists {
		adminError(c, http.StatusConflict, "conflict", fmt.Sprintf("provider %q already exists", req.Name))
		return
	}

	opts := []tenant.ProviderOption{}
	if req.Weight > 0 {
		opts = append(opts, tenant.WithProviderWeight(req.Weight))
	}
	if req.BaseURL != "" {
		opts = append(opts, tenant.WithProviderBaseURL(req.BaseURL))
	}
	if req.APIKey != "" {
		opts = append(opts, tenant.WithProviderAPIKey(req.APIKey))
	}
	if len(req.GithubTokens) > 0 {
		opts = append(opts, tenant.WithProviderGithubTokens(req.GithubTokens))
	}
	if len(req.Models) > 0 {
		opts = append(opts, tenant.WithProviderModels(req.Models))
	}
	if req.DefaultModel != "" {
		opts = append(opts, tenant.WithProviderDefaultModel(req.DefaultModel))
	}

	rec, err := h.store.CreateProvider(req.Name, req.Type, opts...)
	if err != nil {
		adminError(c, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	// Instantiate and register in the live registry
	if err := h.activateProvider(rec); err != nil {
		// Rollback DB record on activation failure
		_ = h.store.DeleteProvider(rec.ID)
		adminError(c, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("provider config invalid: %v", err))
		return
	}

	h.logger.Info("Provider created via admin API",
		zap.String("name", rec.Name), zap.String("type", rec.Type))

	c.JSON(http.StatusCreated, rec)
}

// ============================================================
// GET /admin/providers — List providers (DB + runtime status)
// ============================================================

func (h *AdminHandler) ListProviders(c *gin.Context) {
	records := h.store.ListProviderRecords()

	type providerInfo struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Type         string   `json:"type"`
		Weight       int      `json:"weight"`
		Enabled      bool     `json:"enabled"`
		BaseURL      string   `json:"base_url,omitempty"`
		Models       []string `json:"models,omitempty"`
		DefaultModel string   `json:"default_model,omitempty"`
		Healthy      bool     `json:"healthy"`
		CreatedAt    string   `json:"created_at"`
	}

	result := make([]providerInfo, 0, len(records))
	for _, r := range records {
		result = append(result, providerInfo{
			ID:           r.ID,
			Name:         r.Name,
			Type:         r.Type,
			Weight:       r.Weight,
			Enabled:      r.Enabled,
			BaseURL:      r.BaseURL,
			Models:       r.Models,
			DefaultModel: r.DefaultModel,
			Healthy:      h.registry.IsHealthy(r.Name),
			CreatedAt:    r.CreatedAt.Format(time.RFC3339),
		})
	}

	// Also include runtime-only providers (registered via config but not in DB)
	runtimeAll := h.registry.All()
	dbNames := make(map[string]bool, len(records))
	for _, r := range records {
		dbNames[r.Name] = true
	}
	for name := range runtimeAll {
		if !dbNames[name] {
			result = append(result, providerInfo{
				Name:    name,
				Healthy: h.registry.IsHealthy(name),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"providers": result, "total": len(result)})
}

// ============================================================
// GET /admin/providers/:id — Get provider detail
// ============================================================

func (h *AdminHandler) GetProvider(c *gin.Context) {
	id := c.Param("id")
	rec, err := h.store.GetProvider(id)
	if err != nil {
		adminError(c, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	c.JSON(http.StatusOK, rec)
}

// ============================================================
// PUT /admin/providers/:id — Update provider
// ============================================================

type updateProviderRequest struct {
	Name         *string  `json:"name"`
	Type         *string  `json:"type"`
	Weight       *int     `json:"weight"`
	Enabled      *bool    `json:"enabled"`
	BaseURL      *string  `json:"base_url"`
	APIKey       *string  `json:"api_key"`
	GithubTokens []string `json:"github_tokens"`
	Models       []string `json:"models"`
	DefaultModel *string  `json:"default_model"`
}

func (h *AdminHandler) UpdateProvider(c *gin.Context) {
	id := c.Param("id")

	var req updateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		adminError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	// Get old record for name change / re-registration
	oldRec, err := h.store.GetProvider(id)
	if err != nil {
		adminError(c, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	oldName := oldRec.Name

	opts := []tenant.ProviderOption{}
	if req.Name != nil {
		opts = append(opts, tenant.WithProviderName(*req.Name))
	}
	if req.Type != nil {
		opts = append(opts, tenant.WithProviderType(*req.Type))
	}
	if req.Weight != nil {
		opts = append(opts, tenant.WithProviderWeight(*req.Weight))
	}
	if req.Enabled != nil {
		opts = append(opts, tenant.WithProviderEnabled(*req.Enabled))
	}
	if req.BaseURL != nil {
		opts = append(opts, tenant.WithProviderBaseURL(*req.BaseURL))
	}
	if req.APIKey != nil {
		opts = append(opts, tenant.WithProviderAPIKey(*req.APIKey))
	}
	if req.GithubTokens != nil {
		opts = append(opts, tenant.WithProviderGithubTokens(req.GithubTokens))
	}
	if req.Models != nil {
		opts = append(opts, tenant.WithProviderModels(req.Models))
	}
	if req.DefaultModel != nil {
		opts = append(opts, tenant.WithProviderDefaultModel(*req.DefaultModel))
	}

	rec, err := h.store.UpdateProvider(id, opts...)
	if err != nil {
		adminError(c, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	// Re-register: unregister old name, register new instance
	h.registry.Unregister(oldName)
	if rec.Enabled {
		if err := h.activateProvider(rec); err != nil {
			h.logger.Error("Failed to re-activate provider after update", zap.String("name", rec.Name), zap.Error(err))
		}
	}

	h.logger.Info("Provider updated via admin API",
		zap.String("name", rec.Name), zap.String("type", rec.Type))

	c.JSON(http.StatusOK, rec)
}

// ============================================================
// DELETE /admin/providers/:id — Delete provider
// ============================================================

func (h *AdminHandler) DeleteProvider(c *gin.Context) {
	id := c.Param("id")

	rec, err := h.store.GetProvider(id)
	if err != nil {
		adminError(c, http.StatusNotFound, "not_found", "provider not found")
		return
	}

	h.registry.Unregister(rec.Name)
	if err := h.store.DeleteProvider(id); err != nil {
		adminError(c, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	h.logger.Info("Provider deleted via admin API", zap.String("name", rec.Name))
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// activateProvider converts a ProviderRecord to an AIProvider and registers it.
func (h *AdminHandler) activateProvider(rec *tenant.ProviderRecord) error {
	pc := config.ProviderConfig{
		Name:         rec.Name,
		Type:         rec.Type,
		Weight:       rec.Weight,
		Enabled:      rec.Enabled,
		BaseURL:      rec.BaseURL,
		APIKey:       rec.APIKey,
		GithubTokens: rec.GithubTokens,
		Models:       rec.Models,
		DefaultModel: rec.DefaultModel,
	}
	p, err := h.factory(pc, h.logger)
	if err != nil {
		return err
	}
	h.registry.RegisterWithConfig(p, rec.Weight, rec.Models)
	return nil
}

// ============================================================
// GET /admin/usage — Usage statistics
// ============================================================

func (h *AdminHandler) GetUsage(c *gin.Context) {
	q := tenant.UsageQuery{
		KeyID: c.Query("key_id"),
		Model: c.Query("model"),
	}
	if from := c.Query("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err == nil {
			q.From = t
		}
	}
	if to := c.Query("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err == nil {
			q.To = t
		}
	}

	summaries, err := h.store.QueryUsage(q)
	if err != nil {
		adminError(c, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	// Support CSV export via Accept header
	if c.GetHeader("Accept") == "text/csv" {
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename=usage.csv")
		w := csv.NewWriter(c.Writer)
		if err := w.Write([]string{"key_id", "key_name", "total_requests", "input_tokens", "output_tokens", "total_tokens"}); err != nil {
			return
		}
		for _, s := range summaries {
			if err := w.Write([]string{
				s.KeyID, s.KeyName,
				fmt.Sprintf("%d", s.TotalRequests),
				fmt.Sprintf("%d", s.InputTokens),
				fmt.Sprintf("%d", s.OutputTokens),
				fmt.Sprintf("%d", s.TotalTokens),
			}); err != nil {
				return
			}
		}
		w.Flush()
		return
	}

	c.JSON(http.StatusOK, gin.H{"usage": summaries})
}
