package setup

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/alishafiee1/computeshare-client/proto"
)

type Resources struct {
	CPUs   int
	RAMGB  float64
	DiskGB float64
	GPU    string
}

// Detect implements node 001 R2.
func Detect() Resources {
	r := Resources{CPUs: runtime.NumCPU()}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if f := strings.Fields(l); len(f) >= 2 && f[0] == "MemTotal:" {
				kb, _ := strconv.ParseFloat(f[1], 64)
				r.RAMGB = kb / 1024 / 1024
			}
		}
	}
	var st syscall.Statfs_t
	p := "/var/lib/incus"
	if _, err := os.Stat(p); err != nil {
		p = "/"
	}
	if syscall.Statfs(p, &st) == nil {
		r.DiskGB = float64(st.Bavail) * float64(st.Bsize) / 1e9
	}
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader").Output(); err == nil {
		r.GPU = strings.TrimSpace(strings.Split(string(out), "\n")[0])
	}
	return r
}

func (r Resources) SuggestCPU() string {
	n := r.CPUs / 2
	if n < 1 {
		n = 1
	}
	return strconv.Itoa(n)
}

func (r Resources) SuggestMemory() string {
	g := int(r.RAMGB / 2)
	if g < 1 {
		g = 1
	}
	return fmt.Sprintf("%dGiB", g)
}

func (r Resources) Table() string {
	gpu := r.GPU
	if gpu == "" {
		gpu = "-"
	}
	return fmt.Sprintf(`RESOURCE  TOTAL/FREE        SUGGESTED
CPU       %-17s %s cores
RAM       %-17s %s
Disk      %-17s 100GiB
GPU       %-17s off
`, fmt.Sprintf("%d cores", r.CPUs), r.SuggestCPU(), fmt.Sprintf("%.1f GiB", r.RAMGB), r.SuggestMemory(), fmt.Sprintf("%.0f GB free", r.DiskGB), gpu)
}

func (r Resources) Specs(gpuOn bool) proto.Specs {
	s := proto.Specs{CPU: fmt.Sprintf("%dc", r.CPUs), RAMGB: float64(int(r.RAMGB + 0.5)), DiskGB: float64(int(r.DiskGB))}
	if gpuOn {
		s.GPU = r.GPU
	}
	return s
}
