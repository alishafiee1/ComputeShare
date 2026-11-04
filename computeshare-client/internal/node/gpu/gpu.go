// Package gpu passes an NVIDIA GPU into the space (node card 004).
package gpu

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/node/incus"
)

var (
	ErrNoDriver    = errors.New("nvidia driver/toolkit missing")
	ErrUnsupported = errors.New("GPU passthrough not supported on this system")
)

// Env describes the host; filled by Probe, injectable in tests.
type Env struct {
	WSL     bool // /dev/dxg present (WSL2)
	Driver  bool // nvidia-smi works on host
	Toolkit bool // nvidia-container-cli present (Linux)
}

func Probe() Env {
	_, dxg := os.Stat("/dev/dxg")
	_, smi := exec.Command("nvidia-smi", "-L").Output()
	_, tk := exec.LookPath("nvidia-container-cli")
	return Env{WSL: dxg == nil, Driver: smi == nil, Toolkit: tk == nil}
}

// Plan is the incus config/device commands for index idx (R1/R2, edge: --gpu N).
func Plan(e Env, idx int) (config []string, devices [][]string, err error) {
	if e.WSL {
		return []string{"environment.LD_LIBRARY_PATH=/usr/lib/wsl/lib"},
			[][]string{
				// mode 0666: the default is 0660 root:root, so the space user (not root) got "GPU access blocked".
				{"csgpu", "unix-char", "source=/dev/dxg", "path=/dev/dxg", "mode=0666"},
				{"cswsllib", "disk", "source=/usr/lib/wsl/lib", "path=/usr/lib/wsl/lib", "readonly=true"},
				// the NVIDIA user-space driver lives in the Windows driver store (a 9p mount); without it
				// nvidia-smi inside the space says "couldn't communicate with the NVIDIA driver". The kernel
				// does not allow recursive and read-only together, so this one is not marked readonly (the mount itself is ro).
				{"cswsldrv", "disk", "source=/usr/lib/wsl/drivers", "path=/usr/lib/wsl/drivers", "recursive=true"},
			}, nil
	}
	if !e.Driver || !e.Toolkit {
		return nil, nil, ErrNoDriver
	}
	return []string{"nvidia.runtime=true", "nvidia.driver.capabilities=all"},
		[][]string{{"csgpu", "gpu", "gputype=physical", fmt.Sprintf("id=%d", idx)}}, nil
}

// Enable applies the plan, restarts the space and checks nvidia-smi inside.
// On WSL a failing check removes the devices and returns ErrUnsupported (never fatal for setup).
func Enable(inst string, e Env, idx int) error {
	cfg, devs, err := Plan(e, idx)
	if err != nil {
		return err
	}
	for _, kv := range cfg {
		if _, err := incus.Run(nil, "config", "set", inst, kv); err != nil {
			return err
		}
	}
	for _, d := range devs {
		if _, err := incus.Run(nil, append([]string{"config", "device", "add", inst}, d...)...); err != nil && !strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	incus.Stop(inst, false)
	if err := incus.Start(inst); err != nil {
		return err
	}
	if e.WSL {
		incus.Exec(inst, nil, "sh", "-c", WSLGuestSetup) // best effort: ssh sessions find nvidia-smi and its library
	}
	if _, err := incus.Exec(inst, nil, "sh", "-c", "PATH=$PATH:/usr/lib/wsl/lib nvidia-smi -L"); err != nil {
		Disable(inst)
		if e.WSL {
			return ErrUnsupported
		}
		return fmt.Errorf("nvidia-smi failed inside space: %w", err)
	}
	return nil
}

// WSLGuestSetup lets plain ssh sessions (which ignore incus' environment.* keys) find nvidia-smi
// and libnvidia-ml.so without LD_LIBRARY_PATH. Idempotent.
const WSLGuestSetup = `echo /usr/lib/wsl/lib > /etc/ld.so.conf.d/cs-wsl.conf; (ldconfig || /sbin/ldconfig) 2>/dev/null
grep -q '^PATH=' /etc/environment 2>/dev/null || echo 'PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/lib/wsl/lib"' >> /etc/environment
true`

// Disable removes every GPU device/config we added (R3 off).
func Disable(inst string) {
	for _, d := range []string{"csgpu", "cswsllib", "cswsldrv", "gpu"} {
		incus.Run(nil, "config", "device", "remove", inst, d)
	}
	for _, k := range []string{"nvidia.runtime", "nvidia.driver.capabilities", "environment.LD_LIBRARY_PATH"} {
		incus.Run(nil, "config", "unset", inst, k)
	}
}

// Info returns "name, NNNN MiB" from the host for stats (R4).
func Info(idx int) string {
	out, err := exec.Command("nvidia-smi", fmt.Sprintf("--id=%d", idx), "--query-gpu=name,memory.total", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
