// Package tunnel is the node side of the WSS+yamux tunnel (protocol §1, node 006 R3).
package tunnel

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

// ErrFatal stops reconnecting (banned / unknown node).
var ErrFatal = errors.New("fatal")

type Client struct {
	Server    string
	NodeID    string
	Key       ed25519.PrivateKey
	HTTP      *http.Client
	PingEvery time.Duration
	MinBack   time.Duration
	MaxBack   time.Duration
	Stats     func() proto.Stats
	Paused    func() bool
	OnStream  func(net.Conn)
	Log       *slog.Logger
	// LastIP and RememberIP keep the last broker address that worked, so a failed DNS lookup does not
	// stop a reconnect (umbrella card 017 R2). Both are optional.
	LastIP     func() string
	RememberIP func(ip string)
	// Lookup replaces the DNS resolver (tests); nil uses the system resolver.
	Lookup func(ctx context.Context, host string) ([]net.IPAddr, error)

	mu     sync.Mutex
	ctrl   net.Conn
	Online func(bool)
}

func (c *Client) defaults() {
	if c.PingEvery == 0 {
		c.PingEvery = 15 * time.Second
	}
	if c.MinBack == 0 {
		c.MinBack = time.Second
	}
	if c.MaxBack == 0 {
		c.MaxBack = 30 * time.Second
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
}

// Backoff returns the delay for attempt n (0-based): exponential from min up to max (the node uses 1 s..30 s) with ±25% jitter.
func Backoff(n int, min, max time.Duration) time.Duration {
	d := min
	for i := 0; i < n && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	j := time.Duration(rand.Int64N(int64(d)/2+1)) - d/4
	return d + j
}

func wsURL(server string) string {
	s := strings.TrimRight(server, "/")
	s = strings.Replace(s, "https://", "wss://", 1)
	s = strings.Replace(s, "http://", "ws://", 1)
	return s + "/v1/tunnel"
}

// Run keeps the tunnel up until ctx ends or a fatal error occurs.
func (c *Client) Run(ctx context.Context) error {
	c.defaults()
	attempt := 0
	for {
		start := time.Now()
		err := c.once(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, ErrFatal) {
			return err
		}
		if time.Since(start) > 30*time.Second {
			attempt = 0
		}
		d := Backoff(attempt, c.MinBack, c.MaxBack)
		attempt++
		c.Log.Warn("tunnel down, reconnecting", "err", fmt.Sprint(err), "in", d.Round(time.Millisecond).String())
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(d):
		}
	}
}

