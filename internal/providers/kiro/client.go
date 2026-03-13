package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/SilkageNet/anti-gateway/internal/core/eventstream"
	"github.com/SilkageNet/anti-gateway/internal/models"
	"go.uber.org/zap"
)

const (
	cwEndpoint  = "https://q.us-east-1.amazonaws.com/generateAssistantResponse"
	cwTarget    = "AmazonCodeWhispererStreamingService.GenerateAssistantResponse"
	cwUserAgent = "kiro-cli-chat-macos-aarch64-1.27.2"

	maxRetries = 3
)

// retryBackoff defines the backoff durations for retries: [1s, 3s, 10s].
var retryBackoff = []time.Duration{1 * time.Second, 3 * time.Second, 10 * time.Second}

// CWClient handles HTTP communication with the CodeWhisperer backend.
type CWClient struct {
	client *http.Client
	logger *zap.Logger
}

func NewCWClient(logger *zap.Logger) *CWClient {
	return &CWClient{
		client: &http.Client{Timeout: 5 * time.Minute},
		logger: logger,
	}
}

// CWStreamEvent represents a parsed event from the CW response stream.
type CWStreamEvent struct {
	Type         string // "text", "tool_use", "context_usage", "exception", "end"
	Content      string
	ToolUse      *CWToolUseAccumulator
	ContextUsage float64
	Error        error
}

type CWToolUseAccumulator struct {
	ToolUseID string
	Name      string
	Input     any
}

// GenerateStream sends a request to CW and returns an event channel.
// Retries up to maxRetries times on 5xx errors or timeouts with exponential backoff.
func (c *CWClient) GenerateStream(ctx context.Context, cwReq *models.CWRequest, token *TokenInfo) (<-chan CWStreamEvent, error) {
	bodyBytes, err := json.Marshal(cwReq)
	if err != nil {
		return nil, fmt.Errorf("marshal cw request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryBackoff[attempt-1]
			c.logger.Warn("retrying CW request",
				zap.Int("attempt", attempt),
				zap.Duration("backoff", delay),
				zap.Error(lastErr),
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", cwEndpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/x-amz-json-1.0")
		req.Header.Set("x-amz-target", cwTarget)
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		req.Header.Set("User-Agent", cwUserAgent)
		if token.IsExternalIdP {
			req.Header.Set("TokenType", "EXTERNAL_IDP")
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("cw request failed: %w", err)
			continue // retry on network/timeout errors
		}

		if resp.StatusCode >= 500 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("cw returned %d: %s", resp.StatusCode, string(body))
			continue // retry on 5xx
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("cw returned %d: %s", resp.StatusCode, string(body))
		}

		events := make(chan CWStreamEvent, 32)
		go c.processStream(resp.Body, events)
		return events, nil
	}

	return nil, fmt.Errorf("cw request failed after %d retries: %w", maxRetries, lastErr)
}

func (c *CWClient) processStream(body io.ReadCloser, out chan<- CWStreamEvent) {
	defer close(out)
	defer body.Close()

	rawEvents := make(chan eventstream.Event, 32)
	go func() {
		if err := eventstream.ParseStreamingResponse(body, rawEvents); err != nil {
			c.logger.Error("eventstream parse error", zap.Error(err))
		}
	}()

	var activeTool *struct {
		ID       string
		Name     string
		InputBuf string
	}

	for raw := range rawEvents {
		if raw.MessageType == "exception" {
			var exc models.CWExceptionEvent
			json.Unmarshal(raw.Payload, &exc)
			out <- CWStreamEvent{Type: "exception", Error: fmt.Errorf("CW exception: %s", exc.Message)}
			continue
		}

		switch raw.EventType {
		case "assistantResponseEvent":
			var evt models.CWAssistantResponseEvent
			if err := json.Unmarshal(raw.Payload, &evt); err == nil {
				out <- CWStreamEvent{Type: "text", Content: evt.Content}
			}

		case "toolUse":
			// Legacy: complete tool use in one event
			var evt struct {
				ToolUseID string `json:"toolUseId"`
				Name      string `json:"name"`
				Input     any    `json:"input"`
			}
			if err := json.Unmarshal(raw.Payload, &evt); err == nil {
				out <- CWStreamEvent{
					Type: "tool_use",
					ToolUse: &CWToolUseAccumulator{
						ToolUseID: evt.ToolUseID,
						Name:      evt.Name,
						Input:     evt.Input,
					},
				}
			}

		case "toolUseEvent":
			// Streaming tool use: accumulate chunks
			var evt models.CWToolUseEvent
			if err := json.Unmarshal(raw.Payload, &evt); err != nil {
				continue
			}
			if evt.Name != "" && evt.ToolUseID != "" {
				// New tool
				activeTool = &struct {
					ID       string
					Name     string
					InputBuf string
				}{ID: evt.ToolUseID, Name: evt.Name}
			}
			if activeTool != nil && evt.Input != "" {
				activeTool.InputBuf += evt.Input
			}
			if evt.Stop && activeTool != nil {
				var input any
				json.Unmarshal([]byte(activeTool.InputBuf), &input)
				out <- CWStreamEvent{
					Type: "tool_use",
					ToolUse: &CWToolUseAccumulator{
						ToolUseID: activeTool.ID,
						Name:      activeTool.Name,
						Input:     input,
					},
				}
				activeTool = nil
			}

		case "contextUsageEvent":
			var evt models.CWContextUsageEvent
			if err := json.Unmarshal(raw.Payload, &evt); err == nil {
				out <- CWStreamEvent{Type: "context_usage", ContextUsage: evt.ContextUsagePercentage}
			}

		case "supplementaryWebLinksEvent", "meteringEvent":
			// Ignored

		default:
			c.logger.Debug("unknown cw event", zap.String("type", raw.EventType))
		}
	}

	out <- CWStreamEvent{Type: "end"}
}
