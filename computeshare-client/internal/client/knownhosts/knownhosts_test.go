package knownhosts

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/alishafiee1/computeshare-client/internal/client/trust"
	"github.com/alishafiee1/computeshare-client/proto"
)

const hk = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGm1+4CH6JCGeM0fBxxizeBxZk0R8hX8mKD4dYNr8w7r"
const hk2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGtyVzAyP3cQ0e1v1d0x8h9mJx2dS0x3sT5pQwqk4hYb"

func row(k string) proto.NodeRow {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return proto.NodeRow{Name: "myBrotherPc", HostKey: k, RegisteredAt: 5, DevicePubkey: proto.EncodePubkey(pub), HostKeySig: proto.Sign(priv, proto.RegistrationBytes("myBrotherPc", k, 5))}
}

func TestVerify(t *testing.T) {
	ts := trust.Load(t.TempDir())
	r := Build([]proto.NodeRow{row(hk)}, ts, nil)
	if len(r.Lines) != 1 || len(r.Problems) != 0 {
		t.Fatal(r)
	}
	bad := row(hk)
	bad.HostKey = hk2
	if r := Build([]proto.NodeRow{bad}, ts, nil); len(r.Lines) != 0 || r.Problems[0] != "signature invalid for myBrotherPc" {
		t.Fatal(r)
	}
	if r := Build([]proto.NodeRow{row(hk)}, ts, nil); r.Problems[0] != "device key changed for myBrotherPc; ask the owner" {
		t.Fatal(r)
	}
	fp, _ := proto.Fingerprint(hk)
	ts2 := trust.Load(t.TempDir())
	ts2.Trust("myBrotherPc", fp)
	if r := Build([]proto.NodeRow{row(hk2)}, ts2, nil); len(r.Lines) != 0 || r.Problems[0] != "fingerprint mismatch for myBrotherPc" {
		t.Fatal(r)
	}
}

// client 017 R3: nodes without an owner fingerprint are reported for a warning.
func TestTOFUOnly(t *testing.T) {
	ts := trust.Load(t.TempDir())
	ts.Trust("Alpha", "SHA256:x")
	got := TOFUOnly([]string{"alpha.cs,alpha ssh-ed25519 AAAA", "beta.cs,beta ssh-ed25519 BBBB"}, ts)
	if len(got) != 1 || got[0] != "beta" {
		t.Fatal(got)
	}
}
