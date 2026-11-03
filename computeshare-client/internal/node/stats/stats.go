// Package stats collects the Ping stats (node 006 R3 / broker 003 R1).
package stats

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
)

type Collector struct {
	// SpaceCPU, if set, returns cumulative CPU nanoseconds used by the space and its CPU limit (cores).
	SpaceCPU func() (ns int64, cores float64, memPct float64, ok bool)
	// NetBytes, if set, returns cumulative rx/tx bytes of the space NIC.
	NetBytes func() (rx, tx int64, ok bool)

	mu                  sync.Mutex
	lastT               time.Time
	lastIdle, lastTotal uint64
	lastNS              int64
	lastRx, lastTx      int64
}

func (c *Collector) Collect() proto.Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	dt := now.Sub(c.lastT).Seconds()
	var s proto.Stats
	// host CPU from /proc/stat (fallback)
	idle, total := readCPU()
	if c.lastTotal > 0 && total > c.lastTotal {
		s.CPUPct = 100 * (1 - float64(idle-c.lastIdle)/float64(total-c.lastTotal))
	}
	c.lastIdle, c.lastTotal = idle, total
	s.RAMPct = memPct()
	if c.SpaceCPU != nil {
		if ns, cores, mp, ok := c.SpaceCPU(); ok {
			if c.lastNS > 0 && dt > 0 && cores > 0 {
				s.CPUPct = 100 * float64(ns-c.lastNS) / (dt * 1e9 * cores)
			}
			c.lastNS = ns
			if mp > 0 {
				s.RAMPct = mp
			}
		}
	}
	if c.NetBytes != nil {
		if rx, tx, ok := c.NetBytes(); ok {
			if c.lastRx > 0 && dt > 0 {
				s.RxBps, s.TxBps = float64(rx-c.lastRx)*8/dt, float64(tx-c.lastTx)*8/dt
			}
			c.lastRx, c.lastTx = rx, tx
		}
	}
	if s.CPUPct < 0 {
		s.CPUPct = 0
	}
	if s.CPUPct > 100 {
		s.CPUPct = 100
	}
	s.Load1, s.Load5, s.Load15 = loadAvg()
	s.GPUPct, s.VRAMPct = gpu()
	c.lastT = now
	return s
}

func readCPU() (idle, total uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if sc.Scan() {
		fs := strings.Fields(sc.Text())
		for i, v := range fs[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			total += n
			if i == 3 || i == 4 {
				idle += n
			}
		}
	}
	return
}

func memPct() float64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var tot, avail float64
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(f[1], 64)
		switch f[0] {
		case "MemTotal:":
			tot = v
		case "MemAvailable:":
			avail = v
		}
	}
	if tot == 0 {
		return 0
	}
	return 100 * (1 - avail/tot)
}

func loadAvg() (a, b, c float64) {
	d, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	f := strings.Fields(string(d))
	if len(f) >= 3 {
		a, _ = strconv.ParseFloat(f[0], 64)
		b, _ = strconv.ParseFloat(f[1], 64)
		c, _ = strconv.ParseFloat(f[2], 64)
	}
	return
}

var nvsmi, _ = exec.LookPath("nvidia-smi")

func gpu() (*float64, *float64) {
	if nvsmi == "" {
		return nil, nil
	}
	out, err := exec.Command(nvsmi, "--query-gpu=utilization.gpu,memory.used,memory.total", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, nil
	}
	f := strings.Split(strings.Split(strings.TrimSpace(string(out)), "\n")[0], ",")
	if len(f) < 3 {
		return nil, nil
	}
	u, _ := strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
	mu, _ := strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
	mt, _ := strconv.ParseFloat(strings.TrimSpace(f[2]), 64)
	v := 0.0
	if mt > 0 {
		v = 100 * mu / mt
	}
	return &u, &v
}
