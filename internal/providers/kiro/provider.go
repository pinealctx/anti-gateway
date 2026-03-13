package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SilkageNet/anti-gateway/internal/core/converter"
	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	"github.com/SilkageNet/anti-gateway/internal/core/sanitizer"
	"github.com/SilkageNet/anti-gateway/internal/models"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// KVStore is a minimal interface for key-value persistence (implemented by tenant.Store).
type KVStore interface {
	GetKV(key string) (string, bool)
	SetKV(key, value string) error
}

const kvKeyKiroToken = "kiro:login_token"

// Provider implements the AIProvider interface for Kiro/CodeWhisperer.
type Provider struct {
	tokenMgr   *TokenManager
	client     *CWClient
	authMgr    *KiroAuthManager
	logger     *zap.Logger
	profileArn string
	kvStore    KVStore
}

// NewProvider creates a Kiro provider using the built-in PKCE login flow.
func NewProvider(logger *zap.Logger) *Provider {
	tm := NewTokenManager(logger)
	tm.StartBackgroundRefresh(2 * time.Minute)

	return &Provider{
		tokenMgr: tm,
		client:   NewCWClient(logger),
		authMgr:  NewKiroAuthManager(logger),
		logger:   logger,
	}
}

// AuthMgr returns the Kiro auth manager for PKCE login management.
func (p *Provider) AuthMgr() *KiroAuthManager {
	return p.authMgr
}

// SetStore injects a KV store for token persistence.
func (p *Provider) SetStore(store KVStore) {
	p.kvStore = store
}

// SetLoginToken injects a token from the built-in PKCE login.
func (p *Provider) SetLoginToken(lt *LoginToken) {
	p.tokenMgr.SetLoginToken(lt)
	p.profileArn = lt.ProfileArn
	p.persistToken(lt)
}

// persistedToken is the JSON-serializable form stored in the DB.
type persistedToken struct {
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token"`
	ClientID      string    `json:"client_id"`
	TokenEndpoint string    `json:"token_endpoint"`
	ExpiresAt     time.Time `json:"expires_at"`
	IsExternalIdP bool      `json:"is_external_idp"`
	RefreshScope  string    `json:"refresh_scope,omitempty"`
	ProfileArn    string    `json:"profile_arn,omitempty"`
}

func (p *Provider) persistToken(lt *LoginToken) {
	if p.kvStore == nil {
		return
	}
	pt := persistedToken{
		AccessToken:   lt.AccessToken,
		RefreshToken:  lt.RefreshToken,
		ClientID:      lt.ClientID,
		TokenEndpoint: lt.TokenEndpoint,
		ExpiresAt:     lt.ExpiresAt,
		IsExternalIdP: lt.IsExternalIdP,
		RefreshScope:  lt.RefreshScope,
		ProfileArn:    lt.ProfileArn,
	}
	data, err := json.Marshal(pt)
	if err != nil {
		p.logger.Error("failed to marshal token for persistence", zap.Error(err))
		return
	}
	if err := p.kvStore.SetKV(kvKeyKiroToken, string(data)); err != nil {
		p.logger.Error("failed to persist kiro token", zap.Error(err))
	}
}

// RestoreToken loads a previously persisted token from the KV store.
// Returns true if a token was successfully restored.
func (p *Provider) RestoreToken() bool {
	if p.kvStore == nil {
		return false
	}
	data, ok := p.kvStore.GetKV(kvKeyKiroToken)
	if !ok || data == "" {
		return false
	}
	var pt persistedToken
	if err := json.Unmarshal([]byte(data), &pt); err != nil {
		p.logger.Error("failed to unmarshal persisted token", zap.Error(err))
		return false
	}

	lt := &LoginToken{
		AccessToken:   pt.AccessToken,
		RefreshToken:  pt.RefreshToken,
		ClientID:      pt.ClientID,
		TokenEndpoint: pt.TokenEndpoint,
		ExpiresAt:     pt.ExpiresAt,
		IsExternalIdP: pt.IsExternalIdP,
		RefreshScope:  pt.RefreshScope,
		ProfileArn:    pt.ProfileArn,
	}
	p.tokenMgr.SetLoginToken(lt)
	p.profileArn = pt.ProfileArn
	p.logger.Info("kiro token restored from DB",
		zap.Bool("has_refresh", pt.RefreshToken != ""),
		zap.String("profile_arn", pt.ProfileArn),
		zap.Time("expires_at", pt.ExpiresAt))
	return true
}

