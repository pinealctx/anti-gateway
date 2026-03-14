package middleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS returns a CORS middleware.
// If origins is empty, allows all origins (development mode).
// In production, pass explicit origins to restrict access.
func CORS(origins []string) gin.HandlerFunc {
	if len(origins) == 0 {
		origins = []string{"*"}
	}
	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-API-Key", "X-Request-ID", "anthropic-version"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	})
}
