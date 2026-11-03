package gpu

import (
	"strings"
	"testing"
)

func TestPlan(t *testing.T) {
	if _, _, err := Plan(Env{}, 0); err != ErrNoDriver {
		t.Fatal(err)
	}
	c, d, _ := Plan(Env{Driver: true, Toolkit: true}, 1)
	if c[0] != "nvidia.runtime=true" || strings.Join(d[0], " ") != "csgpu gpu gputype=physical id=1" {
		t.Fatal(c, d)
	}
	c, d, _ = Plan(Env{WSL: true}, 0)
	if !strings.Contains(c[0], "/usr/lib/wsl/lib") || d[0][1] != "unix-char" || d[1][4] != "readonly=true" || len(d) != 3 || d[2][0] != "cswsldrv" || d[2][3] != "path=/usr/lib/wsl/drivers" {
		t.Fatal(c, d)
	}
}
