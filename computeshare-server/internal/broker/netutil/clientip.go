// Package netutil resolves the real client IP behind trusted reverse proxies (server 002).
package netutil

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Trusted is a set of proxy networks whose X-Forwarded-For / X-Real-IP headers are believed.
type Trusted []*net.IPNet

// ParseTrusted parses a comma/space separated list of IPs or CIDRs ("127.0.0.1, 172.18.0.0/16").
// An empty string means no proxy is trusted: headers are ignored and RemoteAddr is used.
func ParseTrusted(v string) (Trusted, error) {
	var out Trusted
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if !strings.Contains(f, "/") {
			ip := net.ParseIP(f)
			if ip == nil {
				return nil, fmt.Errorf("trusted_proxies: invalid address %q", f)
			}
			if ip.To4() != nil {
				f += "/32"
			} else {
				f += "/128"
			}
		}
		_, n, err := net.ParseCIDR(f)
		if err != nil {
			return nil, fmt.Errorf("trusted_proxies: invalid network %q", f)
		}
		out = append(out, n)
	}
	return out, nil
}

func (t Trusted) contains(ip net.IP) bool {
	for _, n := range t {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func hostIP(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// ClientIP returns the client address used for rate limiting and logs.
// Only when the direct peer is a trusted proxy are forwarding headers used; X-Forwarded-For is
// walked from the right and the first address that is not itself a trusted proxy wins, so a
// client cannot spoof its address by sending its own X-Forwarded-For header.
func (t Trusted) ClientIP(r *http.Request) string {
	peer := hostIP(r.RemoteAddr)
	pip := net.ParseIP(peer)
	if pip == nil || len(t) == 0 || !t.contains(pip) {
		return peer
	}
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		for _, p := range strings.Split(h, ",") {
			if p = strings.TrimSpace(p); p != "" {
				hops = append(hops, p)
			}
		}
	}
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(hostIP(hops[i]))
		if ip == nil {
			return peer // malformed chain: do not trust it
		}
		if !t.contains(ip) {
			return ip.String()
		}
	}
	if xr := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); xr != nil {
		return xr.String()
	}
	return peer
}
