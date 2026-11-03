package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdempotentAndOutside(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte("Host foo\n  User x\n"), 0o600)
	b := Block("203.0.113.10", "user", false)
	Apply(p, b)
	Apply(p, b)
	s, _ := os.ReadFile(p)
	if strings.Count(string(s), Begin) != 1 || !strings.HasPrefix(string(s), "Host foo\n  User x\n") || !strings.Contains(string(s), "ProxyCommand ssh -W %n:22 join@203.0.113.10") {
		t.Fatal(string(s))
	}
	os.WriteFile(p, append(s, []byte("Host *.cs\n  User y\n")...), 0o600)
	if w, _ := Apply(p, b); w == "" {
		t.Fatal("no warning for unmarked block")
	}
}

// a key with a non-default name must be named in the block, and every block makes dead connections visible.
func TestIdentityAndAlive(t *testing.T) {
	b := BlockWith("h", "u", false, "/home/me/.ssh/id_ed25519_cs")
	for _, want := range []string{"  IdentityFile /home/me/.ssh/id_ed25519_cs\n", "  IdentitiesOnly yes\n", "  ServerAliveInterval 30\n", "  ServerAliveCountMax 4\n"} {
		if !strings.Contains(b, want) {
			t.Fatalf("missing %q in\n%s", want, b)
		}
	}
	if b := Block("h", "u", false); strings.Contains(b, "IdentityFile") || !strings.Contains(b, "ServerAliveInterval 30") {
		t.Fatal(b)
	}
}

// client 017 R4: connection sharing except on Windows.
func TestMux(t *testing.T) {
	if b := Block("h", "u", true); !strings.Contains(b, "  ControlMaster auto\n  ControlPath ~/.ssh/cs-%C\n  ControlPersist 10m\n"+End) {
		t.Fatal(b)
	}
	if strings.Contains(Block("h", "u", false), "Control") || Mux("windows") || !Mux("linux") {
		t.Fatal("windows must not get ControlMaster")
	}
}
