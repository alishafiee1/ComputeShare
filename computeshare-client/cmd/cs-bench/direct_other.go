//go:build !linux

package main

import (
	"os"
	"unsafe"
)

func uintptrOf(b []byte) uintptr { return uintptr(unsafe.Pointer(&b[0])) }

func openDirect(path string, flag int) (*os.File, bool, error) {
	f, err := os.OpenFile(path, flag, 0o600)
	return f, false, err
}
