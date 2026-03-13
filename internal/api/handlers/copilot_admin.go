package handlers

import (
	"net/http"

	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	copilotProvider "github.com/SilkageNet/anti-gateway/internal/providers/copilot"
	"github.com/gin-gonic/gin"
)

// CopilotAdminHandler provides device flow management endpoints.
type CopilotAdminHandler struct {
	registry *providers.Registry
}

func NewCopilotAdminHandler(registry *providers.Registry) *CopilotAdminHandler {
	return &CopilotAdminHandler{registry: registry}
}

// findCopilotProvider dynamically finds the first Copilot provider from the registry.
func (h *CopilotAdminHandler) findCopilotProvider() *copilotProvider.Provider {
	for _, p := range h.registry.All() {
		if cp, ok := p.(*copilotProvider.Provider); ok {
			return cp
		}
	}
	return nil
}

// StartDeviceFlow initiates a GitHub device code flow.
// POST /admin/auth/device-code
func (h *CopilotAdminHandler) StartDeviceFlow(c *gin.Context) {
	provider := h.findCopilotProvider()
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
// GET /admin/auth/poll/:id
func (h *CopilotAdminHandler) PollDeviceFlow(c *gin.Context) {
	provider := h.findCopilotProvider()
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

// CompleteDeviceFlow finalizes a device flow by adding the token to the pool.
// POST /admin/auth/complete/:id
func (h *CopilotAdminHandler) CompleteDeviceFlow(c *gin.Context) {
	provider := h.findCopilotProvider()
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

	// Add the new account to the Copilot provider pool
	provider.AddAccount(token)
	provider.AuthMgr().RemoveSession(id)

	c.JSON(http.StatusOK, gin.H{
		"message": "Account added to Copilot pool",
	})
}

// ListAccounts shows current Copilot accounts and their status.
// GET /admin/copilot/accounts
func (h *CopilotAdminHandler) ListAccounts(c *gin.Context) {
	provider := h.findCopilotProvider()
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no copilot provider configured"})
		return
	}

	accounts := provider.ListAccounts()
	c.JSON(http.StatusOK, gin.H{
		"accounts": accounts,
		"total":    len(accounts),
	})
}
