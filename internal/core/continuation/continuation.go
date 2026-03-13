package continuation

import (
	"strings"
	"unicode/utf8"

	"github.com/SilkageNet/anti-gateway/internal/models"
)

const (
	MaxContinuations          = 5
	MinOutputForContinuation  = 4000 // characters
	InputOutputRatioThreshold = 8
)

const ContinuationPrompt = "Your previous response was truncated by the system at the output limit. The content is INCOMPLETE. You MUST continue outputting from EXACTLY where you left off — pick up from the very last character. Do NOT summarize, do NOT add commentary, do NOT say 'let me know if you need more'. Just output the remaining content until it is genuinely finished."

// ShouldAutoContinue implements the three-layer heuristic to determine
// if the output was genuinely truncated and needs continuation.
func ShouldAutoContinue(outputText string, inputMessages []models.ChatMessage) bool {
	// Layer 1: Output too short → don't continue
	if utf8.RuneCountInString(outputText) < MinOutputForContinuation {
		return false
	}

	// Layer 2: Input/output ratio — if input >> output, it was likely input that filled context
	inputLen := 0
	for _, msg := range inputMessages {
		if s, ok := msg.Content.(string); ok {
			inputLen += utf8.RuneCountInString(s)
		}
	}
	outputLen := utf8.RuneCountInString(outputText)
	if inputLen > outputLen*InputOutputRatioThreshold {
		return false
	}

	// Layer 3: Ending structure analysis
	stripped := strings.TrimSpace(outputText)
	if stripped == "" {
		return false
	}

	lastChar, _ := utf8.DecodeLastRuneInString(stripped)

	// Natural ending punctuation → don't continue
	if strings.ContainsRune(".。!！?？…", lastChar) {
		return false
	}

	// Closing brackets/quotes → don't continue
	if strings.ContainsRune(")）]】」》\"\"''", lastChar) {
		return false
	}

	// Markdown block endings
	lines := strings.Split(stripped, "\n")
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if lastLine == "```" || lastLine == "---" || lastLine == "***" || lastLine == "===" {
		return false
	}

	// Trailing double newline → natural paragraph end
	trimmed := strings.TrimRight(outputText, " \t")
	if strings.HasSuffix(trimmed, "\n\n") {
		return false
	}

	// Passed all checks → truly truncated
	return true
}

// BuildContinuationMessages appends the continuation prompt to the original messages.
func BuildContinuationMessages(original []models.ChatMessage, outputSoFar string) []models.ChatMessage {
	msgs := make([]models.ChatMessage, len(original))
	copy(msgs, original)

	msgs = append(msgs,
		models.ChatMessage{
			Role:    "assistant",
			Content: outputSoFar,
		},
		models.ChatMessage{
			Role:    "user",
			Content: ContinuationPrompt,
		},
	)
	return msgs
}
