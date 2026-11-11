package users

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"runtime"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/alishafiee1/computeshare-client/internal/node/config"
)

func key(t *testing.T) (ssh.PublicKey, string) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	pk, _ := ssh.NewPublicKey(pub)
	return pk, string(ssh.MarshalAuthorizedKey(pk))
}

// 015 R3: names, password rules, argon2id, keys, persistence with 0600.
func TestStore(t *testing.T) {
	t.Setenv("CSNODE_DIR", t.TempDir())
	pk, line := key(t)
	other, _ := key(t)
	err := Update(func(s *Store) error {
		for _, bad := range []string{"root", "Ali", "1ali", "a b", ""} {
			if s.Add(bad, "long enough password", nil, "") != ErrName {
				t.Fatal("accepted name", bad)
			}
		}
		if s.Add("ali", "short", nil, "") != ErrShort || s.Add("ali", "", nil, "") != ErrNoAuth {
			t.Fatal("password rules")
		}
		if err := s.Add("ali", "correct horse battery", []string{line}, "20GB"); err != nil {
			return err
		}
		if s.Add("ali", "correct horse battery", nil, "") != ErrExists {
			t.Fatal("duplicate")
		}
		return s.Add("sara", "", []string{line}, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(config.Path(File))
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 { // Windows has no unix mode bits
		t.Fatal("users.json mode", fi.Mode())
	}
	s := Load()
	u, ok := s.Get("ali")
	if !ok || !u.CheckPassword("correct horse battery") || u.CheckPassword("correct horse batterx") {
		t.Fatal("password check")
	}
	if sara, _ := s.Get("sara"); sara.CheckPassword("") {
		t.Fatal("no-password user matched empty password")
	}
	if !u.HasKey(pk) || u.HasKey(other) || u.Disk != "20GB" {
		t.Fatal("keys")
	}
	if !u.RemoveKey("key1") || u.HasKey(pk) {
		t.Fatal("remove key")
	}
	if Instance("box", "ali") != "cs-box-ali" {
		t.Fatal("instance name")
	}
}
