package setup

import (
	"net"
	"os"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/node/incus"
)

// DNSEnv overrides the upstream DNS servers of csbr0's dnsmasq (comma/space separated IPs).
// Example: CSNODE_DNS="1.1.1.1,9.9.9.9". Use it when the default choice does not work on your network.
const DNSEnv = "CSNODE_DNS"

// dnsMarker tags raw.dnsmasq text written by csnode, so a rerun can replace it and never touches
// text the owner wrote.
const dnsMarker = "# csnode-managed"

// FallbackDNS is used when the host has no resolver dnsmasq can reach.
var FallbackDNS = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}

// ResolvConfFiles are read in order; the systemd-resolved file holds the real upstreams
// (/etc/resolv.conf there only points at the 127.0.0.53 stub).
var ResolvConfFiles = []string{"/run/systemd/resolve/resolv.conf", "/etc/resolv.conf"}

// parseNameservers returns the "nameserver" IPs of a resolv.conf text.
func parseNameservers(conf string) []string {
	var out []string
	for _, l := range strings.Split(conf, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "nameserver" {
			if i := strings.IndexByte(f[1], '%'); i >= 0 { // zone id
				f[1] = f[1][:i]
			}
			out = append(out, f[1])
		}
	}
	return out
}

// usableUpstream says whether dnsmasq (a host process on the bridge) can use ip as upstream.
// Loopback (127.x, stubs) and the WSL2 DNS-tunnel address 10.255.255.254 only answer to
// processes bound inside the host namespace; unspecified/link-local/multicast/IPv6 are skipped too.
func usableUpstream(s, gw string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil || ip.To4() == nil { // IPv4 only: csbr0 has ipv6.address=none
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
		return false
	}
	if ip.Equal(net.IPv4(10, 255, 255, 254)) || s == gw {
		return false
	}
	return true
}

// PickDNSUpstreams decides dnsmasq's upstream servers.
//   - override (CSNODE_DNS) wins and is used as given (invalid entries dropped);
//   - else the usable nameservers of the resolv.conf texts (first file with any wins), reported as hostOK=true
//     (dnsmasq already finds them by itself, no raw.dnsmasq is needed);
//   - else the public fallbacks, hostOK=false.
func PickDNSUpstreams(override string, gw string, confs ...string) (servers []string, hostOK bool) {
	if f := strings.FieldsFunc(override, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }); len(f) > 0 {
		for _, s := range f {
			if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
				servers = append(servers, s)
			}
		}
		if len(servers) > 0 {
			return servers, false
		}
	}
	for _, c := range confs {
		for _, s := range parseNameservers(c) {
			if usableUpstream(s, gw) {
				servers = append(servers, s)
			}
		}
		if len(servers) > 0 {
			return servers, true
		}
	}
	return append([]string{}, FallbackDNS...), false
}

// DNSRaw builds the raw.dnsmasq value; "" means "no managed config".
func DNSRaw(servers []string) string {
	var b strings.Builder
	b.WriteString(dnsMarker + "\n")
	for _, s := range servers {
		b.WriteString("server=" + s + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// EnsureDNS sets explicit dnsmasq upstreams on csbr0 when the host resolver is unusable
// (WSL2 tunnel, 127.0.0.53 stub, ...) or the owner set CSNODE_DNS. Idempotent: it writes only on change
// and never overwrites a raw.dnsmasq that csnode did not write.
//
// It does not touch csacl: dnsmasq runs on the host and asks upstream itself, the space only talks
// to the bridge address (port 53 to the gateway is handled by Incus), so no LAN rule is opened.
func EnsureDNS(gw string, out func(string)) error {
	var confs []string
	for _, f := range ResolvConfFiles {
		confs = append(confs, readFile(f))
	}
	servers, hostOK := PickDNSUpstreams(os.Getenv(DNSEnv), gw, confs...)
	cur, _ := incus.Run(nil, "network", "get", Bridge, "raw.dnsmasq")
	cur = strings.TrimSpace(cur)
	managed := cur == "" || strings.HasPrefix(cur, dnsMarker)
	if !managed {
		return nil // the owner's own config
	}
	want := ""
	if !hostOK {
		want = DNSRaw(servers)
	}
	if cur == want {
		return nil
	}
	if want == "" {
		out("host DNS is usable; removing csnode's DNS override on " + Bridge)
		_, err := incus.Run(nil, "network", "unset", Bridge, "raw.dnsmasq")
		return err
	}
	out("host DNS is not reachable from dnsmasq; using upstream DNS " + strings.Join(servers, ", "))
	_, err := incus.Run(nil, "network", "set", Bridge, "raw.dnsmasq="+want)
	return err
}
