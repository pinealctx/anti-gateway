package middleware

import (
	"crypto/hmac"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Auth returns a Gin middleware that validates the API key via timing-safe comparison.
// If apiKey is empty, no authentication is enforced.
func Auth(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiKey == "" {
			c.Next()
			return
		}

		auth := c.GetHeader("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")

		// Also check x-api-key header (common in Anthropic SDK)
		if token == "" || token == auth {
			token = c.GetHeader("x-api-key")
		}

		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "Missing API key",
					"type":    "authentication_error",
				},
			})
			return
		}

		// Timing-safe comparison to prevent timing attacks
		if !hmac.Equal([]byte(token), []byte(apiKey)) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "Invalid API key",
					"type":    "authentication_error",
				},
			})
			return
		}

		c.Next()
	}
}
