// Package stream handles broker-opened streams: it reads StreamOpen, applies the
// owner gates and connects ONLY to the space's sshd (node 006 R4).
package stream

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
)

// DefaultMaxStreams is the node-side cap when config has no max_streams (client 016 R1).
const DefaultMaxStreams = 32

// MaxLine bounds the StreamOpen line.
const MaxLine = 4096

// ReadOpen reads exactly one '\n'-terminated JSON line without over-reading.
func ReadOpen(r io.Reader) (*proto.StreamOpen, error) {
	var buf []byte
	b := make([]byte, 1)
	for len(buf) < MaxLine {
		if _, err := io.ReadFull(r, b); err != nil {
			return nil, err
		}
		if b[0] == '\n' {
			var so proto.StreamOpen
			if err := json.Unmarshal(buf, &so); err != nil {
				return nil, err
			}
			if so.Kind != "ssh" {
				return nil, errors.New("unsupported stream kind")
			}
			return &so, nil
		}
		buf = append(buf, b[0])
	}
	return nil, errors.New("StreamOpen too long")
}

// Gate decides whether a stream may pass. Returning a non-empty reason refuses it
// (the reason is written as one text line before closing).
type Gate func(so *proto.StreamOpen) (ok bool, reason string)

// Session is one live session for `csnode who`.
type Session struct {
	ID        uint32    `json:"id"`
	SrcIP     string    `json:"src_ip"`
	Via       string    `json:"via"`
	Started   time.Time `json:"started"`
	LocalPort int       `json:"local_port"`
	// Live counters shown by `csnode who` (umbrella card 021 R4).
	BytesUp   int64 `json:"bytes_up"`   // client -> space
	BytesDown int64 `json:"bytes_down"` // space -> client
	IdleSecs  int   `json:"idle_secs"`  // seconds since the last byte in either direction
}

type Handler struct {
	// Target returns the space address. It must always be "<space-ip>:22";
	// the only exception is the lab override in config (documented).
	Target func() (string, error)
	Gates  []Gate
	Log    *slog.Logger
	// OnDial maps the local source port of the node->space TCP connection to the
	// real client IP so sshd failures can be attributed (lockout, node 005 R7).
	OnDial func(localPort int, so *proto.StreamOpen)
	OnEnd  func(localPort int)
	// OnChange is called after a session was added to or removed from the live list, so a
	// snapshot for `csnode who` can be written right then (umbrella card 021 R4).
	OnChange func()
	// MaxStreams caps concurrent streams on this node, whatever the broker allows
	// (client 016 R1). Zero means DefaultMaxStreams.
	MaxStreams int
	// Serve, when set, takes over the stream after the gates instead of dialing Target:
	// the per-user gateway terminates SSH on the node (client 015 R1).
	Serve func(c net.Conn, so *proto.StreamOpen)
	// Idle closes a session with no bytes in either direction for this long; Stall closes one whose
	// write is blocked this long. Zero uses DefaultIdle / DefaultStall, negative turns the limit off
	// (umbrella card 021 R3).
	Idle, Stall time.Duration

	mu       sync.Mutex
	active   int
	sessions map[uint32]Session
	conns    map[uint32][2]net.Conn
	states   map[uint32]*sessState
}

// Kick closes one live session immediately (local panel / csnode kick). Returns false if unknown.
func (h *Handler) Kick(id uint32) bool {
	h.mu.Lock()
	c, ok := h.conns[id]
	st := h.states[id]
	h.mu.Unlock()
	if ok {
		if st != nil {
			st.setReason(EndKick)
		}
		c[0].Close()
		c[1].Close()
	}
	return ok
}

// KickIP closes every live session from one client IP; returns how many were closed.
func (h *Handler) KickIP(ip string) int {
	h.mu.Lock()
	var ids []uint32
	for id, s := range h.sessions {
		if s.SrcIP == ip {
			ids = append(ids, id)
		}
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.Kick(id)
	}
	return len(ids)
}

// KickAll closes every live session (share turned off from the panel).
func (h *Handler) KickAll() int {
	h.mu.Lock()
	var ids []uint32
	for id := range h.sessions {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.Kick(id)
	}
	return len(ids)
}

