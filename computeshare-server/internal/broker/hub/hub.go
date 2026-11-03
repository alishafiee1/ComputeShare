// Package hub tracks live node tunnels and opens per-connection streams to them.
// The broker only copies bytes; it never parses inner-SSH traffic (broker 002 R6).
package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"github.com/hashicorp/yamux"
)

var (
	ErrOffline = errors.New("offline")
	ErrPaused  = errors.New("node paused")
)

type Conn struct {
	NodeID, Name string
	Sess         *yamux.Session
	Ctrl         net.Conn
	ctrlMu       sync.Mutex
	paused       atomic.Bool
	lastStats    atomic.Pointer[proto.Stats]
	LastPing     atomic.Int64
	streamSeq    atomic.Uint32
	Closed       chan struct{}
	closeOnce    sync.Once
}

func (c *Conn) Paused() bool           { return c.paused.Load() }
func (c *Conn) SetPaused(p bool)       { c.paused.Store(p) }
func (c *Conn) Stats() *proto.Stats    { return c.lastStats.Load() }
func (c *Conn) SetStats(s proto.Stats) { c.lastStats.Store(&s) }

// Send writes one JSON control line.
func (c *Conn) Send(v any) error {
	b, _ := json.Marshal(v)
	c.ctrlMu.Lock()
	defer c.ctrlMu.Unlock()
	c.Ctrl.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.Ctrl.Write(append(b, '\n'))
	return err
}

func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		close(c.Closed)
		c.Sess.Close()
	})
}

type Hub struct {
	mu    sync.RWMutex
	conns map[string]*Conn // by node id
}

func New() *Hub { return &Hub{conns: map[string]*Conn{}} }

func NewConn(id, name string, s *yamux.Session, ctrl net.Conn) *Conn {
	c := &Conn{NodeID: id, Name: name, Sess: s, Ctrl: ctrl, Closed: make(chan struct{})}
	c.LastPing.Store(time.Now().Unix())
	return c
}

// Add registers a live tunnel, closing any previous one of the same node.
func (h *Hub) Add(c *Conn) {
	h.mu.Lock()
	old := h.conns[c.NodeID]
	h.conns[c.NodeID] = c
	h.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

// Remove drops c if it is still the current tunnel; returns true if it was.
func (h *Hub) Remove(c *Conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[c.NodeID] == c {
		delete(h.conns, c.NodeID)
		return true
	}
	return false
}

func (h *Hub) Get(id string) *Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[id]
}

func (h *Hub) All() []*Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Conn, 0, len(h.conns))
	for _, c := range h.conns {
		out = append(out, c)
	}
	return out
}

// Open opens a yamux stream to the node and writes the StreamOpen line (protocol §1, §5).
func (h *Hub) Open(id, via, srcIP string) (net.Conn, error) {
	c := h.Get(id)
	if c == nil {
		return nil, ErrOffline
	}
	if c.Paused() {
		return nil, ErrPaused
	}
	st, err := c.Sess.OpenStream()
	if err != nil {
		return nil, ErrOffline
	}
	so := proto.StreamOpen{StreamID: st.StreamID(), Kind: "ssh", Via: via, SrcIP: srcIP, StartedAt: time.Now().Unix()}
	b, _ := json.Marshal(so)
	if _, err := st.Write(append(b, '\n')); err != nil {
		st.Close()
		return nil, ErrOffline
	}
	return st, nil
}

// Pipe copies both ways and returns bytes up (client->node) and down.
func Pipe(client io.ReadWriteCloser, node io.ReadWriteCloser) (up, down int64) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		down, _ = io.Copy(client, node)
		closeWrite(client)
	}()
	up, _ = io.Copy(node, client)
	closeWrite(node)
	wg.Wait()
	client.Close()
	node.Close()
	return
}

func closeWrite(c io.Closer) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
		return
	}
	c.Close()
}

// Age formats a duration like "12m", "3h", "2d".
func Age(sec int64) string {
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh", sec/3600)
	}
	return fmt.Sprintf("%dd", sec/86400)
}
