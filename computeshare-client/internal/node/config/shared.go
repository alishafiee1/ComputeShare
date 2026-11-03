package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alishafiee1/computeshare-client/proto"
)

// ParseGB reads a size such as "4GiB", "20GB", "512MiB", "1.5T" and returns it in GB (GiB for the
// binary suffixes, not converted: the owner sees the same number they typed). ok is false for an
// empty or unreadable value, which means "no limit".
func ParseGB(s string) (gb float64, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "off" || s == "unlimited" {
		return 0, false
	}
	unit := 1.0
	for _, u := range []struct {
		suf string
		mul float64
	}{{"tib", 1024}, {"tb", 1000}, {"t", 1024}, {"gib", 1}, {"gb", 1}, {"g", 1}, {"mib", 1.0 / 1024}, {"mb", 1.0 / 1000}, {"m", 1.0 / 1024}} {
		if strings.HasSuffix(s, u.suf) {
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suf))
			unit = u.mul
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v * unit, true
}

// SharedSpecs is what the owner shares, for the heartbeat (umbrella card 016 R3): only limited
// fields are set. The GPU is set to its name only when the owner shares it.
func (l Limits) SharedSpecs(gpuName string) proto.Specs {
	var s proto.Specs
	if n, err := strconv.ParseFloat(strings.TrimSpace(l.CPU), 64); err == nil && n > 0 {
		s.CPU = fmt.Sprintf("%gc", n)
	}
	if gb, ok := ParseGB(l.Memory); ok {
		s.RAMGB = round1(gb)
	}
	if gb, ok := ParseGB(l.Disk); ok {
		s.DiskGB = round1(gb)
	}
	if l.GPU {
		s.GPU = gpuName
	}
	return s
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
