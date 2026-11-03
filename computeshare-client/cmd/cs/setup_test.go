package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alishafiee1/computeshare-client/internal/client/sshconfig"
	"github.com/alishafiee1/computeshare-client/internal/client/trust"
	"github.com/alishafiee1/computeshare-client/proto"
)

// client 022 R2: only SHA256:<43 base64 characters> is accepted.
func TestValidFingerprint(t *testing.T) {
	good := "SHA256:" + strings.Repeat("A", 43)
	if !validFingerprint(good) || !validFingerprint("SHA256:+TbWYUCGabcdefghijklmnopqrstuvwxyz0123456/0") {
		t.Fatal("valid fingerprint rejected")
	}
	for _, bad := range []string{"", "SHA256:", "SHA256:short", "sha256:" + strings.Repeat("A", 43), "SHA256:" + strings.Repeat("A", 44), "SHA256:" + strings.Repeat("A", 42) + "=", "MD5:" + strings.Repeat("A", 43), "hello"} {
		if validFingerprint(bad) {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// client 022 R3: an IP server needs --server-host; named servers do not.
func TestIsIPHost(t *testing.T) {
	for in, want := range map[string]bool{"1.2.3.4:2222": true, "1.2.3.4": true, "[::1]:2222": true, "node.n3xus.ir:2222": false, "node.n3xus.ir": false} {
		if got := isIPHost(in); got != want {
			t.Fatalf("%s: got %v want %v", in, got, want)
		}
	}
}

func TestNodesRequestHost(t *testing.T) {
	req, err := nodesRequest(Config{API: "https://203.0.113.9", APIHost: "node.example.ir"})
	if err != nil || req.Host != "node.example.ir" || req.URL.Host != "203.0.113.9" {
		t.Fatalf("host=%q url=%q err=%v", req.Host, req.URL.Host, err)
	}
	req, _ = nodesRequest(Config{API: "https://node.example.ir"})
	if req.Host != req.URL.Host {
		t.Fatalf("host overridden without an IP URL: %q", req.Host)
	}
}

// client 019 R1-R4: cs setup is idempotent and reports the trust state per node.
func TestSetupIdempotent(t *testing.T) {
	const hk = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGm1+4CH6JCGeM0fBxxizeBxZk0R8hX8mKD4dYNr8w7r"
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	row := proto.NodeRow{Name: "box", Status: "online", HostKey: hk, RegisteredAt: 5, DevicePubkey: proto.EncodePubkey(pub),
		HostKeySig: proto.Sign(priv, proto.RegistrationBytes("box", hk, 5))}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]proto.NodeRow{row})
	}))
	defer api.Close()
	cfg, home := t.TempDir(), t.TempDir()
	t.Setenv("CS_CONFIG_DIR", cfg)
	t.Setenv("CS_SSH_DIR", home)
	// no config yet: setup creates one from the flags
	setupCmd([]string{"--server", "h:2222", "--api", api.URL})
	c, err := loadOpt()
	if err != nil || c.Server != "h:2222" || c.API != api.URL || c.User != "user" {
		t.Fatalf("config: %+v err=%v", c, err)
	}
	first, _ := os.ReadFile(filepath.Join(home, "config"))
	khFirst, _ := os.ReadFile(filepath.Join(home, "cs_known_hosts"))
	setupCmd(nil)
	second, _ := os.ReadFile(filepath.Join(home, "config"))
	khSecond, _ := os.ReadFile(filepath.Join(home, "cs_known_hosts"))
	if string(first) != string(second) || strings.Count(string(second), sshconfig.Begin) != 1 {
		t.Fatal(string(second))
	}
	if string(khFirst) != string(khSecond) || !strings.Contains(string(khSecond), "box.cs,box "+hk) {
		t.Fatal(string(khSecond))
	}
	// pinned by an owner fingerprint: stays pinned, setup does not downgrade it
	fp, _ := proto.Fingerprint(hk)
	ts := trust.Load(cfg)
	ts.Trust("box", fp)
	ts.Save()
	setupCmd(nil)
	if trust.Load(cfg).Fingerprint("box") != fp {
		t.Fatal("owner pin was lost")
	}
}
