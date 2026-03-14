package sanitizer

import (
	"strings"
	"testing"
)

func BenchmarkSanitizeText_Short(b *testing.B) {
	text := "Hello, I'm Kiro. Let me help with CodeWhisperer."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SanitizeText(text, false)
	}
}

func BenchmarkSanitizeText_LargeBlock(b *testing.B) {
	text := strings.Repeat("Line of code content here\n", 500)
	text += "<function_calls><invoke name=\"tool\"><parameter>val</parameter></invoke></function_calls>\n"
	text += "I'm Kiro and I use Amazon Q.\n"
	text += strings.Repeat("More content\n", 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SanitizeText(text, false)
	}
}

func BenchmarkBuildSystemPrompt(b *testing.B) {
	userSystem := strings.Repeat("System context here. ", 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildSystemPrompt(userSystem, true)
	}
}
