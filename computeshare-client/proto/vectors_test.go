package proto

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden vectors")

// Golden vectors (R5): deterministic seed so node and broker tests share them.
type vectors struct {
	SeedHex      string          `json:"seed_hex_doc"`
	DevicePubkey string          `json:"device_pubkey"`
	Register     RegisterRequest `json:"register"`
	HelloNodeID  string          `json:"hello_node_id"`
	HelloTS      int64           `json:"hello_ts"`
	HelloSig     string          `json:"hello_sig"`
	LeaveSig     string          `json:"leave_sig"`
	Fingerprint  string          `json:"host_key_fingerprint"`
}

func vectorKey() ed25519.PrivateKey {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

const vecHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGtyVzAyP3cQ0e1v1d0x8h9mJx2dS0x3sT5pQwqk4hYb"

func build() vectors {
	k := vectorKey()
	pub := EncodePubkey(k.Public().(ed25519.PublicKey))
	r := RegisterRequest{Proto: 1, Name: "myBrotherPc", Token: "<redacted-in-vectors>", DevicePubkey: pub,
		HostKey: vecHostKey, RegisteredAt: 1792130000, OS: "linux", Arch: "amd64",
		Specs: Specs{CPU: "8c", RAMGB: 16, GPU: "RTX 4070 12GB", DiskGB: 100}, Limits: Limits{"20Mbit", "20GB/day"}, Version: "0.1.0"}
	r.HostKeySig = Sign(k, RegistrationBytes(r.Name, r.HostKey, r.RegisteredAt))
	fp, _ := Fingerprint(vecHostKey)
	return vectors{"000102..1f", pub, r, "n_01HVECTOR", 1792130100,
		Sign(k, HelloBytes("n_01HVECTOR", 1792130100)), Sign(k, LeaveBytes("n_01HVECTOR", 1792130100)), fp}
}

func TestGoldenVectors(t *testing.T) {
	got := build()
	path := "testdata/vectors.json"
	if *update {
		b, _ := json.MarshalIndent(got, "", "  ")
		os.WriteFile(path, append(b, '\n'), 0o644)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want vectors
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if want.Register.HostKeySig != got.Register.HostKeySig || want.HelloSig != got.HelloSig || want.LeaveSig != got.LeaveSig || want.Fingerprint != got.Fingerprint {
		t.Fatal("golden vectors drifted: wire format changed without bumping proto")
	}
	if !Verify(want.DevicePubkey, RegistrationBytes(want.Register.Name, want.Register.HostKey, want.Register.RegisteredAt), want.Register.HostKeySig) {
		t.Fatal("registration sig")
	}
	if !Verify(want.DevicePubkey, HelloBytes(want.HelloNodeID, want.HelloTS), want.HelloSig) {
		t.Fatal("hello sig")
	}
	if Verify(want.DevicePubkey, LeaveBytes(want.HelloNodeID, want.HelloTS), want.HelloSig) {
		t.Fatal("hello sig must not verify as leave (domain separation)")
	}
	if err := want.Register.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationCaseInsensitiveName(t *testing.T) {
	if string(RegistrationBytes("MyBrotherPC", "k", 1)) != string(RegistrationBytes("mybrotherpc", "k", 1)) {
		t.Fatal("name must be case-folded in signing bytes")
	}
}

func TestTamper(t *testing.T) {
	v := build()
	if Verify(v.DevicePubkey, RegistrationBytes(v.Register.Name, v.Register.HostKey+"x", v.Register.RegisteredAt), v.Register.HostKeySig) {
		t.Fatal("tampered host key verified")
	}
	if Verify("ed25519:AAAA", []byte("x"), v.HelloSig) || Verify(v.DevicePubkey, []byte("x"), "!!") {
		t.Fatal("garbage verified")
	}
}

func TestValidators(t *testing.T) {
	for n, ok := range map[string]bool{"myBrotherPc": true, "ab": false, "abc": true, "1abc": false, "a-b-c": true, "a_bc": false, "a23456789012345678901234567890123": false} {
		if ValidName(n) != ok {
			t.Errorf("name %q", n)
		}
	}
	for u, ok := range map[string]bool{"user": true, "User": false, "_x": true, "9a": false, "": false} {
		if ValidUser(u) != ok {
			t.Errorf("user %q", u)
		}
	}
	if NameTakenMessage("MyBrotherPC") != `name "MyBrotherPC" is already taken; choose another` {
		t.Fatal("msg")
	}
}

func TestLimits(t *testing.T) {
	bw := map[string]int64{"20Mbit": 20e6, "": 0, "off": 0, "500kbit": 500e3, "1Gbit": 1e9, "1.5Mbit": 15e5}
	for s, w := range bw {
		if g, err := ParseBandwidth(s); err != nil || g != w {
			t.Errorf("bw %q=%d %v", s, g, err)
		}
	}
	q := map[string]int64{"20GB/day": 20e9, "": 0, "1GB/day": 1e9, "500MB/day": 500e6}
	for s, w := range q {
		if g, err := ParseQuota(s); err != nil || g != w {
			t.Errorf("q %q=%d %v", s, g, err)
		}
	}
	for _, s := range []string{"20MB", "x", "-1Mbit", "0Mbit"} {
		if _, err := ParseBandwidth(s); err == nil {
			t.Errorf("bw %q should fail", s)
		}
	}
	if _, err := ParseQuota("20GB"); err == nil {
		t.Error("quota without /day")
	}
}

func TestUnknownFieldsAndMissing(t *testing.T) {
	var r RegisterRequest
	if err := json.Unmarshal([]byte(`{"proto":2,"name":"abc","future":1}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Validate() == nil {
		t.Fatal("missing fields must fail")
	}
	if typ, _ := PeekType([]byte(`{"type":"Ping","x":2}`)); typ != TypePing {
		t.Fatal(typ)
	}
	if SkewOK(1000, 1121) || !SkewOK(1000, 880) {
		t.Fatal("skew")
	}
}
