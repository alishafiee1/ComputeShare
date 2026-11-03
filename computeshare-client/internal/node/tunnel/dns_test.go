package tunnel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
)

func directDial(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func listen(t *testing.T) (net.Listener, string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return l, port
}

// umbrella card 017 R2: when DNS fails, the node dials the last good IP.
func TestDialFallsBackToLastIP(t *testing.T) {
	l, port := listen(t)
	defer l.Close()
	c := &Client{Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Lookup: func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("i/o timeout") },
		LastIP: func() string { return "127.0.0.1" }}
	conn, err := c.dialAddr(context.Background(), "tcp", "broker.example:"+port, directDial)
	if err != nil {
		t.Fatal("no fallback:", err)
	}
	conn.Close()
}

// without a cached address the lookup error is returned unchanged.
func TestDialNoCacheKeepsLookupError(t *testing.T) {
	want := errors.New("i/o timeout")
	c := &Client{Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Lookup: func(context.Context, string) ([]net.IPAddr, error) { return nil, want }}
	if _, err := c.dialAddr(context.Background(), "tcp", "broker.example:443", directDial); !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

// a successful dial remembers the address; the next failed lookup uses it.
func TestDialRemembersAndReuses(t *testing.T) {
	l, port := listen(t)
	defer l.Close()
	var mu sync.Mutex
	saved := ""
	fail := false
	c := &Client{Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Lookup: func(context.Context, string) ([]net.IPAddr, error) {
			if fail {
				return nil, errors.New("i/o timeout")
			}
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		},
		RememberIP: func(ip string) { mu.Lock(); saved = ip; mu.Unlock() },
		LastIP:     func() string { mu.Lock(); defer mu.Unlock(); return saved }}
	conn, err := c.dialAddr(context.Background(), "tcp", "broker.example:"+port, directDial)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if saved != "127.0.0.1" {
		t.Fatalf("saved %q", saved)
	}
	fail = true
	conn, err = c.dialAddr(context.Background(), "tcp", "broker.example:"+port, directDial)
	if err != nil {
		t.Fatal("did not reuse the remembered address:", err)
	}
	conn.Close()
}

// an IP literal is dialed as is, with no lookup and no cache.
func TestDialIPLiteral(t *testing.T) {
	l, port := listen(t)
	defer l.Close()
	c := &Client{Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Lookup: func(context.Context, string) ([]net.IPAddr, error) { t.Fatal("lookup called"); return nil, nil }}
	conn, err := c.dialAddr(context.Background(), "tcp", "127.0.0.1:"+port, directDial)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}
