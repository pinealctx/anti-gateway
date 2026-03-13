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

// Provider implements the AIProvider interface for Kiro/CodeWhisperer.
type Provider struct {
	tokenMgr *TokenManager
	client   *CWClient
	authMgr  *KiroAuthManager
	logger   *zap.Logger
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

// SetLoginToken injects a token from the built-in PKCE login and starts background refresh.
func (p *Provider) SetLoginToken(lt *LoginToken) {
	p.tokenMgr.SetLoginToken(lt)
	// Ensure background refresh is running for the login token
	p.tokenMgr.StartBackgroundRefresh(2 * time.Minute)
}

// TokenStatus returns information about the current token state.
func (p *Provider) TokenStatus() map[string]interface{} {
	p.tokenMgr.mu.RLock()
	defer p.tokenMgr.mu.RUnlock()

	status := map[string]interface{}{
		"has_login":   p.tokenMgr.loginToken != nil,
		"has_current": p.tokenMgr.current != nil,
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
		p.StreamCompletion(ctx, req, stream)
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
					Content:   fullContent,
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

	cwReq, err := converter.OpenAIToCW(req, "")
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
