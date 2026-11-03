package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alishafiee1/computeshare-client/internal/client/sshconfig"
	"github.com/alishafiee1/computeshare-client/internal/client/trust"
	"github.com/alishafiee1/computeshare-client/proto"
)

// client 017 R5: clear messages instead of "bad response".
func TestStatusMessage(t *testing.T) {
	for code, want := range map[int]string{200: "", 429: "rate limited", 403: "temporarily blocked", 502: "server error 502"} {
		if got := statusMessage(code); (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Fatal(code, got)
		}
	}
}

func TestSameServer(t *testing.T) {
	if !sameServer("node.n3xus.ir", "node.n3xus.ir:2222") || sameServer("a.ir:2222", "b.ir:2222") {
		t.Fatal("sameServer")
	}
}

// client 017 R4: cs ssh passes the same sharing options as the ssh config block.
func TestSSHBaseMux(t *testing.T) {
	b := strings.Join(sshBase(Config{Server: "h:2222"}), " ")
	has := strings.Contains(b, "-o ControlMaster=auto -o ControlPath=~/.ssh/cs-%C -o ControlPersist=10m")
	// Windows OpenSSH has no ControlMaster support: the options are left out there (client 017 R4).
	if want := sshconfig.Mux(runtime.GOOS); has != want {
		t.Fatalf("mux options present=%v want=%v: %s", has, want, b)
	}
}

// client 017 R2: cs add sets up ssh config, trust and known_hosts; running it twice changes nothing.
func TestAddIdempotent(t *testing.T) {
	const hk = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGm1+4CH6JCGeM0fBxxizeBxZk0R8hX8mKD4dYNr8w7r"
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	row := proto.NodeRow{Name: "box", HostKey: hk, RegisteredAt: 5, DevicePubkey: proto.EncodePubkey(pub),
		HostKeySig: proto.Sign(priv, proto.RegistrationBytes("box", hk, 5))}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]proto.NodeRow{row})
	}))
	defer api.Close()
	cfg, home := t.TempDir(), t.TempDir()
	t.Setenv("CS_CONFIG_DIR", cfg)
	t.Setenv("CS_SSH_DIR", home)
	b, _ := json.Marshal(Config{Server: "h:2222", API: api.URL, User: "user"})
	os.WriteFile(filepath.Join(cfg, "config.json"), b, 0o600)
	fp, _ := proto.Fingerprint(hk)
	add([]string{"box", fp})
	first, _ := os.ReadFile(filepath.Join(home, "config"))
	add([]string{"box", fp})
	second, _ := os.ReadFile(filepath.Join(home, "config"))
	if string(first) != string(second) || strings.Count(string(second), sshconfig.Begin) != 1 {
		t.Fatal(string(second))
	}
	kh, _ := os.ReadFile(filepath.Join(home, "cs_known_hosts"))
	if !strings.Contains(string(kh), "box.cs,box "+hk) {
		t.Fatal(string(kh))
	}
	if trust.Load(cfg).Fingerprint("box") != fp {
		t.Fatal("fingerprint not stored")
	}
}
