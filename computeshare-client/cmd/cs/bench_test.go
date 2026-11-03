//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/bench"
)

// client 018 R2, R5, R6: --run uploads the helper once, runs it, stores the result and
// refuses a second run within 10 minutes. A fake `ssh` runs the remote command locally.
func TestBenchRun(t *testing.T) {
	d := t.TempDir()
	home, bin, cfg := filepath.Join(d, "home"), filepath.Join(d, "bin"), filepath.Join(d, "cfg")
	os.MkdirAll(home, 0o700)
	os.MkdirAll(bin, 0o700)
	os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\nfor a; do last=$a; done\necho \"$last\" >> \"$HOME/../ssh.log\"\nexec sh -c \"$last\"\n"), 0o755)
	helper := filepath.Join(bin, "cs-bench-linux-"+goArch(runtime.GOARCH))
	os.WriteFile(helper, []byte("#!/bin/sh\necho '{\"version\":\"t\",\"cpus\":2,\"cpu_single\":10,\"cpu_all\":20,\"ram_gbs\":3,\"disk_write_mbs\":100,\"disk_read_mbs\":200,\"gpu\":\"none\",\"seconds\":1}'\n"), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	t.Setenv("CS_BENCH_DIR", bin)
	t.Setenv("CS_CONFIG_DIR", cfg)
	c := Config{Server: "h:2222", User: "user"}
	// uname -m on the test host may say x86_64; helperPath maps it.
	r, err := runBench(c, "box")
	if err != nil || r.CPUAll != 20 {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cache/cs-bench/cs-bench")); err != nil {
		t.Fatal("helper not uploaded")
	}
	runBench(c, "box")
	log, _ := os.ReadFile(filepath.Join(d, "ssh.log"))
	if n := strings.Count(string(log), "cat > "); n != 1 {
		t.Fatalf("helper uploaded %d times:\n%s", n, log)
	}
	r.Node, r.At = "box", time.Now()
	bench.Save(cfg, r)
	if err := bench.CheckGap(bench.Last(cfg, "box"), time.Now()); err == nil || !strings.Contains(err.Error(), "try again in 10 min") {
		t.Fatal(err)
	}
}
