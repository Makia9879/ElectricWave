// Package logging configures structured (slog) logging with redaction helpers.
//
// Hard rule enforced across the codebase: the Authorization header, raw tokens
// and full notification bodies never enter log output. When a body fragment is
// genuinely needed for debugging it must be passed through TruncateBody first.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New constructs a JSON slog.Logger writing to stdout at INFO level. JSON keeps
// the output machine-parseable for the redaction grep tests.
func New() *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h)
}

// TruncateBody clamps a string to at most max runes, appending an ellipsis when
// truncated. Used only when a body fragment must be logged for debugging.
func TruncateBody(s string, max int) string {
	if max < 0 {
		max = 0
	}
	r := []rune(s)
	if len(r) <= max {
		return string(r)
	}
	if max == 0 {
		return "…"
	}
	return string(r[:max]) + "…"
}

// MaskID returns a short non-sensitive hint (first 8 chars) for an identifier
// such as a token id, useful in logs without exposing the secret itself.
func MaskID(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + "…" + s[len(s)-2:]
}
