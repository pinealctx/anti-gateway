package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pinealctx/anti-gateway/core/providers"
	copilotProvider "github.com/pinealctx/anti-gateway/providers/copilot"
	"github.com/pinealctx/anti-gateway/tenant"
)

// CopilotAdminHandler provides device flow management endpoints.
type CopilotAdminHandler struct {
	registry *providers.Registry
	store    *tenant.Store
}

func NewCopilotAdminHandler(registry *providers.Registry, store *tenant.Store) *CopilotAdminHandler {
	return &CopilotAdminHandler{registry: registry, store: store}
}

// findCopilotProvider finds a Copilot provider by name, or the first one if name is empty.
func (h *CopilotAdminHandler) findCopilotProvider(name string) *copilotProvider.Provider {
	if name != "" {
		if p, ok := h.registry.Get(name); ok {
			if cp, ok := p.(*copilotProvider.Provider); ok {
				return cp
			}
		}
		return nil
	}
	// Find first Copilot provider
	for _, p := range h.registry.All() {
		if cp, ok := p.(*copilotProvider.Provider); ok {
			return cp
		}
	}
	return nil
}

// StartDeviceFlow initiates a GitHub device code flow.
// POST /admin/auth/device-code?provider=<name>
func (h *CopilotAdminHandler) StartDeviceFlow(c *gin.Context) {
	providerName := c.Query("provider")
	provider := h.findCopilotProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no copilot provider configured"})
		return
	}

	session, err := provider.AuthMgr().StartDeviceFlow()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":               session.ID,
		"user_code":        session.UserCode,
		"verification_uri": session.VerificationURI,
		"expires_at":       session.ExpiresAt,
		"interval":         session.Interval,
		"status":           session.Status,
	})
}

// PollDeviceFlow checks the status of a device flow session.
// GET /admin/auth/poll/:id?provider=<name>
func (h *CopilotAdminHandler) PollDeviceFlow(c *gin.Context) {
	providerName := c.Query("provider")
	provider := h.findCopilotProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no copilot provider configured"})
		return
	}

	id := c.Param("id")
	session, ok := provider.AuthMgr().GetSession(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	session.Mu().Lock()
	status := session.Status
	errStr := session.Error
	session.Mu().Unlock()

	resp := gin.H{
		"id":     id,
		"status": status,
	}
	if errStr != "" {
		resp["error"] = errStr
	}

	c.JSON(http.StatusOK, resp)
}

// CompleteDeviceFlow finalizes a device flow by setting the token on the provider.
// POST /admin/auth/complete/:id?provider=<name>
func (h *CopilotAdminHandler) CompleteDeviceFlow(c *gin.Context) {
	providerName := c.Query("provider")
	provider := h.findCopilotProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no copilot provider configured"})
		return
	}

	id := c.Param("id")
	session, ok := provider.AuthMgr().GetSession(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	session.Mu().Lock()
	status := session.Status
	token := session.AccessToken
	session.Mu().Unlock()

	if status != "completed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session not completed yet", "status": status})
		return
	}

	if token == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no access token available"})
		return
	}

	// Set the GitHub token on the provider (single account per provider)
	provider.SetGithubToken(token)
	provider.AuthMgr().RemoveSession(id)

	// Persist the token to database
	if h.store != nil {
		if rec, exists := h.store.GetProviderByName(provider.Name()); exists {
			_, _ = h.store.UpdateProvider(rec.ID, tenant.WithProviderGithubToken(token))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Copilot token activated",
		"provider": provider.Name(),
	})
}

// GetStatus shows the current Copilot provider token status.
// GET /admin/copilot/status?provider=<name>
func (h *CopilotAdminHandler) GetStatus(c *gin.Context) {
	providerName := c.Query("provider")
	provider := h.findCopilotProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no copilot provider configured"})
		return
	}

	c.JSON(http.StatusOK, provider.GetTokenInfo())
}
