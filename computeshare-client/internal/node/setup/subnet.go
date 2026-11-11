package setup

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// routeNets parses the IPv4 destinations (not the default route) from /proc/net/route.
func routeNets(route string) []*net.IPNet {
	var out []*net.IPNet
	lines := strings.Split(route, "\n")
	for _, l := range lines[min(1, len(lines)):] {
		f := strings.Fields(l)
		if len(f) < 8 {
			continue
		}
		d, e1 := strconv.ParseUint(f[1], 16, 32)
		m, e2 := strconv.ParseUint(f[7], 16, 32)
		if e1 != nil || e2 != nil || m == 0 {
			continue
		}
		ip := net.IPv4(byte(d), byte(d>>8), byte(d>>16), byte(d>>24))
		out = append(out, &net.IPNet{IP: ip.Mask(net.IPv4Mask(byte(m), byte(m>>8), byte(m>>16), byte(m>>24))),
			Mask: net.IPv4Mask(byte(m), byte(m>>8), byte(m>>16), byte(m>>24))})
	}
	return out
}

// usedNets is every IPv4 network the host already has: interface addresses and routes.
// On WSL2 with mirrored networking these include all of Windows' networks.
func usedNets() []*net.IPNet {
	var out []*net.IPNet
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			out = append(out, &net.IPNet{IP: n.IP.Mask(n.Mask), Mask: n.Mask})
		}
	}
	return append(out, routeNets(readFile("/proc/net/route"))...)
}

func overlaps(a, b *net.IPNet) bool { return a.Contains(b.IP) || b.Contains(a.IP) }

// PickSubnet returns a free private /24 as "x.y.z.1/24", or "" when none is left.
// prefer is tried first (e.g. "10.77.0.1/24"); the rest comes from 10.x, 172.16-31.x, 192.168.x.
// It never returns a network that overlaps one in used (including /32 host addresses).
func PickSubnet(used []*net.IPNet, prefer ...string) string {
	free := func(c *net.IPNet) bool {
		for _, u := range used {
			if overlaps(c, u) {
				return false
			}
		}
		return true
	}
	try := func(a, b, c int) string {
		n := &net.IPNet{IP: net.IPv4(byte(a), byte(b), byte(c), 0), Mask: net.CIDRMask(24, 32)}
		if free(n) {
			return fmt.Sprintf("%d.%d.%d.1/24", a, b, c)
		}
		return ""
	}
	for _, p := range prefer {
		if _, n, err := net.ParseCIDR(p); err == nil && free(n) {
			return strings.Replace(n.String(), ".0/24", ".1/24", 1)
		}
	}
	for b := 78; b < 256; b++ { // 10.77 is the default, so start next to it
		for c := 0; c < 256; c += 8 {
			if s := try(10, b, c); s != "" {
				return s
			}
		}
	}
	for b := 16; b < 32; b++ {
		for c := 0; c < 256; c += 8 {
			if s := try(172, b, c); s != "" {
				return s
			}
		}
	}
	for c := 100; c < 256; c++ {
		if s := try(192, 168, c); s != "" {
			return s
		}
	}
	return ""
}
