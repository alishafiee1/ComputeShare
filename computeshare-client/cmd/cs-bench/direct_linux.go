package main

import (
	"os"
	"syscall"
	"unsafe"
)

func uintptrOf(b []byte) uintptr { return uintptr(unsafe.Pointer(&b[0])) }

// openDirect opens with O_DIRECT, falling back to a normal open (tmpfs, some overlay setups).
func openDirect(path string, flag int) (*os.File, bool, error) {
	if f, err := os.OpenFile(path, flag|syscall.O_DIRECT, 0o600); err == nil {
		return f, true, nil
	}
	f, err := os.OpenFile(path, flag, 0o600)
	return f, false, err
}