func (c *Client) once(ctx context.Context) error {
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(dctx, wsURL(c.Server), &websocket.DialOptions{HTTPClient: c.httpClient()})
	if err != nil {
		return err
	}
	ws.SetReadLimit(1 << 20)
	ts := time.Now().Unix()
	h := proto.Hello{Proto: proto.Version, Type: proto.TypeHello, NodeID: c.NodeID, TS: ts, Sig: proto.Sign(c.Key, proto.HelloBytes(c.NodeID, ts))}
	b, _ := json.Marshal(h)
	if err := ws.Write(dctx, websocket.MessageText, b); err != nil {
		return err
	}
	_, msg, err := ws.Read(dctx)
	if err != nil {
		return err
	}
	var w proto.Welcome
	json.Unmarshal(msg, &w)
	if w.Type != proto.TypeWelcome {
		switch w.Code {
		case proto.CodeClockSkew:
			return errors.New("clock skew too large; fix system time")
		case proto.CodeNodeBanned, "NODE_UNKNOWN":
			return fmt.Errorf("%w: %s", ErrFatal, w.Code)
		}
		return fmt.Errorf("broker refused: %s %s", w.Code, w.Message)
	}
	nc := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
	ycfg := yamux.DefaultConfig()
	ycfg.LogOutput = nil
	ycfg.Logger = slog.NewLogLogger(c.Log.Handler(), slog.LevelDebug)
	ycfg.KeepAliveInterval = 20 * time.Second
	ycfg.ConnectionWriteTimeout = 10 * time.Second
	sess, err := yamux.Client(nc, ycfg)
	if err != nil {
		return err
	}
	defer sess.Close()
	ctrl, err := sess.OpenStream() // first stream = control (stream "0" of protocol §1)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.ctrl = ctrl
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.ctrl = nil; c.mu.Unlock() }()
	c.Log.Info("tunnel online", "name", w.Name)
	if c.Online != nil {
		c.Online(true)
		defer c.Online(false)
	}
	errc := make(chan error, 4)
	var lastPong atomic.Int64
	lastPong.Store(time.Now().UnixNano())
	// watchdog: no pong for 3 ping intervals (min 10s) = dead tunnel (half-open TCP, suspended host)
	go func() {
		lim := 3 * c.PingEvery
		if lim < 10*time.Second {
			lim = 10 * time.Second
		}
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if time.Since(time.Unix(0, lastPong.Load())) > lim {
					errc <- errors.New("no pong from broker")
					return
				}
			case <-sess.CloseChan():
				return
			}
		}
	}()
	// control reader
	go func() {
		sc := bufio.NewScanner(ctrl)
		for sc.Scan() {
			lastPong.Store(time.Now().UnixNano())
			t, _ := proto.PeekType(sc.Bytes())
			if t == proto.TypeBan {
				var bn proto.Ban
				json.Unmarshal(sc.Bytes(), &bn)
				c.Log.Error("banned by broker", "reason", bn.Reason)
				errc <- fmt.Errorf("%w: banned: %s", ErrFatal, bn.Reason)
				return
			}
		}
		errc <- errors.New("control stream closed")
	}()
	// pinger + status
	go func() {
		lastPaused := false
		send := func() error {
			if c.Paused != nil {
				p := c.Paused()
				if p != lastPaused {
					st := "online"
					if p {
						st = "paused"
					}
					if err := c.Send(proto.Status{Type: proto.TypeStatus, Status: st}); err != nil {
						return err
					}
					lastPaused = p
				}
			}
			var st proto.Stats
			if c.Stats != nil {
				st = c.Stats()
			}
			return c.Send(proto.Ping{Type: proto.TypePing, Stats: st})
		}
		if c.Paused != nil && c.Paused() {
			lastPaused = false
		}
		if err := send(); err != nil {
			errc <- err
			return
		}
		t := time.NewTicker(c.PingEvery)
		pt := time.NewTicker(time.Second)
		defer t.Stop()
		defer pt.Stop()
		for {
			select {
			case <-t.C:
				if err := send(); err != nil {
					errc <- err
					return
				}
			case <-pt.C: // status changes propagate within 1 s
				if c.Paused != nil && c.Paused() != lastPaused {
					st := map[bool]string{true: "paused", false: "online"}[c.Paused()]
					if err := c.Send(proto.Status{Type: proto.TypeStatus, Status: st}); err != nil {
						errc <- err
						return
					}
					lastPaused = c.Paused()
				}
			case <-sess.CloseChan():
				return
			}
		}
	}()
	// data streams
	go func() {
		for {
			st, err := sess.AcceptStream()
			if err != nil {
				errc <- err
				return
			}
			go c.OnStream(st)
		}
	}()
	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		return err
	}
}

// Send writes a JSON control line on the current tunnel.
func (c *Client) Send(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctrl == nil {
		return errors.New("not connected")
	}
	b, _ := json.Marshal(v)
	c.ctrl.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.ctrl.Write(append(b, '\n'))
	return err
}

// Leave sends a signed Leave (R5) and waits briefly for the ack.
func (c *Client) Leave() error {
	ts := time.Now().Unix()
	return c.Send(proto.Leave{Type: proto.TypeLeave, TS: ts, Sig: proto.Sign(c.Key, proto.LeaveBytes(c.NodeID, ts))})
}
