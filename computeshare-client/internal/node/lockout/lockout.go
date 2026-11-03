// Package lockout refuses a client IP for 10 min after 5 failed auths in 10 min (node 005 R7).
// sshd inside the space only sees the node agent's address, so failures are attributed
// to the real client via the local source port of each node->space connection.
package lockout

import (
	"regexp"
	"strconv"
	"sync"
	"time"
)

type Tracker struct {
	mu       sync.Mutex
	ports    map[int]string // local port -> real src ip
	keyFail  map[int]bool   // ports with a failed publickey and no Accepted yet
	fails    map[string][]time.Time
	until    map[string]time.Time
	Max      int
	Window   time.Duration
	BlockFor time.Duration
	Now      func() time.Time
}

func New() *Tracker {
	return &Tracker{ports: map[int]string{}, keyFail: map[int]bool{}, fails: map[string][]time.Time{}, until: map[string]time.Time{},
		Max: 5, Window: 10 * time.Minute, BlockFor: 10 * time.Minute, Now: time.Now}
}

func (t *Tracker) Map(port int, ip string) { t.mu.Lock(); t.ports[port] = ip; t.mu.Unlock() }

// Unmap is delayed so log lines arriving after disconnect still resolve.
func (t *Tracker) Unmap(port int) {
	time.AfterFunc(30*time.Second, func() { t.mu.Lock(); delete(t.ports, port); delete(t.keyFail, port); t.mu.Unlock() })
}

func (t *Tracker) Blocked(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Now().Before(t.until[ip])
}

// Fail records a failure for ip.
func (t *Tracker) Fail(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.Now()
	f := t.fails[ip][:0]
	for _, x := range t.fails[ip] {
		if now.Sub(x) < t.Window {
			f = append(f, x)
		}
	}
	f = append(f, now)
	t.fails[ip] = f
	if len(f) >= t.Max {
		t.until[ip] = now.Add(t.BlockFor)
		t.fails[ip] = nil
	}
}

// What counts (client 016 R4): every wrong password or unknown user, too many attempts, and a
// connection that tried a key that failed and then closed before any Accepted line. Agents
// often offer several keys before the right one, so "Failed publickey" alone never counts.
var (
	pwFailRe  = regexp.MustCompile(`(?:Failed (?:password|keyboard-interactive/pam) for (?:invalid user )?\S+|Invalid user \S+) from \S+ port (\d+)`)
	keyFailRe = regexp.MustCompile(`Failed publickey for (?:invalid user )?\S+ from \S+ port (\d+)`)
	maxAuthRe = regexp.MustCompile(`maximum authentication attempts exceeded for (?:invalid user )?\S+ from \S+ port (\d+)`)
	acceptRe  = regexp.MustCompile(`Accepted \S+ for \S+ from \S+ port (\d+)`)
	closeRe   = regexp.MustCompile(`(?:Connection closed by|Disconnected from)(?: (?:authenticating|invalid) user \S+)? \S+ port (\d+)`)
)

func port(re *regexp.Regexp, l string) int {
	m := re.FindStringSubmatch(l)
	if m == nil {
		return 0
	}
	p, _ := strconv.Atoi(m[1])
	return p
}

// Line feeds one sshd log line; returns the attributed ip if it counted as a failure.
func (t *Tracker) Line(l string) string {
	count := 0
	t.mu.Lock()
	switch {
	case port(pwFailRe, l) != 0:
		count = port(pwFailRe, l)
	case port(maxAuthRe, l) != 0:
		count = port(maxAuthRe, l)
		delete(t.keyFail, count)
	case port(keyFailRe, l) != 0:
		t.keyFail[port(keyFailRe, l)] = true
	case port(acceptRe, l) != 0:
		delete(t.keyFail, port(acceptRe, l))
	case port(closeRe, l) != 0:
		p := port(closeRe, l)
		if t.keyFail[p] {
			count = p
		}
		delete(t.keyFail, p)
	}
	ip := t.ports[count]
	t.mu.Unlock()
	if count == 0 || ip == "" {
		return ""
	}
	t.Fail(ip)
	return ip
}
