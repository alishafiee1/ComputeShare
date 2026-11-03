package setup

import (
	"strings"
	"testing"
)

const sample = `
  Power Setting GUID: 29f6c1db-86da-48c5-9fdb-f2b67b1f44da  (Sleep after)
  Current AC Power Setting Index: 0x00003840
  Current DC Power Setting Index: 0x00000258
`

// umbrella card 017 R5: the standby timeouts are read from powercfg output.
func TestParsePowercfg(t *testing.T) {
	ac, dc, ok := ParsePowercfg(sample)
	if !ok || ac != 0x3840 || dc != 0x258 {
		t.Fatalf("ac=%d dc=%d ok=%v", ac, dc, ok)
	}
	if _, _, ok := ParsePowercfg("nothing useful"); ok {
		t.Fatal("parsed garbage")
	}
}

func TestSleepAdvice(t *testing.T) {
	if got := SleepAdvice(0, 600); got != "" {
		t.Fatalf("never-sleep-on-AC must give no warning, got %q", got)
	}
	got := SleepAdvice(0x3840, 0x258)
	if !strings.Contains(got, "4h0m0s") || !strings.Contains(got, "10m0s") || !strings.Contains(got, "powercfg /change standby-timeout-ac 0") {
		t.Fatalf("advice: %q", got)
	}
}
