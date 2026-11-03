// Package blog provides structured JSON logs with redaction (broker 004 R4).
package blog

import (
	"io"
	"log/slog"
	"strings"
)

var redacted = map[string]bool{"token": true, "password": true, "key": true, "secret": true, "passwd": true, "sig": true}

func New(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if redacted[strings.ToLower(a.Key)] {
			return slog.String(a.Key, "[REDACTED]")
		}
		return a
	}}))
}
