//go:build !windows

package main

import "testing"

func TestSafeDefault(t *testing.T) {
	if safeDefault("", "4GiB") != "4GiB" || safeDefault("8GiB", "4GiB") != "8GiB" {
		t.Fatal("safeDefault must keep a given value and fill an empty one")
	}
}
