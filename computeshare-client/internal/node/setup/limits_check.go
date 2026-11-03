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

// LXCFSActive reports whether lxcfs is installed and serving /proc views on this host.
func LXCFSActive() bool {
	if _, err := os.Stat("/var/lib/lxcfs/proc/meminfo"); err == nil {
		return true
	}
	_, err := exec.LookPath("lxcfs")
	return err == nil
}

// EnsureLXCFS installs lxcfs (best effort) so a space's `free`, `df` and `nproc` show its own limits.
// Existing spaces see it after a restart.
func EnsureLXCFS(out func(string)) {
	if LXCFSActive() {
		return
	}
	out("installing lxcfs (so the space shows its own CPU and memory limits)...")
	c := exec.Command("sh", "-c", "DEBIAN_FRONTEND=noninteractive apt-get install -y -q lxcfs")
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		out("lxcfs could not be installed (" + err.Error() + "); `csnode doctor` will say that limits are not shown inside the space")
	}
}
