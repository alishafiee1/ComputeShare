package setup

import (
	"os"
	"os/exec"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/node/incus"
)

// Real limits (umbrella card 016): the space only sees its own share in `free`, `df` and `nproc`
// when lxcfs runs on the host, and a disk limit only stops writes on a pool whose driver has quotas.

// PoolDriver parses the first line of `incus storage list --format csv` ("default,dir,…") and
// returns the driver name, or "" when it cannot tell.
func PoolDriver(csv string) string {
	for _, l := range strings.Split(csv, "\n") {
		f := strings.Split(strings.TrimSpace(l), ",")
		if len(f) >= 2 && f[0] != "" {
			return strings.TrimSpace(f[1])
		}
	}
	return ""
}

// StorageDriver is the driver of the first Incus storage pool ("" if Incus is missing).
func StorageDriver() string {
	if !incus.Available() {
		return ""
	}
	out, err := incus.Run(nil, "storage", "list", "--format", "csv")
	if err != nil {
		return ""
	}
	return PoolDriver(out)
}

// QuotaDriver reports whether a pool driver enforces a size limit on an instance's root disk.
// "dir" only shares the host file system: a `size:` there does nothing (phase 1 saw df / = 1007G).
func QuotaDriver(driver string) bool {
	switch driver {
	case "btrfs", "zfs", "lvm", "lvmcluster", "ceph", "cephfs":
		return true
	}
	return false
}

// FirstPool parses the first line of `incus storage list --format csv` and returns the pool name and driver.
func FirstPool(csv string) (name, driver string) {
	for _, l := range strings.Split(csv, "\n") {
		f := strings.Split(strings.TrimSpace(l), ",")
		if len(f) >= 2 && f[0] != "" {
			return strings.TrimSpace(f[0]), strings.TrimSpace(f[1])
		}
	}
	return "", ""
}

// SizeCappedMount reports whether `findmnt -n -o TARGET,SOURCE` output (for the pool's source path) shows a
// file system of its own on a loop device. A `dir` pool there cannot grow past the image size, so the disk
// limit holds even though the driver itself has no quotas (the way to get a real limit where the kernel has
// no btrfs, zfs or lvm, for example in WSL2).
func SizeCappedMount(findmnt string) bool {
	f := strings.Fields(strings.TrimSpace(findmnt))
	return len(f) >= 2 && f[0] != "/" && strings.HasPrefix(f[1], "/dev/loop")
}

// DiskEnforced reports whether the first pool really enforces the disk limit: a driver with quotas, or a
// `dir` pool whose source is a loop-mounted image of its own.
func DiskEnforced() bool {
	if !incus.Available() {
		return true
	}
	out, err := incus.Run(nil, "storage", "list", "--format", "csv")
	if err != nil {
		return false
	}
	name, drv := FirstPool(out)
	if QuotaDriver(drv) {
		return true
	}
	if drv != "dir" || name == "" {
		return false
	}
	src, err := incus.Run(nil, "storage", "get", name, "source")
	if err != nil || strings.TrimSpace(src) == "" {
		return false
	}
	m, err := exec.Command("findmnt", "-n", "-o", "TARGET,SOURCE", "--target", strings.TrimSpace(src)).Output()
	return err == nil && SizeCappedMount(string(m))
}

// LXCFSActive reports whether lxcfs is serving /proc views on this host. An installed but stopped lxcfs
// does not count: spaces then still show the whole computer in `free` (seen on WSL2, where the packaged
// unit refuses to start because systemd reports a "container").
func LXCFSActive() bool {
	_, err := os.Stat("/var/lib/lxcfs/proc/meminfo")
	return err == nil
}

// LXCFSDropIn lets the packaged lxcfs unit start under WSL2: its ConditionVirtualization=!container
// check is false there, so systemd silently skips the service. An empty value clears the condition.
const LXCFSDropIn = "[Unit]\nConditionVirtualization=\n"

// startLXCFS writes the drop-in and starts lxcfs now and at every boot (best effort).
func startLXCFS(out func(string)) {
	dir := "/etc/systemd/system/lxcfs.service.d"
	if err := os.MkdirAll(dir, 0o755); err == nil {
		os.WriteFile(dir+"/csnode.conf", []byte(LXCFSDropIn), 0o644)
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", "lxcfs"}} {
		if err := exec.Command("systemctl", args...).Run(); err != nil {
			out("lxcfs could not be started (systemctl " + args[0] + ": " + err.Error() + ")")
			return
		}
	}
}

// EnsureLXCFS installs and starts lxcfs (best effort) so a space's `free`, `df` and `nproc` show its own
// limits. Existing spaces see it after a restart.
func EnsureLXCFS(out func(string)) {
	if LXCFSActive() {
		return
	}
	if _, err := exec.LookPath("lxcfs"); err != nil {
		out("installing lxcfs (so the space shows its own CPU and memory limits)...")
		c := exec.Command("sh", "-c", "DEBIAN_FRONTEND=noninteractive apt-get install -y -q lxcfs")
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			out("lxcfs could not be installed (" + err.Error() + "); `csnode doctor` will say that limits are not shown inside the space")
			return
		}
	}
	startLXCFS(out)
}
