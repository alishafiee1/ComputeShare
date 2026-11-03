// Package blocklist is the owner's list of blocked client addresses (umbrella card 015 R11).
// An entry is one IP or one CIDR range, with an optional end time and a short reason. The agent
// refuses new sessions from a blocked address and closes live ones within seconds.
package blocklist

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one blocked address or range.
type Entry struct {
	Value  string `json:"value"`            // canonical IP or CIDR
	Added  int64  `json:"added"`            // unix time
	Until  int64  `json:"until,omitempty"`  // unix time; 0 = until removed
	Reason string `json:"reason,omitempty"` // short free text, never a secret
}

// Expired reports whether the entry no longer applies at now.
func (e Entry) Expired(now time.Time) bool { return e.Until != 0 && now.Unix() >= e.Until }

// List is the stored list. The zero value is not usable; call Open.
type List struct {
	path string
	mu   sync.Mutex
	Now  func() time.Time
}

// Open uses the file in dir ("blocklist.json"); a missing file is an empty list.
func Open(dir string) *List { return &List{path: filepath.Join(dir, "blocklist.json"), Now: time.Now} }

// Canon turns "1.2.3.4" or "10.0.0.0/8" into its canonical text, or fails.
func Canon(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return "", fmt.Errorf("%q is not a valid address range", s)
		}
		return n.String(), nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return "", fmt.Errorf("%q is not a valid IP address or range", s)
	}
	return ip.String(), nil
}

func (l *List) read() ([]Entry, error) {
	b, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var es []Entry
	if err := json.Unmarshal(b, &es); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", l.path, err)
	}
	return es, nil
}

func (l *List) write(es []Entry) error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(es, "", "  ")
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// live returns the entries that still apply.
func (l *List) live() ([]Entry, error) {
	es, err := l.read()
	if err != nil {
		return nil, err
	}
	now := l.Now()
	out := es[:0:0]
	for _, e := range es {
		if !e.Expired(now) {
			out = append(out, e)
		}
	}
	return out, nil
}

// Add blocks value (an IP or CIDR) for d (0 = until removed). Adding it again replaces the old entry.
func (l *List) Add(value string, d time.Duration, reason string) (Entry, error) {
	v, err := Canon(value)
	if err != nil {
		return Entry{}, err
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	es, err := l.live()
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Value: v, Added: l.Now().Unix(), Reason: strings.TrimSpace(reason)}
	if d > 0 {
		e.Until = l.Now().Add(d).Unix()
	}
	out := es[:0:0]
	for _, x := range es {
		if x.Value != v {
			out = append(out, x)
		}
	}
	return e, l.write(append(out, e))
}

// Remove unblocks value; it reports whether anything was removed.
func (l *List) Remove(value string) (bool, error) {
	v, err := Canon(value)
	if err != nil {
		return false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	es, err := l.live()
	if err != nil {
		return false, err
	}
	out := es[:0:0]
	for _, x := range es {
		if x.Value != v {
			out = append(out, x)
		}
	}
	if len(out) == len(es) {
		return false, nil
	}
	return true, l.write(out)
}

// Entries returns the live entries, oldest first.
func (l *List) Entries() ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	es, err := l.live()
	sort.Slice(es, func(i, j int) bool { return es[i].Added < es[j].Added })
	return es, err
}

// Blocked reports whether ip (text, with or without a port) is covered by a live entry. A damaged
// file blocks nothing: the list must not lock the owner out of their own node.
func (l *List) Blocked(ip string) bool {
	if h, _, err := net.SplitHostPort(ip); err == nil {
		ip = h
	}
	addr := net.ParseIP(strings.Trim(ip, "[]"))
	if addr == nil {
		return false
	}
	es, err := l.Entries()
	if err != nil {
		return false
	}
	for _, e := range es {
		if strings.Contains(e.Value, "/") {
			if _, n, err := net.ParseCIDR(e.Value); err == nil && n.Contains(addr) {
				return true
			}
		} else if net.ParseIP(e.Value).Equal(addr) {
			return true
		}
	}
	return false
}
