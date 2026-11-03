package main

// cs bench (client 018): node facts from the broker list, and with --run a short hardware test
// inside the user's own space over the normal SSH path.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/bench"
	"github.com/alishafiee1/computeshare-client/proto"
)

const remoteDir = "~/.cache/cs-bench"

func benchCmd(a []string) {
	var name string
	runIt, asJSON := false, false
	for _, x := range a {
		switch x {
		case "--run":
			runIt = true
		case "--json":
			asJSON = true
		default:
			if name != "" || strings.HasPrefix(x, "-") {
				usage()
			}
			name = x
		}
	}
	if name == "" {
		usage()
	}
	c := load()
	now := time.Now()
	last := bench.Last(cfgDir(), name)
	if !runIt {
		row := findNode(fetch(c), name)
		if asJSON {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"node": row, "last": last})
			return
		}
		fmt.Print(nodeFacts(row))
		if last != nil {
			fmt.Print(last.Format(now))
		} else {
			fmt.Printf("test:      none yet (run: cs bench %s --run)\n", name)
		}
		return
	}
	if err := bench.CheckGap(last, now); err != nil { // R6
		die(1, "%v", err)
	}
	fmt.Fprintln(os.Stderr, "testing", name, "(about 15 s)...")
	r, err := runBench(c, name)
	if err != nil {
		die(1, "%v", err)
	}
	r.Node, r.At = name, time.Now().UTC()
	if err := bench.Save(cfgDir(), r); err != nil {
		die(1, "%v", err)
	}
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(r)
		return
	}
	fmt.Print(r.Format(time.Now()))
}

func findNode(rows []proto.NodeRow, name string) proto.NodeRow {
	for _, r := range rows {
		if strings.EqualFold(r.Name, name) {
			return r
		}
	}
	die(1, "no node %q in the list (cs nodes)", name)
	return proto.NodeRow{}
}

// nodeFacts is the R1 part: what the broker knows, no SSH needed.
func nodeFacts(r proto.NodeRow) string {
	up := "-"
	if r.UptimeS > 0 {
		d := time.Duration(r.UptimeS) * time.Second
		up = fmt.Sprintf("%dd %dh %dm", int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60)
	}
	or := func(s string) string {
		if s == "" {
			return "unlimited"
		}
		return s
	}
	gpu := r.Specs.GPU
	if gpu == "" {
		gpu = "none"
	}
	return fmt.Sprintf("node:      %s (%s)\nuptime:    %s\nspecs:     cpu %s, ram %.0f GB, disk %.0f GB, gpu %s\nlimits:    bandwidth %s, quota %s\n",
		r.Name, r.Status, up, r.Specs.CPU, r.Specs.RAMGB, r.Specs.DiskGB, gpu, or(r.Limits.Bandwidth), or(r.Limits.Quota))
}

// sshRun runs one remote command on <name>.cs with the cs ssh options (ControlMaster from 017).
func sshRun(c Config, name, cmd string, stdin []byte) (string, error) {
	args := append(sshBase(c), "-o", "BatchMode=no", c.User+"@"+strings.ToLower(name)+".cs", cmd)
	x := exec.Command("ssh", args...)
	var out, errb bytes.Buffer
	x.Stdout, x.Stderr = &out, &errb
	if stdin != nil {
		x.Stdin = bytes.NewReader(stdin)
	} else {
		x.Stdin = os.Stdin
	}
	if err := x.Run(); err != nil {
		return out.String(), fmt.Errorf("ssh %s.cs: %v %s", name, err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// helperPath finds cs-bench-linux-<arch> next to cs, in CS_BENCH_DIR or in the cs config dir.
func helperPath(arch string) (string, error) {
	n := "cs-bench-linux-" + arch
	var dirs []string
	if d := os.Getenv("CS_BENCH_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	if self, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(self))
	}
	dirs = append(dirs, cfgDir())
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, n)); err == nil {
			return filepath.Join(d, n), nil
		}
	}
	return "", fmt.Errorf("%s not found; download it from the signed release into %s", n, cfgDir())
}

func goArch(uname string) string {
	switch strings.TrimSpace(uname) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return strings.TrimSpace(uname)
}

func runBench(c Config, name string) (*bench.Result, error) {
	info, err := sshRun(c, name, "uname -m; cat "+remoteDir+"/version 2>/dev/null || true", nil)
	if err != nil {
		return nil, err
	}
	lines := strings.SplitN(strings.TrimSpace(info), "\n", 2)
	hp, err := helperPath(goArch(lines[0]))
	if err != nil {
		return nil, err
	}
	bin, err := os.ReadFile(hp)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(bin)
	want := hex.EncodeToString(sum[:])
	if len(lines) < 2 || strings.TrimSpace(lines[1]) != want { // R2: upload if missing or outdated
		up := "set -e; umask 077; mkdir -p " + remoteDir + "; cat > " + remoteDir + "/cs-bench.new; chmod 700 " + remoteDir +
			"/cs-bench.new; mv " + remoteDir + "/cs-bench.new " + remoteDir + "/cs-bench; echo " + want + " > " + remoteDir + "/version"
		if _, err := sshRun(c, name, up, bin); err != nil {
			return nil, err
		}
	}
	out, err := sshRun(c, name, remoteDir+"/cs-bench", []byte{})
	if err != nil {
		return nil, err
	}
	return bench.Parse(out)
}
