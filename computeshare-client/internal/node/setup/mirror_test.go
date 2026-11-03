package setup

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alishafiee1/computeshare-client/internal/release"
)

func TestMirrorCheck(t *testing.T) {
	have := map[string]bool{"/mirror/READY": true, "/mirror/apt/debian/dists/bookworm/Release": true}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !have[r.URL.Path] {
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	if err := CheckMirror(s.URL + "/mirror"); err == nil || err.Error() != "mirror is missing images/streams/v1/index.json" {
		t.Fatal(err)
	}
	var note string
	m, _ := ResolveMirror("auto", s.URL, func(x string) { note = x })
	if m != "" || !strings.Contains(note, "using upstream") {
		t.Fatal(m, note)
	}
	have["/mirror/images/streams/v1/index.json"] = true
	if m, err := ResolveMirror("auto", s.URL, nil); err != nil || m != s.URL+"/mirror" {
		t.Fatal(m, err)
	}
	if !strings.Contains(SpaceSourcesScript("http://x/mirror"), "deb http://x/mirror/apt/debian $VERSION_CODENAME") {
		t.Fatal("sources")
	}
}

func TestVerifyModel(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "a.gguf"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(d, "SHA256SUMS"), []byte("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824  a.gguf\n0000  b.bin\n"), 0o644)
	bad, err := VerifyModel(d)
	if err != nil || len(bad) != 1 || bad[0] != "b.bin (missing)" {
		t.Fatal(bad, err)
	}
}

// client 014 R3: images.lock must be signed by the release key; unknown aliases are refused.
func TestLockedFingerprint(t *testing.T) {
	fp := strings.Repeat("ab", 32)
	lock := "# alias arch fingerprint\ndebian/12 " + runtime.GOARCH + " " + fp + "\n"
	sig := "bad"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/m/images/images.lock":
			w.Write([]byte(lock))
		case "/m/images/images.lock.sig":
			w.Write([]byte(sig))
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	_, err := LockedFingerprint(s.URL+"/m", "debian/12")
	if !errors.Is(err, ErrImageTrust) {
		t.Fatal("unsigned lock accepted:", err)
	}
	if got := ParseImageLock([]byte(lock)); got["debian/12 "+runtime.GOARCH] != fp || len(got) != 1 {
		t.Fatal(got)
	}
	if _, err := LockedFingerprint(s.URL+"/nomirror", "debian/12"); !errors.Is(err, ErrImageTrust) {
		t.Fatal("missing lock must fail closed:", err)
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	verifyLock = func(m, s []byte) error { return release.Verify(pub, m, s) }
	defer func() { verifyLock = release.VerifyEmbedded }()
	sig = string(release.Sign(priv, []byte(lock)))
	if got, err := LockedFingerprint(s.URL+"/m", "debian/12"); err != nil || got != fp {
		t.Fatal("signed lock refused:", got, err)
	}
	if _, err := LockedFingerprint(s.URL+"/m", "ubuntu/24.04"); !errors.Is(err, ErrImageTrust) {
		t.Fatal("alias outside the lock accepted:", err)
	}
	lock = strings.Replace(lock, fp, strings.Repeat("cd", 32), 1) // tampered after signing
	if _, err := LockedFingerprint(s.URL+"/m", "debian/12"); !errors.Is(err, ErrImageTrust) {
		t.Fatal("tampered lock accepted:", err)
	}
}

// client 014 R1 and edge case: no trusted=yes, old sources are rewritten.
func TestAptSource(t *testing.T) {
	if strings.Contains(AptSource("http://m", "bookworm"), "trusted") {
		t.Fatal("trusted=yes in apt source")
	}
	AptSourceFile = filepath.Join(t.TempDir(), "computeshare.list")
	os.WriteFile(AptSourceFile, []byte("deb [trusted=yes] http://m/apt/debian bookworm main\n"), 0o644)
	if fixed, err := FixAptSource(); !fixed || err != nil {
		t.Fatal(fixed, err)
	}
	b, _ := os.ReadFile(AptSourceFile)
	if string(b) != "deb http://m/apt/debian bookworm main\n" {
		t.Fatalf("%q", b)
	}
	if fixed, _ := FixAptSource(); fixed {
		t.Fatal("second run changed it again")
	}
}
