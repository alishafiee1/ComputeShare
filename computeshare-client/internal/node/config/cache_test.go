package config

import (
	"os"
	"testing"
	"time"
)

// client 016 R6: state changes written by another process are seen without re-reading
// unchanged files on every check.
func TestStateCache(t *testing.T) {
	t.Setenv("CSNODE_DIR", t.TempDir())
	if LoadState().Stopped {
		t.Fatal("fresh state stopped")
	}
	State{Stopped: true}.Save()
	if !LoadState().Stopped {
		t.Fatal("stop not seen")
	}
	p := path("state.json")
	// another process (csnode start) replaces the file
	os.WriteFile(p+".x", []byte(`{"stopped":false}`), 0o600)
	os.Chtimes(p+".x", time.Now().Add(time.Second), time.Now().Add(time.Second))
	os.Rename(p+".x", p)
	if LoadState().Stopped {
		t.Fatal("external change not seen")
	}
	cache.Lock()
	e := cache.m[p]
	e.b = []byte(`{"stopped":true}`) // poison: proves an unchanged file is served from memory
	cache.m[p] = e
	cache.Unlock()
	if !LoadState().Stopped {
		t.Fatal("unchanged file was re-read")
	}
}
