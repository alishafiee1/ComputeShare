package setup

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A laptop that goes to sleep drops the tunnel; the broker then shows the node offline for hours.
// Phase 1 of the end-to-end run saw a 2 h 47 min outage that matches AC standby 4 h / DC 10 min
// (umbrella card 017 R5). Inside WSL the Windows power plan is readable with powercfg.exe.

var powerRe = regexp.MustCompile(`(?i)Current (AC|DC) Power Setting Index:\s*(0x[0-9a-f]+)`)

// ParsePowercfg reads the AC and DC standby timeouts (seconds) from `powercfg /query` output.
func ParsePowercfg(out string) (ac, dc int64, ok bool) {
	var gotAC, gotDC bool
	for _, m := range powerRe.FindAllStringSubmatch(out, -1) {
		v, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(m[2]), "0x"), 16, 64)
		if err != nil {
			continue
		}
		if strings.EqualFold(m[1], "AC") {
			ac, gotAC = v, true
		} else {
			dc, gotDC = v, true
		}
	}
	return ac, dc, gotAC && gotDC
}

// SleepAdvice returns a warning when the machine can sleep while on AC power, "" when it cannot.
// Sleep on battery is only mentioned: a laptop on battery is expected to sleep.
func SleepAdvice(ac, dc int64) string {
	if ac == 0 {
		return ""
	}
	msg := fmt.Sprintf("the computer can sleep after %s on AC power (and %s on battery), and the node goes offline while it sleeps. "+
		"Set it to never on AC: Windows Settings > System > Power, or run in a Windows terminal: powercfg /change standby-timeout-ac 0",
		timeout(ac), timeout(dc))
	return msg
}

func timeout(sec int64) string {
	if sec == 0 {
		return "never"
	}
	return time.Duration(sec * int64(time.Second)).String()
}

// inWSL reports whether this Linux runs inside WSL.
func inWSL() bool {
	b, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// PowerAdvice checks the Windows power plan from inside WSL and returns a warning or "".
// It returns "" when it cannot tell (not WSL, powercfg.exe missing or unreadable).
func PowerAdvice() string {
	if !inWSL() {
		return ""
	}
	p, err := exec.LookPath("powercfg.exe")
	if err != nil {
		return ""
	}
	out, err := exec.Command(p, "/query", "SCHEME_CURRENT", "SUB_SLEEP", "STANDBYIDLE").Output()
	if err != nil {
		return ""
	}
	ac, dc, ok := ParsePowercfg(string(out))
	if !ok {
		return ""
	}
	return SleepAdvice(ac, dc)
}
