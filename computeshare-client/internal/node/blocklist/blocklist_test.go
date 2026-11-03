package blocklist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newList(t *testing.T) (*List, *time.Time) {
	l := Open(t.TempDir())
	now := time.Unix(1_800_000_000, 0)
	l.Now = func() time.Time { return now }
	return l, &now
}

func TestCanon(t *testing.T) {
	for in, want := range map[string]string{"1.2.3.4": "1.2.3.4", " 1.2.3.4 ": "1.2.3.4", "10.1.2.3/8": "10.0.0.0/8", "2001:db8::1": "2001:db8::1"} {
		got, err := Canon(in)
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "abc", "1.2.3", "1.2.3.4/33", "999.1.1.1"} {
		if _, err := Canon(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

// card 015 R11: an IP and a range block; others pass; a port suffix is ignored.
func TestAddBlockedRemove(t *testing.T) {
	l, _ := newList(t)
	if _, err := l.Add("203.0.113.9", 0, "spam"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Add("198.51.100.0/24", 0, ""); err != nil {
		t.Fatal(err)
	}
	for ip, want := range map[string]bool{"203.0.113.9": true, "203.0.113.9:5555": true, "198.51.100.77": true, "198.51.101.1": false, "203.0.113.10": false, "not-an-ip": false} {
		if got := l.Blocked(ip); got != want {
			t.Fatalf("%s: got %v want %v", ip, got, want)
		}
	}
	if ok, _ := l.Remove("203.0.113.9"); !ok || l.Blocked("203.0.113.9") {
		t.Fatal("not removed")
	}
	if ok, _ := l.Remove("203.0.113.9"); ok {
		t.Fatal("removed twice")
	}
}

// a time-boxed block ends by itself and is dropped from the file on the next write.
func TestExpiry(t *testing.T) {
	l, now := newList(t)
	l.Add("203.0.113.9", 24*time.Hour, "")
	if !l.Blocked("203.0.113.9") {
		t.Fatal("not blocked")
	}
	*now = now.Add(24*time.Hour + time.Second)
	if l.Blocked("203.0.113.9") {
		t.Fatal("still blocked after the end time")
	}
	es, _ := l.Entries()
	if len(es) != 0 {
		t.Fatal("expired entry listed")
	}
}

func TestAddAgainReplaces(t *testing.T) {
	l, _ := newList(t)
	l.Add("203.0.113.9", time.Hour, "first")
	l.Add("203.0.113.9", 0, "second")
	es, _ := l.Entries()
	if len(es) != 1 || es[0].Reason != "second" || es[0].Until != 0 {
		t.Fatalf("%+v", es)
	}
}

// a damaged file must never lock the owner out: nothing is blocked, and the error is reported on write.
func TestDamagedFileBlocksNothing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "blocklist.json"), []byte("{broken"), 0o600)
	l := Open(dir)
	if l.Blocked("203.0.113.9") {
		t.Fatal("damaged file blocked an address")
	}
	if _, err := l.Add("203.0.113.9", 0, ""); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("add on a damaged file: %v", err)
	}
}

func TestFileIsPrivate(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir)
	l.Add("203.0.113.9", 0, "")
	fi, err := os.Stat(filepath.Join(dir, "blocklist.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtimeIsUnix() && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
}

func runtimeIsUnix() bool { return os.PathSeparator == '/' }
