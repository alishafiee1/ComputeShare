package limits

import (
	"testing"
	"time"
)

type mem map[string]time.Time

func (m mem) Ban(k, v string, d time.Duration, _ string) error {
	m[k+v] = time.Now().Add(d)
	return nil
}
func (m mem) IsBanned(k, v string) bool { return time.Now().Before(m[k+v]) }

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTest(cfg Config) (*Limiter, mem, *clock) {
	m := mem{}
	l := New(cfg, m)
	c := &clock{time.Unix(1_700_000_000, 0)}
	l.Now = c.now
	return l, m, c
}

// 008 R1, R2: an empty bucket refuses but never bans; tokens refill.
func TestBucketRefusesWithoutBan(t *testing.T) {
	l, m, c := newTest(Defaults())
	for i := 0; i < 20; i++ {
		if !l.Allow(SSH, "1.1.1.1") {
			t.Fatal("blocked within burst", i)
		}
	}
	if l.Allow(SSH, "1.1.1.1") {
		t.Fatal("burst exceeded but allowed")
	}
	if m.IsBanned("ip", "1.1.1.1") {
		t.Fatal("volume alone must not ban")
	}
	c.t = c.t.Add(time.Second) // 60/min = 1 token per second
	if !l.Allow(SSH, "1.1.1.1") {
		t.Fatal("token not refilled")
	}
	if !l.Allow(SSH, "2.2.2.2") {
		t.Fatal("other ip blocked")
	}
}

// 008 R1: kinds are independent.
func TestKindsIndependent(t *testing.T) {
	l, _, _ := newTest(Defaults())
	for i := 0; i < 6; i++ {
		l.Allow(Tunnel, "1.1.1.1")
	}
	if l.Allow(Tunnel, "1.1.1.1") {
		t.Fatal("tunnel burst 6 exceeded")
	}
	if !l.Allow(SSH, "1.1.1.1") || !l.Allow(API, "1.1.1.1") {
		t.Fatal("tunnel exhaustion leaked into ssh/api")
	}
}

// Legacy ConnPerMin keeps its meaning for every kind (flag and e2e labs).
func TestLegacyConnPerMin(t *testing.T) {
	l, m, _ := newTest(Config{ConnPerMin: 3})
	for i := 0; i < 3; i++ {
		if !l.Allow(API, "1.1.1.1") {
			t.Fatal(i)
		}
	}
	if l.Allow(API, "1.1.1.1") || m.IsBanned("ip", "1.1.1.1") {
		t.Fatal("want refused, not banned")
	}
}

// 008 R3: abuse signals ban once the threshold is reached.
func TestStrikeBans(t *testing.T) {
	l, m, _ := newTest(Defaults())
	for i := 0; i < 9; i++ {
		if l.Strike(BadHandshake, "6.6.6.6") {
			t.Fatal("banned early", i)
		}
	}
	if !l.Strike(BadHandshake, "6.6.6.6") || !m.IsBanned("ip", "6.6.6.6") {
		t.Fatal("10th handshake failure must ban")
	}
	if l.Allow(SSH, "6.6.6.6") {
		t.Fatal("banned ip allowed")
	}
	for i := 0; i < 4; i++ {
		l.Strike(BadToken, "7.7.7.7")
	}
	if !l.Strike(BadToken, "7.7.7.7") {
		t.Fatal("5th invalid token must ban")
	}
}

// Strikes outside the window do not add up.
func TestStrikeWindow(t *testing.T) {
	l, _, c := newTest(Defaults())
	for i := 0; i < 9; i++ {
		l.Strike(UnknownNode, "8.8.8.8")
	}
	c.t = c.t.Add(61 * time.Second)
	if l.Strike(UnknownNode, "8.8.8.8") {
		t.Fatal("old strikes counted")
	}
}

// 008 R4: concurrency caps refuse without banning.
func TestConcurrent(t *testing.T) {
	l, m, _ := newTest(Defaults())
	for i := 0; i < 40; i++ {
		if !l.Acquire("ip", "n") {
			t.Fatal(i)
		}
	}
	if l.Acquire("ip", "n") {
		t.Fatal("41st stream")
	}
	if m.IsBanned("ip", "ip") {
		t.Fatal("concurrency must not ban")
	}
	l.Release("ip", "n")
	if !l.Acquire("ip", "n") {
		t.Fatal("slot not released")
	}
	cfg := Defaults()
	cfg.ConcurrentPerIP = 1000
	l2, _, _ := newTest(cfg)
	for i := 0; i < 50; i++ {
		l2.Acquire(string(rune('a'+i%26))+string(rune(i)), "node")
	}
	if l2.Acquire("x", "node") {
		t.Fatal("per-node cap 50")
	}
}

// 008 R5: idle entries are pruned.
func TestPrune(t *testing.T) {
	l, _, c := newTest(Defaults())
	for i := 0; i < 100; i++ {
		l.Allow(SSH, "10.0.0."+string(rune('0'+i%10))+string(rune('a'+i/10)))
	}
	if l.Size(SSH) != 100 {
		t.Fatal(l.Size(SSH))
	}
	c.t = c.t.Add(11 * time.Minute)
	l.Allow(SSH, "1.1.1.1")
	if l.Size(SSH) != 1 {
		t.Fatal("not pruned", l.Size(SSH))
	}
}
