package main

import "testing"

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0B", 1023: "1023B", 1024: "1.0KiB", 1536: "1.5KiB", 1 << 20: "1.0MiB", 5 << 30: "5.0GiB"} {
		if got := humanBytes(n); got != want {
			t.Fatalf("%d: got %s want %s", n, got, want)
		}
	}
}
