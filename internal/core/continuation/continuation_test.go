package continuation

import (
	"strings"
	"testing"

	"github.com/SilkageNet/anti-gateway/internal/models"
)

// ============================================================
// ShouldAutoContinue
// ============================================================

func TestShouldAutoContinue_ShortOutput_False(t *testing.T) {
	// Under MinOutputForContinuation chars
	output := strings.Repeat("x", MinOutputForContinuation-1)
	if ShouldAutoContinue(output, nil) {
		t.Error("short output should not trigger continuation")
	}
}

func TestShouldAutoContinue_HighInputRatio_False(t *testing.T) {
	// Input >> output (ratio > 8x)
	output := strings.Repeat("x", 5000)
	msgs := []models.ChatMessage{
		{Role: "user", Content: strings.Repeat("y", 50000)},
	}
	if ShouldAutoContinue(output, msgs) {
		t.Error("high input/output ratio should prevent continuation")
	}
}

func TestShouldAutoContinue_EndsWithPeriod_False(t *testing.T) {
	output := strings.Repeat("x", 5000) + "."
	if ShouldAutoContinue(output, nil) {
		t.Error("output ending with '.' should not continue")
	}
}

func TestShouldAutoContinue_EndsWithExclamation_False(t *testing.T) {
	output := strings.Repeat("x", 5000) + "!"
	if ShouldAutoContinue(output, nil) {
		t.Error("output ending with '!' should not continue")
	}
}

func TestShouldAutoContinue_EndsWithQuestion_False(t *testing.T) {
	output := strings.Repeat("x", 5000) + "?"
	if ShouldAutoContinue(output, nil) {
		t.Error("output ending with '?' should not continue")
	}
}

func TestShouldAutoContinue_EndsWithClosingBracket_False(t *testing.T) {
	output := strings.Repeat("x", 5000) + ")"
	if ShouldAutoContinue(output, nil) {
		t.Error("output ending with ')' should not continue")
	}
}

func TestShouldAutoContinue_EndsWithCodeFence_False(t *testing.T) {
	output := strings.Repeat("x", 5000) + "\n```"
	if ShouldAutoContinue(output, nil) {
		t.Error("output ending with ``` should not continue")
	}
}

func TestShouldAutoContinue_EndsWithDoubleNewline_False(t *testing.T) {
	output := strings.Repeat("x", 5000) + "\n\n"
	if ShouldAutoContinue(output, nil) {
		t.Error("output ending with double newline should not continue")
	}
}

func TestShouldAutoContinue_TruncatedMidWord_True(t *testing.T) {
	// Long output ending mid-word — should continue
	output := strings.Repeat("x", 5000) + "truncated mid-wor"
	if !ShouldAutoContinue(output, nil) {
		t.Error("truncated mid-word output should trigger continuation")
	}
}

func TestShouldAutoContinue_TruncatedMidCode_True(t *testing.T) {
	// Long code block that doesn't close
	output := strings.Repeat("x", 5000) + "\n```go\nfunc main() {\n    fmt.Println("
	if !ShouldAutoContinue(output, nil) {
		t.Error("unclosed code block should trigger continuation")
	}
}

func TestShouldAutoContinue_EmptyAfterTrim_False(t *testing.T) {
	output := strings.Repeat(" ", 5000)
	if ShouldAutoContinue(output, nil) {
		t.Error("whitespace-only output should not continue")
	}
}

// ============================================================
// BuildContinuationMessages
// ============================================================

func TestBuildContinuationMessages(t *testing.T) {
	original := []models.ChatMessage{
		{Role: "user", Content: "Write a long essay"},
	}
	got := BuildContinuationMessages(original, "partial output...")
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	// First should be original
	if got[0].Role != "user" {
		t.Error("first should be original user message")
	}
	// Second: assistant with partial output
	if got[1].Role != "assistant" {
		t.Error("second should be assistant")
	}
	if got[1].Content != "partial output..." {
		t.Errorf("assistant content = %q", got[1].Content)
	}
	// Third: user with continuation prompt
	if got[2].Role != "user" {
		t.Error("third should be user")
	}
	if got[2].Content != ContinuationPrompt {
		t.Error("third should be continuation prompt")
	}
}

func TestBuildContinuationMessages_DoesNotMutateOriginal(t *testing.T) {
	original := []models.ChatMessage{
		{Role: "user", Content: "hello"},
	}
	origLen := len(original)
	BuildContinuationMessages(original, "output")
	if len(original) != origLen {
		t.Error("original slice should not be mutated")
	}
}

// ============================================================
// Constants
// ============================================================

func TestConstants(t *testing.T) {
	if MaxContinuations != 5 {
		t.Errorf("MaxContinuations = %d, want 5", MaxContinuations)
	}
	if MinOutputForContinuation != 4000 {
		t.Errorf("MinOutputForContinuation = %d, want 4000", MinOutputForContinuation)
	}
	if InputOutputRatioThreshold != 8 {
		t.Errorf("InputOutputRatioThreshold = %d, want 8", InputOutputRatioThreshold)
	}
}
