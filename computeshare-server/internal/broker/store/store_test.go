package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
)

func open(t *testing.T) (*Store, string) {
	p := filepath.Join(t.TempDir(), "db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, p
}

func req(t *testing.T, name, tok string) *proto.RegisterRequest {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	hk := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGm1+4CH6JCGeM0fBxxizeBxZk0R8hX8mKD4dYNr8w7r"
	r := &proto.RegisterRequest{Proto: 1, Name: name, Token: tok, DevicePubkey: proto.EncodePubkey(pub), HostKey: hk, RegisteredAt: time.Now().Unix()}
	r.HostKeySig = proto.Sign(priv, proto.RegistrationBytes(name, hk, r.RegisteredAt))
	return r
}

func TestTokenHashedOnly(t *testing.T) {
	s, p := open(t)
	_, tok, err := s.TokenCreate("family", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	secret := tok[strings.LastIndex(tok, ".")+1:]
	s.Close()
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), secret) {
		t.Fatal("token secret stored in clear")
	}
}

func TestRegisterNameCaseAndToken(t *testing.T) {
	s, _ := open(t)
	_, tok, _ := s.TokenCreate("family", 0, 0)
	n, err := s.Register(req(t, "myBrotherPc", tok))
	if err != nil || n.Status != "offline" {
		t.Fatal(err, n)
	}
	if _, err := s.Register(req(t, "MyBrotherPC", tok)); err != ErrNameTaken {
		t.Fatal("case-insensitive uniqueness", err)
	}
	s.Release(n.ID)
	if _, err := s.Register(req(t, "myBrotherPc", tok)); err != nil {
		t.Fatal("released name reusable", err)
	}
	if _, err := s.Register(req(t, "badtok", "cs1.x.y")); err != ErrTokenInvalid {
		t.Fatal(err)
	}
}

func TestRequireApproval(t *testing.T) {
	s, _ := open(t)
	s.RequireApproval = true
	_, tok, _ := s.TokenCreate("f", 0, 0)
	n, err := s.Register(req(t, "pendingOne", tok))
	if err != nil || n.Status != "pending" {
		t.Fatal(err, n)
	}
	if err := s.Approve(n.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.NodeByID(n.ID); got.Status != "offline" {
		t.Fatal(got.Status)
	}
	if err := s.Approve(n.ID); err != ErrNotFound {
		t.Fatal("double approve", err)
	}
}

func TestTokenLimits(t *testing.T) {
	s, _ := open(t)
	_, one, _ := s.TokenCreate("one", 0, 1)
	if _, err := s.Register(req(t, "aaa", one)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register(req(t, "bbb", one)); err != ErrTokenInvalid {
		t.Fatal("max uses", err)
	}
	id, rv, _ := s.TokenCreate("rv", 0, 0)
	s.TokenRevoke(id)
	if _, err := s.Register(req(t, "ccc", rv)); err != ErrTokenInvalid {
		t.Fatal("revoked", err)
	}
	_, ex, _ := s.TokenCreate("ex", time.Second, 0)
	s.Now = func() time.Time { return time.Now().Add(time.Hour) }
	if _, err := s.Register(req(t, "ddd", ex)); err != ErrTokenInvalid {
		t.Fatal("expired", err)
	}
}

func TestConcurrentSameName(t *testing.T) {
	s, _ := open(t)
	_, tok, _ := s.TokenCreate("f", 0, 0)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, taken := 0, 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Register(req(t, "raceName", tok))
			mu.Lock()
			if err == nil {
				ok++
			} else if err == ErrNameTaken {
				taken++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if ok != 1 || taken != 7 {
		t.Fatal(ok, taken)
	}
}

func TestSamplesAndAvg(t *testing.T) {
	s, _ := open(t)
	base := time.Now()
	for i := 0; i < 12; i++ {
		s.Now = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		s.AddSample("n1", proto.Stats{CPUPct: 40})
		// second sample in the same minute is ignored
		ok, _ := s.AddSample("n1", proto.Stats{CPUPct: 99})
		if ok {
			t.Fatal("more than one sample per minute")
		}
	}
	if a := s.Avg24h("n1"); a == nil || *a != 40 {
		t.Fatal("avg", a)
	}
	if s.Avg24h("n2") != nil {
		t.Fatal("avg must be nil under 10 samples")
	}
	s.Now = func() time.Time { return base.Add(8 * 24 * time.Hour) }
	s.Prune()
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM stats_samples`).Scan(&n)
	if n != 0 {
		t.Fatal("7-day retention")
	}
}

func TestSessionRetentionAndBans(t *testing.T) {
	s, _ := open(t)
	id := s.SessionStart("n1", "jump", "1.2.3.4")
	s.SessionEnd(id, 10, 20)
	ss, _ := s.Sessions()
	if len(ss) != 1 || ss[0].Up != 10 || ss[0].SrcIP != "1.2.3.4" {
		t.Fatal(ss)
	}
	s.Ban("ip", "1.2.3.4", 10*time.Minute, "x")
	if !s.IsBanned("ip", "1.2.3.4") {
		t.Fatal("ban")
	}
	s.Now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	if s.IsBanned("ip", "1.2.3.4") {
		t.Fatal("ban expiry")
	}
	s.Prune()
	ss, _ = s.Sessions()
	if len(ss) != 0 {
		t.Fatal("30-day session retention")
	}
}