func (h *Handler) Sessions() []Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Session{}
	now := time.Now().UnixNano()
	for id, s := range h.sessions {
		if st := h.states[id]; st != nil {
			s.BytesUp, s.BytesDown = st.up.Load(), st.down.Load()
			if idle := time.Duration(now - st.last.Load()); idle > 0 {
				s.IdleSecs = int(idle / time.Second)
			}
		}
		out = append(out, s)
	}
	return out
}

func (h *Handler) Handle(c net.Conn) {
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(15 * time.Second))
	so, err := ReadOpen(c)
	if err != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	for _, g := range h.Gates {
		if ok, reason := g(so); !ok {
			if reason != "" {
				c.Write([]byte(reason + "\r\n"))
			}
			h.Log.Info("stream refused", "src_ip", so.SrcIP, "via", so.Via, "reason", reason)
			return
		}
	}
	max := h.MaxStreams
	if max <= 0 {
		max = DefaultMaxStreams
	}
	h.mu.Lock()
	busy := h.active >= max
	if !busy {
		h.active++
	}
	h.mu.Unlock()
	if busy {
		c.Write([]byte("node busy\r\n"))
		h.Log.Info("stream refused", "src_ip", so.SrcIP, "via", so.Via, "reason", "node busy")
		return
	}
	defer func() { h.mu.Lock(); h.active--; h.mu.Unlock() }()
	idle, stall := limit(h.Idle, DefaultIdle), limit(h.Stall, DefaultStall)
	if h.Serve != nil {
		st := h.track(so, c, c, 0)
		h.changed()
		h.Log.Info("session start", "src_ip", so.SrcIP, "via", so.Via, "mode", "gateway")
		cc := &counted{Conn: c, s: st, read: &st.up}
		stop := make(chan struct{})
		go watch(st, idle, stall, func() { c.Close() }, stop)
		h.Serve(cc, so)
		close(stop)
		h.untrack(so)
		h.changed()
		h.Log.Info("session end", "src_ip", so.SrcIP, "via", so.Via, "reason", st.why(),
			"bytes_up", st.up.Load(), "bytes_down", st.down.Load())
		return
	}
	addr, err := h.Target()
	if err != nil {
		c.Write([]byte("space unavailable\r\n"))
		return
	}
	sc, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		c.Write([]byte("space unavailable\r\n"))
		return
	}
	defer sc.Close()
	lp := 0
	if ta, ok := sc.LocalAddr().(*net.TCPAddr); ok {
		lp = ta.Port
	}
	if h.OnDial != nil {
		h.OnDial(lp, so)
	}
	st := h.track(so, c, sc, lp)
	h.changed()
	h.Log.Info("session start", "src_ip", so.SrcIP, "via", so.Via, "space_port", strconv.Itoa(lp))
	// bytes read from the tunnel stream go up to the space; bytes read from the space go down to the client
	wc := &counted{Conn: c, s: st, read: &st.up}
	ws := &counted{Conn: sc, s: st, read: &st.down}
	stop := make(chan struct{})
	go watch(st, idle, stall, func() { c.Close(); sc.Close() }, stop)
	done := make(chan struct{})
	go func() { io.Copy(ws, wc); ws.CloseWrite(); close(done) }()
	io.Copy(wc, ws)
	c.Close()
	<-done
	close(stop)
	h.untrack(so)
	h.changed()
	if h.OnEnd != nil {
		h.OnEnd(lp)
	}
	h.Log.Info("session end", "src_ip", so.SrcIP, "via", so.Via, "reason", st.why(),
		"bytes_up", st.up.Load(), "bytes_down", st.down.Load())
}

func (h *Handler) changed() {
	if h.OnChange != nil {
		h.OnChange()
	}
}

func (h *Handler) track(so *proto.StreamOpen, a, b net.Conn, lp int) *sessState {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions == nil {
		h.sessions = map[uint32]Session{}
		h.conns = map[uint32][2]net.Conn{}
		h.states = map[uint32]*sessState{}
	}
	st := newSessState()
	h.sessions[so.StreamID] = Session{ID: so.StreamID, SrcIP: so.SrcIP, Via: so.Via, Started: time.Now(), LocalPort: lp}
	h.conns[so.StreamID] = [2]net.Conn{a, b}
	h.states[so.StreamID] = st
	return st
}

func (h *Handler) untrack(so *proto.StreamOpen) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, so.StreamID)
	delete(h.conns, so.StreamID)
	delete(h.states, so.StreamID)
}
