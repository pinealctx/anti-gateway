package logutil

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var (
	jsonSecretPattern = regexp.MustCompile(`(?i)"(api_key|github_token|access_token|refresh_token|client_secret|authorization|password|token)"\s*:\s*"([^"]*)"`)
	formSecretPattern = regexp.MustCompile(`(?i)(api_key|github_token|access_token|refresh_token|client_secret|authorization|password|token)=([^&\s]+)`)
)

// RedactString masks common secret fields in JSON/form-like text.
func RedactString(s string) string {
	out := jsonSecretPattern.ReplaceAllString(s, `"$1":"***"`)
	out = formSecretPattern.ReplaceAllString(out, `$1=***`)
	return out
}

// TruncateString truncates content to at most limit bytes for safe logging.
func TruncateString(s string, limit int) (string, bool) {
	if limit <= 0 || len(s) <= limit {
		return s, false
	}
	return s[:limit], true
}

// RedactHeaders sanitizes sensitive headers for debug logging.
func RedactHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vals := range h {
		v := strings.Join(vals, ",")
		switch strings.ToLower(k) {
		case "authorization", "x-api-key", "cookie", "set-cookie":
			out[k] = "***"
		default:
			out[k] = v
		}
	}
	return out
}

// WithTruncationSuffix appends a marker indicating body truncation.
func WithTruncationSuffix(s string, truncated bool, totalLen int, limit int) string {
	if !truncated {
		return s
	}
	return fmt.Sprintf("%s\n...[truncated %d bytes, total=%d]", s, totalLen-limit, totalLen)
}
