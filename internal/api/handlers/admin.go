package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"time"

	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	"github.com/SilkageNet/anti-gateway/internal/tenant"
	"github.com/gin-gonic/gin"
)

// AdminHandler provides management endpoints for API keys, providers, and usage.
type AdminHandler struct {
	store    *tenant.Store
	registry *providers.Registry
}

func NewAdminHandler(store *tenant.Store, registry *providers.Registry) *AdminHandler {
	return &AdminHandler{store: store, registry: registry}
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ============================================================
// GET /admin/providers — Provider status
// ============================================================

func (h *AdminHandler) ListProviders(c *gin.Context) {
	all := h.registry.All()
	type providerStatus struct {
		Name    string `json:"name"`
		Healthy bool   `json:"healthy"`
	}
	result := make([]providerStatus, 0, len(all))
	for name := range all {
		result = append(result, providerStatus{
			Name:    name,
			Healthy: h.registry.IsHealthy(name),
		})
	}
	c.JSON(http.StatusOK, gin.H{"providers": result})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Support CSV export via Accept header
	if c.GetHeader("Accept") == "text/csv" {
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename=usage.csv")
		w := csv.NewWriter(c.Writer)
		w.Write([]string{"key_id", "key_name", "total_requests", "input_tokens", "output_tokens", "total_tokens"})
		for _, s := range summaries {
			w.Write([]string{
				s.KeyID, s.KeyName,
				fmt.Sprintf("%d", s.TotalRequests),
				fmt.Sprintf("%d", s.InputTokens),
				fmt.Sprintf("%d", s.OutputTokens),
				fmt.Sprintf("%d", s.TotalTokens),
			})
		}
		w.Flush()
		return
	}

	c.JSON(http.StatusOK, gin.H{"usage": summaries})
}
