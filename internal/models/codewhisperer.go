package models

// ========================
// CodeWhisperer (CW) types — Kiro backend
// ========================

type CWRequest struct {
	ConversationState CWConversationState `json:"conversationState"`
	ProfileArn        string              `json:"profileArn,omitempty"`
}

type CWConversationState struct {
	ChatTriggerType string           `json:"chatTriggerType"`
	ConversationID  string           `json:"conversationId"`
	CurrentMessage  CWCurrentMsg     `json:"currentMessage"`
	History         []CWHistoryEntry `json:"history,omitempty"`
}

type CWCurrentMsg struct {
	UserInputMessage CWUserInputMessage `json:"userInputMessage"`
}

type CWUserInputMessage struct {
	Content                 string            `json:"content"`
	ModelID                 string            `json:"modelId"`
	Origin                  string            `json:"origin"`
	UserInputMessageContext *CWMessageContext `json:"userInputMessageContext,omitempty"`
	Images                  []CWImage         `json:"images,omitempty"`
}

type CWMessageContext struct {
	ToolResults []CWToolResult `json:"toolResults,omitempty"`
	Tools       []CWTool       `json:"tools,omitempty"`
}

type CWToolResult struct {
	ToolUseID string                `json:"toolUseId"`
	Content   []CWToolResultContent `json:"content"`
	Status    string                `json:"status"`
}

type CWToolResultContent struct {
	Text string `json:"text,omitempty"`
}

type CWTool struct {
	ToolSpecification CWToolSpec `json:"toolSpecification"`
}

type CWToolSpec struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	InputSchema CWInputSchema `json:"inputSchema"`
}

type CWInputSchema struct {
	JSON any `json:"json"` // JSON Schema object
}

type CWImage struct {
	Format string        `json:"format"`
	Source CWImageSource `json:"source"`
}

type CWImageSource struct {
	Bytes string `json:"bytes"` // base64
}

// History entries
type CWHistoryEntry struct {
	UserInputMessage         *CWUserInputMessage         `json:"userInputMessage,omitempty"`
	AssistantResponseMessage *CWAssistantResponseMessage `json:"assistantResponseMessage,omitempty"`
}

type CWAssistantResponseMessage struct {
	MessageID string      `json:"messageId,omitempty"`
	Content   string      `json:"content"`
	ToolUses  []CWToolUse `json:"toolUses,omitempty"`
}

type CWToolUse struct {
	ToolUseID string `json:"toolUseId"`
	Name      string `json:"name"`
	Input     any    `json:"input"`
}

// ========================
// CW streaming event types
// ========================

type CWAssistantResponseEvent struct {
	Content string `json:"content"`
}

type CWToolUseEvent struct {
	ToolUseID string `json:"toolUseId,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     string `json:"input,omitempty"`
	Stop      bool   `json:"stop,omitempty"`
}

type CWContextUsageEvent struct {
	ContextUsagePercentage float64 `json:"contextUsagePercentage"`
}

type CWExceptionEvent struct {
	Message string `json:"message"`
}
