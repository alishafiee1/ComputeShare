// cs-bench: short hardware test that runs INSIDE a space as the user (client 018 R3, R4).
// It needs no root, writes only under ~/.cache/cs-bench/, stays under 15 s and prints one
// JSON line. `cs bench <name> --run` uploads and runs it.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/bench"
)

var version = "0.2.0-dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	h, _ := os.UserHomeDir()
	dir := filepath.Join(h, ".cache", "cs-bench")
	os.MkdirAll(dir, 0o700)
	tmp := filepath.Join(dir, "disk.tmp")
	defer os.Remove(tmp)

	r := bench.Result{Version: version, CPUs: runtime.NumCPU()}
	start := time.Now()
	r.CPUSingle = cpu(ctx, 1, 3*time.Second)
	r.CPUAll = cpu(ctx, runtime.NumCPU(), 3*time.Second)
	r.RAMGBs = ram(ctx, 2*time.Second)
	r.DiskWriteMBs, r.DiskReadMBs, r.DiskDirect = disk(ctx, tmp, 4*time.Second)
	r.GPU = gpu(ctx)
	r.Seconds = math.Round(time.Since(start).Seconds()*10) / 10
	if ctx.Err() != nil {
		r.Error = "interrupted"
	}
	json.NewEncoder(os.Stdout).Encode(r)
}

// cpu runs a mixed integer/float loop on n goroutines and returns million loop rounds per second.
func cpu(ctx context.Context, n int, d time.Duration) float64 {
	var total atomic.Int64
	var wg sync.WaitGroup
	end := time.Now().Add(d)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			x, f := uint64(seed+1), 1.0001
			var c int64
			for ctx.Err() == nil && time.Now().Before(end) {
				for j := 0; j < 100000; j++ {
					x ^= x << 13
					x ^= x >> 7
					x ^= x << 17
					f = f*1.0000001 + float64(x&1023)*1e-9
				}
				c++
			}
			if f == 0 { // keep the loop from being optimised away
				fmt.Fprint(os.Stderr, x)
			}
			total.Add(c)
		}(i)
	}
	wg.Wait()
	return math.Round(float64(total.Load())*100000/d.Seconds()/1e6*10) / 10
}

// ram copies a 64 MiB buffer back and forth and returns GB/s.
func ram(ctx context.Context, d time.Duration) float64 {
	a, b := make([]byte, 64<<20), make([]byte, 64<<20)
	for i := range a {
		a[i] = byte(i)
	}
	var n int64
	t := time.Now()
	for ctx.Err() == nil && time.Since(t) < d {
		copy(b, a)
		a, b = b, a
		n += int64(len(a))
	}
	return math.Round(float64(n)/time.Since(t).Seconds()/1e9*10) / 10
}

// disk writes then reads up to 256 MiB within d (half each), bypassing the page cache when possible.
func disk(ctx context.Context, path string, d time.Duration) (w, r float64, direct bool) {
	const size, block = 256 << 20, 4 << 20
	buf := aligned(block)
	for i := range buf {
		buf[i] = byte(i * 7)
	}
	f, direct, err := openDirect(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
	if err != nil {
		return 0, 0, false
	}
	t, n := time.Now(), 0
	for n < size && ctx.Err() == nil && time.Since(t) < d/2 {
		if _, err := f.Write(buf); err != nil {
			break
		}
		n += block
	}
	f.Sync()
	f.Close()
	w = math.Round(float64(n)/time.Since(t).Seconds()/1e6*10) / 10
	f, _, err = openDirect(path, os.O_RDONLY)
	if err != nil {
		return w, 0, direct
	}
	defer f.Close()
	t, m := time.Now(), 0
	for m < n && ctx.Err() == nil && time.Since(t) < d/2 {
		k, err := f.Read(buf)
		if k <= 0 || err != nil {
			break
		}
		m += k
	}
	r = math.Round(float64(m)/time.Since(t).Seconds()/1e6*10) / 10
	return w, r, direct
}

// aligned returns a buffer aligned to 4096 bytes for O_DIRECT.
func aligned(n int) []byte {
	b := make([]byte, n+4096)
	off := 0
	if r := int(uintptrOf(b) & 4095); r != 0 {
		off = 4096 - r
	}
	return b[off : off+n]
}

// gpu reports the first NVIDIA GPU from nvidia-smi (2 s max), or "none".
func gpu(ctx context.Context) string {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return "none"
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "nvidia-smi", "--query-gpu=name,memory.total,clocks.max.sm", "--format=csv,noheader").Output()
	if err != nil {
		return "none"
	}
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}
