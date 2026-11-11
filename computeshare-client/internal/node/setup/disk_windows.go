//go:build windows

package setup

import (
	"os"

	"golang.org/x/sys/windows"
)

// freeDiskGB returns the free space (GB) on the system drive.
func freeDiskGB() float64 {
	p := os.Getenv("SystemDrive")
	if p == "" {
		p = "C:"
	}
	ptr, err := windows.UTF16PtrFromString(p + `\`)
	if err != nil {
		return 0
	}
	var free, total, totalFree uint64
	if windows.GetDiskFreeSpaceEx(ptr, &free, &total, &totalFree) != nil {
		return 0
	}
	return float64(free) / 1e9
}
