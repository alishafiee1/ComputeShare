//go:build !windows

package setup

import (
	"os"
	"syscall"
)

// freeDiskGB returns the free space (GB) where the node keeps its data.
func freeDiskGB() float64 {
	var st syscall.Statfs_t
	p := "/var/lib/incus"
	if _, err := os.Stat(p); err != nil {
		p = "/"
	}
	if syscall.Statfs(p, &st) != nil {
		return 0
	}
	return float64(st.Bavail) * float64(st.Bsize) / 1e9
}
