package proto

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	nameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{2,31}$`)
	userRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
)

// ValidName checks a node name (R3).
func ValidName(n string) bool { return nameRe.MatchString(n) }

// NameKey is the case-insensitive uniqueness key.
func NameKey(n string) string { return strings.ToLower(n) }

// ValidUser checks a space user name (R3).
func ValidUser(u string) bool { return userRe.MatchString(u) }

// NameTakenMessage is the exact user-facing text.
func NameTakenMessage(n string) string {
	return fmt.Sprintf("name %q is already taken; choose another", n)
}

var unitMul = map[string]float64{"": 1, "k": 1e3, "m": 1e6, "g": 1e9, "t": 1e12}

// ParseBandwidth parses "20Mbit" (also "20mbit", "500kbit", "1Gbit") into bits/s.
// Empty or "off"/"unlimited" = 0 meaning unlimited.
func ParseBandwidth(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "off" || s == "unlimited" {
		return 0, nil
	}
	if !strings.HasSuffix(s, "bit") {
		return 0, fmt.Errorf("invalid bandwidth %q (example: 20Mbit)", s)
	}
	return parseNum(strings.TrimSuffix(s, "bit"), "bandwidth")
}

// ParseQuota parses "20GB/day" into bytes/day. Empty/"off" = 0 (unlimited).
func ParseQuota(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "off" || s == "unlimited" {
		return 0, nil
	}
	if !strings.HasSuffix(s, "b/day") {
		return 0, fmt.Errorf("invalid quota %q (example: 20GB/day)", s)
	}
	return parseNum(strings.TrimSuffix(s, "b/day"), "quota")
}

func parseNum(s, what string) (int64, error) {
	unit := ""
	if n := len(s); n > 0 {
		if _, ok := unitMul[s[n-1:]]; ok && (s[n-1] < '0' || s[n-1] > '9') {
			unit, s = s[n-1:], s[:n-1]
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0, fmt.Errorf("invalid %s value", what)
	}
	return int64(f * unitMul[unit]), nil
}
