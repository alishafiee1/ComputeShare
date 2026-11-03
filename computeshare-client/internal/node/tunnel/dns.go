package tunnel

import (
	"context"
	"net"
	"net/http"
	"time"
)

// lookupTimeout is how long one DNS lookup may take before the cached address is tried. WSL's NAT
// resolver can hang for many seconds; a short limit makes the fallback quick (umbrella card 017 R2).
const lookupTimeout = 4 * time.Second

// dialAddr connects to addr ("host:port"). It resolves the host name; if the lookup or every
// dial fails and a last good IP is known, it dials that IP instead. Only the TCP address
// changes: the HTTP transport still runs TLS against the original host name, so certificate
// checking is never bypassed.
func (c *Client) dialAddr(ctx context.Context, network, addr string, direct func(ctx context.Context, network, addr string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) != nil {
		return direct(ctx, network, addr)
	}
	lookup := c.Lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	lctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	ips, lerr := lookup(lctx, host)
	cancel()
	var derr error
	if lerr == nil {
		for _, ip := range ips {
			conn, err := direct(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				c.remember(conn)
				return conn, nil
			}
			derr = err
		}
	}
	if c.LastIP != nil {
		if ip := c.LastIP(); ip != "" {
			conn, err := direct(ctx, network, net.JoinHostPort(ip, port))
			if err == nil {
				c.Log.Warn("name lookup failed; connected to the last good address", "host", host, "ip", ip)
				return conn, nil
			}
			derr = err
		}
	}
	if lerr != nil {
		return nil, lerr
	}
	return nil, derr
}

func (c *Client) remember(conn net.Conn) {
	if c.RememberIP == nil {
		return
	}
	if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok && ta.IP != nil {
		c.RememberIP(ta.IP.String())
	}
}

// httpClient returns the HTTP client for the tunnel dial: the configured one with a transport
// whose TCP dial falls back to the last good IP.
func (c *Client) httpClient() *http.Client {
	base := c.HTTP
	if base == nil {
		base = &http.Client{}
	}
	var t *http.Transport
	if bt, ok := base.Transport.(*http.Transport); ok && bt != nil {
		t = bt.Clone()
	} else {
		t = http.DefaultTransport.(*http.Transport).Clone()
	}
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 20 * time.Second}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return c.dialAddr(ctx, network, addr, d.DialContext)
	}
	hc := *base
	hc.Transport = t
	return &hc
}
