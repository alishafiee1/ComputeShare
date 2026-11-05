//go:build windows

package main

import "testing"

func TestWslDataDir(t *testing.T) {
	if d, err := wslDataDir(`D:\ComputeShare\wsl`); err != nil || d != `D:\ComputeShare\wsl` {
		t.Fatal(d, err)
	}
	for _, bad := range []string{`\server\share\x`, `relative\dir`} {
		if _, err := wslDataDir(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	if d, err := wslDataDir(""); err != nil || d == "" {
		t.Fatal("default dir", d, err)
	}
}
