package blog

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedaction(t *testing.T) {
	var b bytes.Buffer
	New(&b).Info("x", "token", "cs1.abc.SECRET", "password", "Shiraz-2026-gpu", "key", "k", "node", "myBrotherPc")
	s := b.String()
	if strings.Contains(s, "SECRET") || strings.Contains(s, "Shiraz") || !strings.Contains(s, "myBrotherPc") || !strings.HasPrefix(s, "{") {
		t.Fatal(s)
	}
}
