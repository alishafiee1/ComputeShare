// Package limits implements per-source-IP and per-node rate limits (broker 004, revised by 008).
// Running out of tokens only refuses a request; bans are reserved for abuse signals (008 R2, R3).
package limits

import (
	"sync"
	"time"
)

// Kind selects an independent token bucket per client IP (008 R1).
type Kind string

const (
	SSH    Kind = "ssh"    // jump / list connections on :2222
	Tunnel Kind = "tunnel" // node /v1/tunnel
	API    Kind = "api"    // /v1/register, /v1/names, /v1/nodes
)

// Abuse signals that can lead to a ban (008 R3).
type Abuse string

const (
	BadHandshake Abuse = "failed ssh handshakes"
	UnknownNode  Abuse = "requests for unknown nodes"
	BadToken     Abuse = "invalid join tokens"
)

// Rate is a token bucket: PerMin tokens refill per minute, up to Burst.
type Rate struct {
	PerMin int
	Burst  int
}

type Config struct {
	// ConnPerMin is the legacy single limit (flag -limit-conn-per-min). When a per-kind
	// rate below is zero it falls back to ConnPerMin/minute with burst ConnPerMin.
	ConnPerMin        int
	SSH, Tunnel, API  Rate
	ConcurrentPerIP   int           // concurrent streams per IP (default 40)
	ConcurrentPerNode int           // concurrent streams per node (default 50)
	BanFor            time.Duration // default 10m
}

func Defaults() Config {
	return Config{SSH: Rate{60, 20}, Tunnel: Rate{12, 6}, API: Rate{60, 30},
		ConcurrentPerIP: 40, ConcurrentPerNode: 50, BanFor: 10 * time.Minute}
}

// abuseRule: Max events within Window from one IP → ban.
var abuseRules = map[Abuse]struct {
	Max    int
	Window time.Duration
}{
	BadHandshake: {10, time.Minute},
	UnknownNode:  {10, time.Minute},
	BadToken:     {5, 10 * time.Minute},
}

type Banner interface {
	Ban(kind, value string, d time.Duration, reason string) error
	IsBanned(kind, value string) bool
}

type bucket struct {
	tokens float64
	last   time.Time
}

type Limiter struct {
	cfg     Config
	mu      sync.Mutex
	buckets map[Kind]map[string]*bucket
	strikes map[Abuse]map[string][]time.Time
	conc    map[string]int
	nconc   map[string]int
	bans    Banner
	pruned  time.Time
	Now     func() time.Time
}

func New(cfg Config, b Banner) *Limiter {
	d := Defaults()
	for _, r := range []*Rate{&cfg.SSH, &cfg.Tunnel, &cfg.API} {
		if r.PerMin == 0 && cfg.ConnPerMin > 0 {
			*r = Rate{cfg.ConnPerMin, cfg.ConnPerMin}
		}
	}
	if cfg.SSH.PerMin == 0 {
		cfg.SSH = d.SSH
	}
	if cfg.Tunnel.PerMin == 0 {
		cfg.Tunnel = d.Tunnel
	}
	if cfg.API.PerMin == 0 {
		cfg.API = d.API
	}
	if cfg.ConcurrentPerIP == 0 {
		cfg.ConcurrentPerIP = d.ConcurrentPerIP
	}
	if cfg.ConcurrentPerNode == 0 {
		cfg.ConcurrentPerNode = d.ConcurrentPerNode
	}
	if cfg.BanFor == 0 {
		cfg.BanFor = d.BanFor
	}
	return &Limiter{cfg: cfg, bans: b, Now: time.Now,
		buckets: map[Kind]map[string]*bucket{SSH: {}, Tunnel: {}, API: {}},
		strikes: map[Abuse]map[string][]time.Time{}, conc: map[string]int{}, nconc: map[string]int{}}
}

func (l *Limiter) rate(k Kind) Rate {
	switch k {
	case SSH:
		return l.cfg.SSH
	case Tunnel:
		return l.cfg.Tunnel
	}
	return l.cfg.API
}

// Allow is called for each new inbound request of kind k. It returns false when the IP is
// banned or its bucket for k is empty; an empty bucket never bans (008 R2).
func (l *Limiter) Allow(k Kind, ip string) bool {
	if l.bans.IsBanned("ip", ip) {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	l.pruneLocked(now)
	r := l.rate(k)
	m := l.buckets[k]
	b := m[ip]
	if b == nil {
		b = &bucket{tokens: float64(r.Burst), last: now}
		m[ip] = b
	}
	b.tokens += now.Sub(b.last).Minutes() * float64(r.PerMin)
	if b.tokens > float64(r.Burst) {
		b.tokens = float64(r.Burst)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Strike records one abuse signal from ip; it bans the IP once the rule's threshold is
// reached and reports whether it did (008 R3).
func (l *Limiter) Strike(a Abuse, ip string) bool {
	rule, ok := abuseRules[a]
	if !ok {
		return false
	}
	l.mu.Lock()
	now := l.Now()
	m := l.strikes[a]
	if m == nil {
		m = map[string][]time.Time{}
		l.strikes[a] = m
	}
	h := m[ip][:0]
	for _, t := range m[ip] {
		if now.Sub(t) < rule.Window {
			h = append(h, t)
		}
	}
	h = append(h, now)
	ban := len(h) >= rule.Max
	if ban {
		delete(m, ip)
	} else {
		m[ip] = h
	}
	l.mu.Unlock()
	if ban {
		l.bans.Ban("ip", ip, l.cfg.BanFor, "abuse: "+string(a))
	}
	return ban
}

// Acquire reserves a concurrent stream slot for ip+node. Release must be called when done.
// A full slot table refuses the stream without banning (008 R4).
func (l *Limiter) Acquire(ip, node string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conc[ip] >= l.cfg.ConcurrentPerIP || l.nconc[node] >= l.cfg.ConcurrentPerNode {
		return false
	}
	l.conc[ip]++
	l.nconc[node]++
	return true
}

func (l *Limiter) Release(ip, node string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conc[ip]--; l.conc[ip] <= 0 {
		delete(l.conc, ip)
	}
	if l.nconc[node]--; l.nconc[node] <= 0 {
		delete(l.nconc, node)
	}
}

// pruneLocked drops idle buckets and old strikes at most once a minute (008 R5).
func (l *Limiter) pruneLocked(now time.Time) {
	if now.Sub(l.pruned) < time.Minute {
		return
	}
	l.pruned = now
	for _, m := range l.buckets {
		for ip, b := range m {
			if now.Sub(b.last) > 10*time.Minute {
				delete(m, ip)
			}
		}
	}
	for a, m := range l.strikes {
		for ip, h := range m {
			if len(h) == 0 || now.Sub(h[len(h)-1]) > abuseRules[a].Window {
				delete(m, ip)
			}
		}
	}
}

// Size reports tracked IPs per kind (tests and metrics).
func (l *Limiter) Size(k Kind) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets[k])
}
