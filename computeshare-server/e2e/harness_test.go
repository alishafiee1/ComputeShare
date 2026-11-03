package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"github.com/alishafiee1/computeshare-server/internal/broker/server"
	"github.com/alishafiee1/computeshare-server/internal/broker/store"
	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

// lab is one in-process broker plus fake nodes. Fake nodes implement the node side of
// protocol §1–§5 (the real node lives in computeshare-client) and pipe streams to Target.
type lab struct {
	dir     string
	St      *store.Store
	Srv     *server.Server
	cancel  context.CancelFunc
	nodes   map[string]*fakeNode
	last    *proto.RegisterResponse
	lastMsg string
	out     string
	code    int
	token   string
}

func newLab(limitsConn int, mod ...func(*server.Config, *store.Store)) (*lab, error) {
	dir, _ := os.MkdirTemp("", "csbroker-e2e")
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		return nil, err
	}
	cfg := server.Config{HTTPAddr: "127.0.0.1:0", SSHAddr: "127.0.0.1:0", StateDir: dir,
		OfflineAfter: 6 * time.Second}
	if limitsConn > 0 {
		cfg.Limits.ConnPerMin, cfg.Limits.ConcurrentPerIP, cfg.Limits.ConcurrentPerNode, cfg.Limits.BanFor = limitsConn, 20, 50, 10*time.Minute
	}
	for _, m := range mod {
		m(&cfg, st)
	}
	srv, err := server.New(cfg, st, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Run(ctx)
	select {
	case <-srv.Ready():
	case <-time.After(5 * time.Second):
		cancel()
		return nil, fmt.Errorf("broker did not start")
	}
	return &lab{dir: dir, St: st, Srv: srv, cancel: cancel, nodes: map[string]*fakeNode{}}, nil
}

func (l *lab) close() {
	for _, n := range l.nodes {
		n.stop()
	}
	l.cancel()
	time.Sleep(100 * time.Millisecond)
	l.St.Close()
	os.RemoveAll(l.dir)
}

func (l *lab) url() string { return "http://" + l.Srv.HTTPBound }
func (l *lab) sshPort() string {
	_, p, _ := net.SplitHostPort(l.Srv.SSHBound)
	return p
}

type fakeNode struct {
	Name, ID, Target string
	priv             ed25519.PrivateKey
	hostKey          string
	ctx              context.Context
	cancel           context.CancelFunc
	cut              atomic.Bool
	online           atomic.Bool
	dialed           sync.Map // addr -> true
	proxy            net.Listener
	lastWelcome      atomic.Value
	// tap, if set, sees every byte between the node and the broker (plaintext check).
	tap io.Writer
}

const labHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGm1+4CH6JCGeM0fBxxizeBxZk0R8hX8mKD4dYNr8w7r"

func (l *lab) register(name, token string) (*fakeNode, *proto.RegisterResponse, error) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	hk := labHostKey
	if v := os.Getenv("CS_LAB_HOSTKEY"); v != "" {
		hk = v
	}
	r := proto.RegisterRequest{Proto: proto.Version, Name: name, Token: token, DevicePubkey: proto.EncodePubkey(priv.Public().(ed25519.PublicKey)),
		HostKey: hk, RegisteredAt: time.Now().Unix(), OS: "linux", Arch: "amd64", Version: "test"}
	r.HostKeySig = proto.Sign(priv, proto.RegistrationBytes(name, hk, r.RegisteredAt))
	b, _ := json.Marshal(r)
	resp, err := http.Post(l.url()+"/v1/register", "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	var rr proto.RegisterResponse
	json.NewDecoder(resp.Body).Decode(&rr)
	l.last = &rr
	l.lastMsg = rr.Message
	if rr.Code != proto.CodeRegistered {
		return nil, &rr, nil
	}
	n := &fakeNode{Name: name, ID: rr.NodeID, priv: priv, hostKey: hk, Target: os.Getenv("CS_LAB_SPACE")}
	l.nodes[strings.ToLower(name)] = n
	return n, &rr, nil
}

// start runs the node tunnel through a cuttable TCP proxy, reconnecting every 1 s.
func (n *fakeNode) start(brokerHTTP string) {
	n.ctx, n.cancel = context.WithCancel(context.Background())
	pl, _ := net.Listen("tcp", "127.0.0.1:0")
	n.proxy = pl
	go func() {
		for {
			c, err := pl.Accept()
			if err != nil {
				return
			}
			go func() {
				if n.cut.Load() {
					c.Close()
					return
				}
				u, err := net.Dial("tcp", brokerHTTP)
				if err != nil {
					c.Close()
					return
				}
				go func() {
					for !n.cut.Load() && n.ctx.Err() == nil {
						time.Sleep(100 * time.Millisecond)
					}
					c.Close()
					u.Close()
				}()
				if n.tap != nil {
					go io.Copy(io.MultiWriter(u, n.tap), c)
					io.Copy(io.MultiWriter(c, n.tap), u)
				} else {
					go io.Copy(u, c)
					io.Copy(c, u)
				}
			}()
		}
	}()
	go func() {
		for n.ctx.Err() == nil {
			n.once()
			n.online.Store(false)
			select {
			case <-n.ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}()
}

func (n *fakeNode) stop() {
	if n.cancel != nil {
		n.cancel()
		n.proxy.Close()
	}
}

func (n *fakeNode) once() {
	ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	ws, _, err := websocket.Dial(ctx, "ws://"+n.proxy.Addr().String()+"/v1/tunnel", nil)
	cancel()
	if err != nil {
		return
	}
	ts := time.Now().Unix()
	b, _ := json.Marshal(proto.Hello{Proto: proto.Version, Type: proto.TypeHello, NodeID: n.ID, TS: ts, Sig: proto.Sign(n.priv, proto.HelloBytes(n.ID, ts))})
	ws.Write(n.ctx, websocket.MessageText, b)
	_, msg, err := ws.Read(n.ctx)
	n.lastWelcome.Store(string(msg))
	if err != nil || !strings.Contains(string(msg), proto.TypeWelcome) {
		ws.CloseNow()
		return
	}
	nc := websocket.NetConn(n.ctx, ws, websocket.MessageBinary)
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	sess, err := yamux.Client(nc, cfg)
	if err != nil {
		return
	}
	defer sess.Close()
	ctrl, err := sess.OpenStream()
	if err != nil {
		return
	}
	n.online.Store(true)
	go func() {
		for {
			ping, _ := json.Marshal(proto.Ping{Type: proto.TypePing, Stats: proto.Stats{CPUPct: 23, RAMPct: 40}})
			if _, err := ctrl.Write(append(ping, '\n')); err != nil {
				return
			}
			select {
			case <-sess.CloseChan():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	go io.Copy(io.Discard, ctrl)
	for {
		st, err := sess.AcceptStream()
		if err != nil {
			return
		}
		go func() {
			defer st.Close()
			r := bufio.NewReader(st)
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var so proto.StreamOpen
			json.Unmarshal(line, &so)
			if n.Target == "" {
				return
			}
			n.dialed.Store(n.Target, true)
			sc, err := net.Dial("tcp", n.Target)
			if err != nil {
				return
			}
			defer sc.Close()
			go func() { io.Copy(sc, r); sc.(*net.TCPConn).CloseWrite() }()
			io.Copy(st, sc)
		}()
	}
}

func waitFor(d time.Duration, f func() bool) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if f() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
