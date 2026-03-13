package routes

import (
	"crypto/hmac"
	"net/http"

	"github.com/SilkageNet/anti-gateway/internal/api/handlers"
	"github.com/SilkageNet/anti-gateway/internal/config"
	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	"github.com/SilkageNet/anti-gateway/internal/middleware"
	copilotProvider "github.com/SilkageNet/anti-gateway/internal/providers/copilot"
	kiroProvider "github.com/SilkageNet/anti-gateway/internal/providers/kiro"
	"github.com/SilkageNet/anti-gateway/internal/tenant"
	"github.com/SilkageNet/anti-gateway/internal/web"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// RouterConfig holds everything needed to set up routes.
type RouterConfig struct {
	Registry        *providers.Registry
	Logger          *zap.Logger
	APIKey          string        // Legacy single-key auth (used when Store is nil)
	AdminKey        string        // Admin API authentication key
	Store           *tenant.Store // Tenant store (nil = single-key mode)
	RateLimiter     *tenant.RateLimiter
	CopilotProvider *copilotProvider.Provider // Optional: for device flow management
	KiroProvider    *kiroProvider.Provider    // Optional: for Kiro PKCE login management
}

func SetupRouter(cfg RouterConfig) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// Global middleware
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.Logger(cfg.Logger))
	r.Use(middleware.Metrics())
	r.Use(middleware.CORS())

	// Root endpoint: service info
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"name":    "AntiGateway",
			"version": config.Version,
			"endpoints": []string{
				"/v1/chat/completions",
				"/v1/embeddings",
				"/v1/models",
				"/v1/messages",
				"/v1/messages/count_tokens",
				"/health",
				"/metrics",
				"/admin/keys",
				"/admin/providers",
				"/admin/usage",
				"/admin/kiro/login",
				"/admin/kiro/status",
				"/ui/",
			},
		})
	})

	// Health check (no auth)
	r.GET("/health", func(c *gin.Context) {
		status := gin.H{"status": "ok"}
		for name, p := range cfg.Registry.All() {
			status[name] = p.IsHealthy(c.Request.Context())
		}
		c.JSON(http.StatusOK, status)
	})

	// Prometheus metrics (no auth)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Web admin UI (no auth — SPA handles admin key via localStorage)
	r.GET("/ui", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/ui/")
	})
	r.GET("/ui/*filepath", gin.WrapH(http.StripPrefix("/ui", web.Handler())))

	// API routes (with auth)
	api := r.Group("/")
	if cfg.Store != nil {
		// Multi-tenant auth
		api.Use(middleware.TenantAuth(cfg.Store, cfg.RateLimiter))
	} else {
		// Legacy single-key auth
		api.Use(middleware.Auth(cfg.APIKey))
	}

	// OpenAI-compatible endpoints
	openaiH := handlers.NewOpenAIHandler(cfg.Registry, cfg.Logger)
	api.POST("/v1/chat/completions", openaiH.ChatCompletions)
	api.POST("/v1/embeddings", openaiH.Embeddings)
	api.GET("/v1/models", openaiH.Models)

	// Anthropic-compatible endpoints
	anthropicH := handlers.NewAnthropicHandler(cfg.Registry, cfg.Logger)
	api.POST("/v1/messages", anthropicH.Messages)
	api.POST("/v1/messages/count_tokens", anthropicH.CountTokens)

	// Admin API (separate auth with admin key)
	if cfg.Store != nil {
		admin := r.Group("/admin")
		admin.Use(adminAuth(cfg.AdminKey))

		adminH := handlers.NewAdminHandler(cfg.Store, cfg.Registry)
		admin.POST("/keys", adminH.CreateKey)
		admin.GET("/keys", adminH.ListKeys)
		admin.GET("/keys/:id", adminH.GetKey)
		admin.PUT("/keys/:id", adminH.UpdateKey)
		admin.DELETE("/keys/:id", adminH.DeleteKey)
		admin.GET("/providers", adminH.ListProviders)
		admin.GET("/usage", adminH.GetUsage)

		// Copilot device flow management (only if copilot provider exists)
		if cfg.CopilotProvider != nil {
			copilotH := handlers.NewCopilotAdminHandler(cfg.CopilotProvider)
			admin.POST("/auth/device-code", copilotH.StartDeviceFlow)
			admin.GET("/auth/poll/:id", copilotH.PollDeviceFlow)
			admin.POST("/auth/complete/:id", copilotH.CompleteDeviceFlow)
			admin.GET("/copilot/accounts", copilotH.ListAccounts)
		}

		// Kiro PKCE login management (only if kiro provider exists)
		if cfg.KiroProvider != nil {
			kiroH := handlers.NewKiroAdminHandler(cfg.KiroProvider)
			admin.POST("/kiro/login", kiroH.StartLogin)
			admin.GET("/kiro/login/:id", kiroH.GetLoginStatus)
			admin.POST("/kiro/login/complete/:id", kiroH.CompleteLogin)
			admin.GET("/kiro/status", kiroH.GetStatus)
		}
	}

	return r
}

// adminAuth is a simple bearer token auth for admin endpoints.
func adminAuth(adminKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if adminKey == "" {
			c.Next() // No admin key configured = no admin auth
			return
		}
		token := middleware.ExtractBearerToken(c)
		if token == "" || !hmac.Equal([]byte(token), []byte(adminKey)) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "Invalid admin key", "type": "authentication_error"},
			})
			return
		}
		c.Next()
	}
}
