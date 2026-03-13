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

	"github.com/pinealctx/anti-gateway/internal/api/routes"
	"github.com/pinealctx/anti-gateway/internal/config"
	"github.com/pinealctx/anti-gateway/internal/core/providers"
	anthropicProvider "github.com/pinealctx/anti-gateway/internal/providers/anthropic"
	copilotProvider "github.com/pinealctx/anti-gateway/internal/providers/copilot"
	"github.com/pinealctx/anti-gateway/internal/providers/kiro"
	openaiProvider "github.com/pinealctx/anti-gateway/internal/providers/openai"
	"github.com/pinealctx/anti-gateway/internal/tenant"
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
	defer func() { _ = logger.Sync() }()

	logger.Info("Starting AntiGateway",
		zap.String("version", config.Version),
		zap.String("host", gwCfg.Server.Host),
		zap.Int("port", gwCfg.Server.Port),
		zap.Bool("tenant_auth", gwCfg.Tenant.Enabled),
	)

	// Initialize provider registry
	fallback := gwCfg.Defaults.Provider
	strategy := providers.LBStrategy(gwCfg.Defaults.LBStrategy)
	if strategy == "" {
		strategy = providers.LBWeightedRandom
	}
	registry := providers.NewRegistryWithStrategy(fallback, strategy)
	logger.Info("Load balancing strategy", zap.String("strategy", string(strategy)))

	// SQLite store — always created for provider & key management
	dbPath := gwCfg.Tenant.DBPath
	if dbPath == "" {
		dbPath = "antigateway.db"
	}
	store, err := tenant.NewStore(dbPath)
	if err != nil {
		log.Fatalf("Failed to init store: %v", err)
	}
	defer func() { _ = store.Close() }()
	logger.Info("Store initialized", zap.String("db", dbPath))

	// Load dynamically-managed providers from DB
	for _, rec := range store.ListProviderRecords() {
		if !rec.Enabled {
			logger.Info("Skipping disabled provider", zap.String("name", rec.Name))
			continue
		}
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
		p, err := createProvider(pc, logger)
		if err != nil {
			logger.Error("Failed to create provider", zap.String("name", rec.Name), zap.Error(err))
			continue
		}
		registry.RegisterWithConfig(p, rec.Weight, rec.Models)
		logger.Info("Registered provider",
			zap.String("name", rec.Name),
			zap.String("type", rec.Type),
			zap.Int("weight", rec.Weight),
		)
	}

	// Start background health checks (every 30 seconds)
	registry.StartHealthCheck(30 * time.Second)

	// Inject store into Kiro providers and restore persisted tokens
	for _, p := range registry.All() {
		if kp, ok := p.(*kiro.Provider); ok {
			kp.SetStore(store)
			if kp.RestoreToken() {
				logger.Info("Kiro token restored from persistent storage")
			}
		}
	}

	// Build router config
	routerCfg := routes.RouterConfig{
		Registry:        registry,
		Logger:          logger,
		APIKey:          gwCfg.Auth.APIKey,
		AdminKey:        gwCfg.Auth.AdminKey,
		Store:           store,
		TenantAuth:      gwCfg.Tenant.Enabled,
		RateLimiter:     tenant.NewRateLimiter(),
		CORSOrigins:     gwCfg.Server.CORSOrigins,
		ProviderFactory: createProvider,
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
		p := kiro.NewProvider(pc.Name, logger)
		return p, nil

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