// ForceRefresh forces a token refresh and re-persists the updated token.
func (p *Provider) ForceRefresh() error {
	_, err := p.tokenMgr.ForceRefresh()
	if err != nil {
		return err
	}
	// Re-persist the updated login token
	p.tokenMgr.mu.RLock()
	lt := p.tokenMgr.loginToken
	p.tokenMgr.mu.RUnlock()
	if lt != nil {
		p.persistToken(lt)
	}
	return nil
}

// TokenStatus returns information about the current token state.
func (p *Provider) TokenStatus() map[string]any {
	p.tokenMgr.mu.RLock()
	defer p.tokenMgr.mu.RUnlock()

	status := map[string]any{
		"has_login":   p.tokenMgr.loginToken != nil,
		"has_current": p.tokenMgr.current != nil,
		"profile_arn": p.profileArn,
	}
	if p.tokenMgr.current != nil {
		status["expires_at"] = p.tokenMgr.current.ExpiresAt
		status["is_external_idp"] = p.tokenMgr.current.IsExternalIdP
	}
	return status
}

func (p *Provider) Name() string { return "kiro" }

func (p *Provider) ChatCompletion(ctx context.Context, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
	// Use streaming internally and collect the full response
	stream := make(chan providers.StreamChunk, 64)
	go func() {
		_ = p.StreamCompletion(ctx, req, stream)
	}()

	var fullContent string
	var toolCalls []models.ToolCall
	var finishReason string

	for chunk := range stream {
		if chunk.Error != nil {
			return nil, chunk.Error
		}
		fullContent += chunk.Content
		if len(chunk.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.ToolCalls...)
		}
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
	}

	if finishReason == "" {
		finishReason = "stop"
	}

	return &models.ChatCompletionResponse{
		ID:      "chatcmpl-" + uuid.New().String()[:8],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []models.ChatCompletionChoice{
			{
				Index: 0,
				Message: models.ChatMessage{
					Role:      "assistant",
					Content:   models.RawString(fullContent),
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
	}, nil
}

func (p *Provider) StreamCompletion(ctx context.Context, req *models.ChatCompletionRequest, stream chan<- providers.StreamChunk) error {
	defer close(stream)

	token, err := p.tokenMgr.GetToken()
	if err != nil {
		stream <- providers.StreamChunk{Error: fmt.Errorf("token error: %w", err)}
		return err
	}

	cwReq, err := converter.OpenAIToCW(req, p.profileArn)
	if err != nil {
		stream <- providers.StreamChunk{Error: fmt.Errorf("conversion error: %w", err)}
		return err
	}

	events, err := p.client.GenerateStream(ctx, cwReq, token)
	if err != nil {
		stream <- providers.StreamChunk{Error: fmt.Errorf("cw stream error: %w", err)}
		return err
	}

	for evt := range events {
		switch evt.Type {
		case "text":
			sanitized := sanitizer.SanitizeText(evt.Content, true)
			if sanitized != "" {
				stream <- providers.StreamChunk{Content: sanitized}
			}

		case "tool_use":
			if evt.ToolUse != nil && !sanitizer.IsBuiltinTool(evt.ToolUse.Name) {
				inputJSON, _ := json.Marshal(evt.ToolUse.Input)
				stream <- providers.StreamChunk{
					ToolCalls: []models.ToolCall{
						{
							ID:   evt.ToolUse.ToolUseID,
							Type: "function",
							Function: models.ToolCallFunction{
								Name:      evt.ToolUse.Name,
								Arguments: string(inputJSON),
							},
						},
					},
				}
			}

		case "context_usage":
			if evt.ContextUsage > 0.95 {
				stream <- providers.StreamChunk{FinishReason: "length"}
			}

		case "exception":
			if evt.Error != nil {
				stream <- providers.StreamChunk{Error: evt.Error}
			}

		case "end":
			// Stream complete
		}
	}

	return nil
}

func (p *Provider) RefreshToken(ctx context.Context) error {
	_, err := p.tokenMgr.refresh()
	return err
}

func (p *Provider) IsHealthy(ctx context.Context) bool {
	_, err := p.tokenMgr.GetToken()
	return err == nil
}
