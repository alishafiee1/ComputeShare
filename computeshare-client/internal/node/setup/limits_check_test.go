package setup

import (
	"strings"
	"testing"
)

func TestPoolDriver(t *testing.T) {
	for in, want := range map[string]string{
		"default,dir,,1,CREATED\n":       "dir",
		"cs,btrfs,loop file,2,CREATED\n": "btrfs",
		"\n  \ndefault,zfs,,0,CREATED":   "zfs",
		"":                               "",
		"garbage":                        "",
	} {
		if got := PoolDriver(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

// WSL2: the packaged lxcfs unit is skipped unless its virtualization condition is cleared.
func TestLXCFSDropIn(t *testing.T) {
	if !strings.HasPrefix(LXCFSDropIn, "[Unit]\n") || !strings.Contains(LXCFSDropIn, "\nConditionVirtualization=\n") {
		t.Fatalf("%q", LXCFSDropIn)
	}
}

func TestFirstPool(t *testing.T) {
	if n, d := FirstPool("cs,dir,loop file,2,CREATED\n"); n != "cs" || d != "dir" {
		t.Fatalf("%q %q", n, d)
	}
	if n, d := FirstPool("garbage"); n != "" || d != "" {
		t.Fatalf("%q %q", n, d)
	}
}

// a dir pool counts as capped only on its own loop-mounted file system, never on the host root.
func TestSizeCappedMount(t *testing.T) {
	for in, want := range map[string]bool{
		"/var/lib/incus-cs/mnt /dev/loop3\n": true,
		"/ /dev/sdc\n":                       false,
		"/ /dev/loop0\n":                     false,
		"/mnt/data /dev/sdb1\n":              false,
		"":                                   false,
		"/only-one-field":                    false,
	} {
		if got := SizeCappedMount(in); got != want {
			t.Fatalf("%q: %v", in, got)
		}
	}
}

// umbrella card 016 R2: only drivers with quotas may claim a disk limit.
func TestQuotaDriver(t *testing.T) {
	for d, want := range map[string]bool{"dir": false, "": false, "btrfs": true, "zfs": true, "lvm": true} {
		if got := QuotaDriver(d); got != want {
			t.Fatalf("%q: %v", d, got)
		}
	}
}
