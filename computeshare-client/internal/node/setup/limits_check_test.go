package setup

import "testing"

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

// umbrella card 016 R2: only drivers with quotas may claim a disk limit.
func TestQuotaDriver(t *testing.T) {
	for d, want := range map[string]bool{"dir": false, "": false, "btrfs": true, "zfs": true, "lvm": true} {
		if got := QuotaDriver(d); got != want {
			t.Fatalf("%q: %v", d, got)
		}
	}
}
