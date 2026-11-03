package stream

import (
	"io"
	"net"
	"sync/atomic"
	"time"
)

// Why a session ended (umbrella card 021 R1); written to the "session end" log line.
const (
	EndClosed  = "closed"  // one side closed normally
	EndKick    = "kick"    // the owner kicked it (`csnode kick`, panel, share off)
	EndIdle    = "idle"    // no bytes in either direction for Handler.Idle
	EndStalled = "stalled" // a write was blocked for longer than Handler.Stall
)

// DefaultIdle and DefaultStall apply when the Handler leaves Idle or Stall zero. A negative value turns the limit off.
const (
	DefaultIdle  = 15 * time.Minute
	DefaultStall = 90 * time.Second
)

// sessState is the live bookkeeping of one session: bytes moved, last activity, blocked write, end reason.
type sessState struct {
	up, down   atomic.Int64 // client->space, space->client
	last       atomic.Int64 // unix nano of the last byte
	writeStart atomic.Int64 // unix nano when the write in flight began, 0 if none
	reason     atomic.Value // string
}

func newSessState() *sessState {
	s := &sessState{}
	s.last.Store(time.Now().UnixNano())
	return s
}

// setReason keeps the first reason only, so a kick is not turned into "closed" by the cleanup that follows it.
func (s *sessState) setReason(r string) { s.reason.CompareAndSwap(nil, r) }

func (s *sessState) why() string {
	if r, ok := s.reason.Load().(string); ok {
		return r
	}
	return EndClosed
}

// counted wraps a connection: dir "up" counts bytes read from it as client->space, "down" the opposite.
type counted struct {
	net.Conn
	s    *sessState
	read *atomic.Int64 // counter for bytes read from this side
}

func (c *counted) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.read.Add(int64(n))
		c.s.last.Store(time.Now().UnixNano())
	}
	return n, err
}

func (c *counted) Write(p []byte) (int, error) {
	c.s.writeStart.Store(time.Now().UnixNano())
	n, err := c.Conn.Write(p)
	c.s.writeStart.Store(0)
	if n > 0 {
		c.s.last.Store(time.Now().UnixNano())
	}
	return n, err
}

func (c *counted) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Conn.Close()
}

// watch closes both sides when the session has been idle or a write has been blocked too long.
// It returns when stop is closed.
func watch(s *sessState, idle, stall time.Duration, closeBoth func(), stop <-chan struct{}) {
	if idle <= 0 && stall <= 0 {
		return
	}
	tick := time.Second
	for _, d := range []time.Duration{idle, stall} {
		if d > 0 && d/4 < tick {
			tick = d / 4
		}
	}
	if tick < time.Millisecond {
		tick = time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		now := time.Now().UnixNano()
		if ws := s.writeStart.Load(); stall > 0 && ws != 0 && time.Duration(now-ws) > stall {
			s.setReason(EndStalled)
			closeBoth()
			return
		}
		if idle > 0 && time.Duration(now-s.last.Load()) > idle {
			s.setReason(EndIdle)
			closeBoth()
			return
		}
	}
}

// limit resolves a configured duration: zero means the default, negative means off.
func limit(v, def time.Duration) time.Duration {
	switch {
	case v == 0:
		return def
	case v < 0:
		return 0
	}
	return v
}

var _ io.Closer = (*counted)(nil)
