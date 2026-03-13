package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/pinealctx/anti-gateway/internal/tenant"
	"github.com/gin-gonic/gin"
)

const (
	// Context keys for downstream handlers
	CtxKeyTenantID        = "tenant_id"
	CtxKeyTenantName      = "tenant_name"
	CtxKeyAPIKey          = "api_key"
	CtxKeyDefaultProvider = "default_provider"
	CtxKeyRateLimiter     = "rate_limiter"
)

// TenantAuth returns middleware that authenticates requests against the tenant store.
// It sets tenant context values for use by downstream handlers.
// If store is nil, falls back to the original single-key Auth behavior.
func TenantAuth(store *tenant.Store, limiter *tenant.RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "Missing API key",
					"type":    "authentication_error",
				},
			})
			return
		}

		key, ok := store.GetKeyByToken(token)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "Invalid API key",
					"type":    "authentication_error",
				},
			})
			return
		}

		if !key.Enabled {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"message": "API key is disabled",
					"type":    "permission_error",
				},
			})
			return
		}

		// QPM rate limit check
		if key.QPM > 0 && limiter != nil {
			if !limiter.AllowRequest(key.ID, key.QPM) {
				retryAfter := limiter.RetryAfter(key.ID)
				c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": gin.H{
						"message": fmt.Sprintf("Rate limit exceeded: %d QPM. Retry after %d seconds.", key.QPM, retryAfter),
						"type":    "rate_limit_error",
					},
				})
				return
			}
		}

		// TPM rate limit pre-check (reject if already over budget)
		if key.TPM > 0 && limiter != nil {
			if !limiter.AllowTokens(key.ID, key.TPM, 0) {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": gin.H{
						"message": fmt.Sprintf("Token rate limit exceeded: %d TPM.", key.TPM),
						"type":    "rate_limit_error",
					},
				})
				return
			}
		}

		// Set tenant context for downstream
		c.Set(CtxKeyTenantID, key.ID)
		c.Set(CtxKeyTenantName, key.Name)
		c.Set(CtxKeyAPIKey, key)
		if limiter != nil {
			c.Set(CtxKeyRateLimiter, limiter)
		}
		if key.DefaultProvider != "" {
			c.Set(CtxKeyDefaultProvider, key.DefaultProvider)
		}

		c.Next()
	}
}

// CheckModelPermission verifies that the tenant is allowed to access a model.
// Call this in handlers after model is known.
func CheckModelPermission(c *gin.Context, model string) bool {
	keyVal, exists := c.Get(CtxKeyAPIKey)
	if !exists {
		return true // No tenant auth, allow all
	}
	key := keyVal.(*tenant.APIKey)
	if len(key.AllowedModels) == 0 {
		return true // Empty = all models allowed
	}
	for _, m := range key.AllowedModels {
		if strings.EqualFold(m, model) {
			return true
		}
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error": gin.H{
			"message": fmt.Sprintf("Model %q not allowed for this API key", model),
			"type":    "permission_error",
		},
	})
	return false
}

// CheckProviderPermission verifies that the tenant is allowed to use a provider.
func CheckProviderPermission(c *gin.Context, provider string) bool {
	keyVal, exists := c.Get(CtxKeyAPIKey)
	if !exists {
		return true
	}
	key := keyVal.(*tenant.APIKey)
	if len(key.AllowedProviders) == 0 {
		return true
	}
	for _, p := range key.AllowedProviders {
		if strings.EqualFold(p, provider) {
			return true
		}
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error": gin.H{
			"message": fmt.Sprintf("Provider %q not allowed for this API key", provider),
			"type":    "permission_error",
		},
	})
	return false
}

// GetTenantID extracts tenant ID from gin context (returns "" if no tenant auth).
func GetTenantID(c *gin.Context) string {
	id, _ := c.Get(CtxKeyTenantID)
	if id == nil {
		return ""
	}
	return id.(string)
}

// GetDefaultProvider returns the API Key's preferred provider (empty if not set).
func GetDefaultProvider(c *gin.Context) string {
	v, _ := c.Get(CtxKeyDefaultProvider)
	if v == nil {
		return ""
	}
	return v.(string)
}

// RecordTokenUsage records token consumption for TPM tracking.
// Safe to call even when tenant auth is not enabled (no-op).
func RecordTokenUsage(c *gin.Context, totalTokens int) {
	if totalTokens <= 0 {
		return
	}
	keyVal, _ := c.Get(CtxKeyAPIKey)
	limiterVal, _ := c.Get(CtxKeyRateLimiter)
	if keyVal == nil || limiterVal == nil {
		return
	}
	key := keyVal.(*tenant.APIKey)
	limiter := limiterVal.(*tenant.RateLimiter)
	if key.TPM > 0 {
		limiter.RecordTokens(key.ID, totalTokens)
	}
}

// ExtractBearerToken extracts the Bearer token from Authorization or x-api-key headers.
func ExtractBearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	token := strings.TrimPrefix(auth, "Bearer ")
	if token != "" && token != auth {
		return token
	}
	// Anthropic SDK uses x-api-key
	return c.GetHeader("x-api-key")
}

func extractToken(c *gin.Context) string {
	return ExtractBearerToken(c)
}
