package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SilkageNet/anti-gateway/internal/api/routes"
	"github.com/SilkageNet/anti-gateway/internal/config"
	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	anthropicProvider "github.com/SilkageNet/anti-gateway/internal/providers/anthropic"
	copilotProvider "github.com/SilkageNet/anti-gateway/internal/providers/copilot"
	"github.com/SilkageNet/anti-gateway/internal/providers/kiro"
	openaiProvider "github.com/SilkageNet/anti-gateway/internal/providers/openai"
	"github.com/SilkageNet/anti-gateway/internal/tenant"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:     "antigateway",
	Short:   "AntiGateway - Unified AI Gateway",
	Long:    "AntiGateway is a standalone AI gateway that proxies OpenAI / Anthropic protocols to multiple upstream providers.",
	Version: config.Version,
	RunE:    runServe,
}

func init() {
	config.BindFlags(rootCmd)
}

func runServe(cmd *cobra.Command, _ []string) error {
	gwCfg, err := config.LoadGatewayConfig(cmd)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup logger
	var logger *zap.Logger
	if gwCfg.Server.LogLevel == "debug" {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("Starting AntiGateway",
		zap.String("version", config.Version),
		zap.String("host", gwCfg.Server.Host),
		zap.Int("port", gwCfg.Server.Port),
		zap.Int("providers", len(gwCfg.Providers)),
		zap.Bool("multi_tenant", gwCfg.Tenant.Enabled),
	)

	// Initialize provider registry
	fallback := gwCfg.Defaults.Provider
	if fallback == "" && len(gwCfg.Providers) > 0 {
		fallback = gwCfg.Providers[0].Name
	}
	strategy := providers.LBStrategy(gwCfg.Defaults.LBStrategy)
	if strategy == "" {
		strategy = providers.LBWeightedRandom
	}
	registry := providers.NewRegistryWithStrategy(fallback, strategy)
	logger.Info("Load balancing strategy", zap.String("strategy", string(strategy)))

	// Register providers from config
	var copilotProv *copilotProvider.Provider
	var kiroProv *kiro.Provider
	for _, pc := range gwCfg.Providers {
		if !pc.Enabled {
			logger.Info("Skipping disabled provider", zap.String("name", pc.Name))
			continue
		}

		p, err := createProvider(pc, logger)
		if err != nil {
			logger.Error("Failed to create provider", zap.String("name", pc.Name), zap.Error(err))
			continue
		}

		// Track copilot provider for admin API
		if cp, ok := p.(*copilotProvider.Provider); ok {
			copilotProv = cp
		}

		// Track kiro provider for admin API
		if kp, ok := p.(*kiro.Provider); ok {
			kiroProv = kp
		}

		registry.RegisterWithConfig(p, pc.Weight, pc.Models)
		logger.Info("Registered provider",
			zap.String("name", pc.Name),
			zap.String("type", pc.Type),
			zap.Int("weight", pc.Weight),
			zap.Int("models", len(pc.Models)),
		)
	}

	// Start background health checks (every 30 seconds)
	registry.StartHealthCheck(30 * time.Second)

	// Initialize tenant store (if multi-tenant enabled)
	routerCfg := routes.RouterConfig{
		Registry:        registry,
		Logger:          logger,
		APIKey:          gwCfg.Auth.APIKey,
		AdminKey:        gwCfg.Auth.AdminKey,
		CopilotProvider: copilotProv,
		KiroProvider:    kiroProv,
	}

	if gwCfg.Tenant.Enabled {
		dbPath := gwCfg.Tenant.DBPath
		if dbPath == "" {
			dbPath = "antigateway_tenant.db"
		}
		store, err := tenant.NewStore(dbPath)
		if err != nil {
			log.Fatalf("Failed to init tenant store: %v", err)
		}
		defer store.Close()

		routerCfg.Store = store
		routerCfg.RateLimiter = tenant.NewRateLimiter()
		logger.Info("Multi-tenant mode enabled", zap.String("db", dbPath))
	}

	// Setup Gin router
	r := routes.SetupRouter(routerCfg)

	// Graceful shutdown
	addr := fmt.Sprintf("%s:%d", gwCfg.Server.Host, gwCfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// Start server in goroutine
	go func() {
		logger.Info("Server listening", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server error", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("Shutting down server...", zap.String("signal", sig.String()))

	// Give active connections 30 seconds to finish
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
		return err
	}

	logger.Info("Server exited gracefully")
	return nil
}

// createProvider instantiates an AIProvider from config.
func createProvider(pc config.ProviderConfig, logger *zap.Logger) (providers.AIProvider, error) {
	switch pc.Type {
	case "kiro":
		return kiro.NewProvider(logger), nil

	case "openai", "openai-compat":
		if pc.BaseURL == "" {
			if pc.Type == "openai" {
				pc.BaseURL = "https://api.openai.com/v1"
			} else {
				return nil, fmt.Errorf("base_url is required for openai-compat provider %q", pc.Name)
			}
		}
		return openaiProvider.NewProvider(openaiProvider.Config{
			Name:         pc.Name,
			BaseURL:      pc.BaseURL,
			APIKey:       pc.APIKey,
			DefaultModel: pc.DefaultModel,
			Logger:       logger,
		}), nil

	case "copilot":
		return copilotProvider.NewProvider(copilotProvider.Config{
			Name:         pc.Name,
			GithubTokens: pc.GithubTokens,
			Logger:       logger,
		}), nil

	case "anthropic":
		if pc.APIKey == "" {
			return nil, fmt.Errorf("api_key is required for anthropic provider %q", pc.Name)
		}
		return anthropicProvider.NewProvider(anthropicProvider.Config{
			Name:         pc.Name,
			BaseURL:      pc.BaseURL,
			APIKey:       pc.APIKey,
			DefaultModel: pc.DefaultModel,
			Logger:       logger,
		}), nil

	default:
		return nil, fmt.Errorf("unknown provider type: %q", pc.Type)
	}
}
