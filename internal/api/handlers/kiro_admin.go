package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pinealctx/anti-gateway/internal/core/providers"
	kiroProvider "github.com/pinealctx/anti-gateway/internal/providers/kiro"
)

// KiroAdminHandler provides Kiro PKCE login management endpoints.
type KiroAdminHandler struct {
	registry *providers.Registry
}

func NewKiroAdminHandler(registry *providers.Registry) *KiroAdminHandler {
	return &KiroAdminHandler{registry: registry}
}

// findKiroProvider dynamically finds the first Kiro provider from the registry.
func (h *KiroAdminHandler) findKiroProvider() *kiroProvider.Provider {
	for _, p := range h.registry.All() {
		if kp, ok := p.(*kiroProvider.Provider); ok {
			return kp
		}
	}
	return nil
}

// StartLogin initiates a Kiro PKCE authorization code login flow.
// POST /admin/kiro/login
// Body: { "port": 3128 } (optional, default 3128)
func (h *KiroAdminHandler) StartLogin(c *gin.Context) {
	provider := h.findKiroProvider()
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no kiro provider configured"})
		return
	}

	var req struct {
		Port int `json:"port"`
	}
	_ = c.ShouldBindJSON(&req) // optional body

	session, err := provider.AuthMgr().StartLogin(req.Port)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":       session.ID,
		"auth_url": session.AuthURL,
		"port":     session.CallbackPort,
		"status":   session.Status,
	})
}

// GetLoginStatus checks the status of a Kiro PKCE login session.
// GET /admin/kiro/login/:id
func (h *KiroAdminHandler) GetLoginStatus(c *gin.Context) {
	provider := h.findKiroProvider()
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no kiro provider configured"})
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

// CompleteLogin finalizes a Kiro PKCE login by injecting the token into the provider.
// POST /admin/kiro/login/complete/:id
func (h *KiroAdminHandler) CompleteLogin(c *gin.Context) {
	provider := h.findKiroProvider()
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no kiro provider configured"})
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
	accessToken := session.AccessToken
	refreshToken := session.RefreshToken
	clientID := session.ClientID
	tokenEndpoint := session.TokenEndpoint
	tokenExpiresAt := session.TokenExpiresAt
	profileArn := session.ProfileArn
	session.Mu().Unlock()

	if status != "completed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session not completed yet", "status": status})
		return
	}

	if accessToken == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no access token available"})
		return
	}

	lt := &kiroProvider.LoginToken{
		AccessToken:   accessToken,
		RefreshToken:  refreshToken,
		ClientID:      clientID,
		TokenEndpoint: tokenEndpoint,
		ExpiresAt:     tokenExpiresAt,
		IsExternalIdP: true,
		ProfileArn:    profileArn,
	}

	provider.SetLoginToken(lt)
	provider.AuthMgr().RemoveSession(id)

	c.JSON(http.StatusOK, gin.H{
		"message": "Kiro token activated via PKCE login",
	})
}

// GetStatus shows the current Kiro token status.
// GET /admin/kiro/status
func (h *KiroAdminHandler) GetStatus(c *gin.Context) {
	provider := h.findKiroProvider()
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no kiro provider configured"})
		return
	}

	c.JSON(http.StatusOK, provider.TokenStatus())
}

// RefreshToken forces a token refresh.
// POST /admin/kiro/refresh
func (h *KiroAdminHandler) RefreshToken(c *gin.Context) {
	provider := h.findKiroProvider()
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no kiro provider configured"})
		return
	}

	if err := provider.ForceRefresh(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, provider.TokenStatus())
}
