package release

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func init() { scryptN = 1 << 10 }

func TestGenerateSignVerify(t *testing.T) {
	if _, _, err := Generate([]byte("short")); err != ErrShortPass {
		t.Fatal("short passphrase accepted")
	}
	pass := []byte("correct horse battery")
	privFile, pubPEM, err := Generate(pass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(privFile, []byte("wrong passphrase!!")); err != ErrBadPass {
		t.Fatal("wrong passphrase opened the key")
	}
	priv, err := Open(privFile, pass)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ParsePublic(pubPEM)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("abc  csnode-linux-amd64\n")
	sig := Sign(priv, msg)
	if err := Verify(pub, msg, sig); err != nil {
		t.Fatal(err)
	}
	if Verify(pub, append(msg, 'x'), sig) != ErrBadSig {
		t.Fatal("tampered manifest verified")
	}
	sig[0] ^= 1
	if Verify(pub, msg, sig) != ErrBadSig {
		t.Fatal("tampered signature verified")
	}
}

func TestNoEmbeddedKeyFailsClosed(t *testing.T) {
	if len(embeddedPub) == 0 && VerifyEmbedded([]byte("x"), make([]byte, 64)) != ErrNoKey {
		t.Fatal("missing key must fail closed")
	}
}

// The shell installer verifies with openssl; the formats must interoperate.
func TestOpenSSLInterop(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not installed")
	}
	pass := []byte("correct horse battery")
	privFile, pubPEM, _ := Generate(pass)
	priv, _ := Open(privFile, pass)
	d := t.TempDir()
	msg := []byte("deadbeef  cs-linux-amd64\n")
	os.WriteFile(filepath.Join(d, "pub.pem"), pubPEM, 0o644)
	os.WriteFile(filepath.Join(d, "SHA256SUMS"), msg, 0o644)
	os.WriteFile(filepath.Join(d, "SHA256SUMS.sig"), Sign(priv, msg), 0o644)
	run := func() error {
		return exec.Command("openssl", "pkeyutl", "-verify", "-pubin", "-inkey", filepath.Join(d, "pub.pem"),
			"-rawin", "-in", filepath.Join(d, "SHA256SUMS"), "-sigfile", filepath.Join(d, "SHA256SUMS.sig")).Run()
	}
	if err := run(); err != nil {
		t.Fatal("openssl rejected a good signature:", err)
	}
	os.WriteFile(filepath.Join(d, "SHA256SUMS"), append(msg, 'x'), 0o644)
	if run() == nil {
		t.Fatal("openssl accepted a tampered manifest")
	}
}

func TestVerifyFile(t *testing.T) {
	pass := []byte("correct horse battery")
	privFile, pubPEM, _ := Generate(pass)
	priv, _ := Open(privFile, pass)
	old := embeddedPub
	embeddedPub = pubPEM
	defer func() { embeddedPub = old }()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "rootfs.tar.gz"), []byte("rootfs"), 0o644)
	sums := []byte("6c4bd4f7a48bbbc7d4d9bce6c3a0b3dc8c4bd7b2ba22b52e0d3d9bf0a7f3f1d6  other\n" +
		"bc2f9d1b1c2fe0d4ad3a9d2a2c88d4bb0cbbbca8c5c9a6ae58e5e2fa3ff3b9b4  rootfs.tar.gz\n")
	os.WriteFile(filepath.Join(d, "SHA256SUMS"), sums, 0o644)
	os.WriteFile(filepath.Join(d, "SHA256SUMS.sig"), Sign(priv, sums), 0o644)
	if err := VerifyFile(filepath.Join(d, "rootfs.tar.gz")); err == nil {
		t.Fatal("wrong checksum accepted")
	}
	h := sha256.Sum256([]byte("rootfs"))
	sums = []byte(hex.EncodeToString(h[:]) + "  rootfs.tar.gz\n")
	os.WriteFile(filepath.Join(d, "SHA256SUMS"), sums, 0o644)
	if VerifyFile(filepath.Join(d, "rootfs.tar.gz")) == nil {
		t.Fatal("stale signature accepted")
	}
	os.WriteFile(filepath.Join(d, "SHA256SUMS.sig"), Sign(priv, sums), 0o644)
	if err := VerifyFile(filepath.Join(d, "rootfs.tar.gz")); err != nil {
		t.Fatal(err)
	}
	// Git Bash on Windows writes "<sum> *<name>" (one space, binary mark): it must be found too
	sums = []byte(hex.EncodeToString(h[:]) + " *rootfs.tar.gz\n")
	os.WriteFile(filepath.Join(d, "SHA256SUMS"), sums, 0o644)
	os.WriteFile(filepath.Join(d, "SHA256SUMS.sig"), Sign(priv, sums), 0o644)
	if err := VerifyFile(filepath.Join(d, "rootfs.tar.gz")); err != nil {
		t.Fatal("windows sha256sum format refused: ", err)
	}
}
