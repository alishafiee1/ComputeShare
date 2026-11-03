// Package server wires the broker: HTTP API, WSS tunnel, SSH jump (single port), admin panel.
// It executes no commands and spawns no processes (broker 001 R1).
package server

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"github.com/alishafiee1/computeshare-server/internal/broker/hub"
	"github.com/alishafiee1/computeshare-server/internal/broker/limits"
	"github.com/alishafiee1/computeshare-server/internal/broker/netutil"
	"github.com/alishafiee1/computeshare-server/internal/broker/store"
	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"golang.org/x/crypto/ssh"
)

type Config struct {
	HTTPAddr     string // e.g. ":443"
	TLSCert      string // empty = plain HTTP (behind a TLS proxy or lab)
	TLSKey       string
	SSHAddr      string        // e.g. ":2222" (the only SSH port; no per-node ports)
	StateDir     string        // SQLite + ssh host key
	PingEvery    time.Duration // advertised to nodes, default 15s
	OfflineAfter time.Duration // default 45s
	Version      string        // build version, served by GET /v1/version (server 022 R1)
	// SessionIdle closes a jump session with no bytes in either direction for this long (default 15 min);
	// StallAfter closes one whose write has been blocked this long (default 90 s). Negative turns the limit off.
	// Umbrella card 021 R3.
	SessionIdle time.Duration
	StallAfter  time.Duration
	Limits      limits.Config
	// TrustedProxies whose X-Forwarded-For is believed (nginx in front of the HTTP API).
	TrustedProxies netutil.Trusted
}

func (c *Config) defaults() {
	if c.PingEvery == 0 {
		c.PingEvery = 15 * time.Second
	}
	if c.OfflineAfter == 0 {
		c.OfflineAfter = 45 * time.Second
	}
	if c.SessionIdle == 0 {
		c.SessionIdle = 15 * time.Minute
	}
	if c.StallAfter == 0 {
		c.StallAfter = 90 * time.Second
	}

}

type Server struct {
	cfg    Config
	St     *store.Store
	Hub    *hub.Hub
	Lim    *limits.Limiter
	Log    *slog.Logger
	sshCfg *ssh.ServerConfig
	ctx    context.Context
	// Actual listen addresses (useful with ":0" in tests).
	HTTPBound, SSHBound string
	ready               chan struct{}
	panel               atomic.Value // http.Handler of the admin panel (server 003)
}

// Ready is closed once listeners are bound.
func (s *Server) Ready() <-chan struct{} { return s.ready }

func New(cfg Config, st *store.Store, log *slog.Logger) (*Server, error) {
	cfg.defaults()
	s := &Server{cfg: cfg, St: st, Hub: hub.New(), Log: log, ready: make(chan struct{})}
	s.Lim = limits.New(cfg.Limits, st)
	signer, err := loadHostKey(filepath.Join(cfg.StateDir, "ssh_host_ed25519_key"))
	if err != nil {
		return nil, err
	}
	s.sshCfg = &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-csbroker",
		NoClientAuth:  true,
		NoClientAuthCallback: func(m ssh.ConnMetadata) (*ssh.Permissions, error) {
			if m.User() == "join" || m.User() == "list" {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unknown user")
		},
		MaxAuthTries: 3,
	}
	s.sshCfg.AddHostKey(signer)
	return s, nil
}

func loadHostKey(path string) (ssh.Signer, error) {
	if b, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(b)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	blk, err := ssh.MarshalPrivateKey(priv, "csbroker")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(blk), 0o600); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

// Run starts all listeners and blocks until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	s.ctx = ctx
	// Nodes found online in DB are stale after restart.
	nodes, _ := s.St.Nodes()
	for _, n := range nodes {
		if n.Status == "online" || n.Status == "paused" {
			s.St.SetStatus(n.ID, "offline")
		}
	}
	hl, err := net.Listen("tcp", s.cfg.HTTPAddr)
	if err != nil {
		return err
	}
	sl, err := net.Listen("tcp", s.cfg.SSHAddr)
	if err != nil {
		return err
	}
	s.HTTPBound, s.SSHBound = hl.Addr().String(), sl.Addr().String()
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if s.cfg.TLSCert != "" {
			srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13}
			srv.ServeTLS(hl, s.cfg.TLSCert, s.cfg.TLSKey)
		} else {
			srv.Serve(hl)
		}
	}()
	go s.serveSSH(sl)
	go s.housekeeping(ctx)
	close(s.ready)
	s.Log.Info("csbroker started", "http", hl.Addr().String(), "ssh", sl.Addr().String(), "tls", s.cfg.TLSCert != "")
	<-ctx.Done()
	srv.Close()
	sl.Close()
	for _, c := range s.Hub.All() {
		c.Close()
	}
	return nil
}

func (s *Server) housekeeping(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now().Unix()
		for _, c := range s.Hub.All() {
			if time.Duration(now-c.LastPing.Load())*time.Second > s.cfg.OfflineAfter {
				s.Log.Info("node heartbeat timeout", "node", c.Name)
				c.Close()
				continue
			}
			nd, err := s.St.NodeByID(c.NodeID)
			if errors.Is(err, store.ErrNotFound) {
				c.Close() // released by admin
			} else if err == nil && (nd.Status == "banned" || s.St.IsBanned("node", proto.NameKey(nd.Name))) {
				c.Send(proto.Ban{Type: proto.TypeBan, Reason: "banned by admin"})
				time.Sleep(100 * time.Millisecond)
				c.Close()
			}
		}
		if n++; n%300 == 0 {
			s.St.Prune()
		}
	}
}

// ---------------- HTTP ----------------

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("POST /v1/register", s.handleRegister)
	m.HandleFunc("GET /v1/names/{name}", s.handleName)
	m.HandleFunc("GET /v1/nodes", s.handleNodes)
	m.HandleFunc("GET /v1/tunnel", s.handleTunnel)
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	m.HandleFunc("GET /v1/version", s.handleVersion)
	m.HandleFunc("/panel/", func(w http.ResponseWriter, r *http.Request) {
		if h, ok := s.panel.Load().(http.Handler); ok && h != nil {
			h.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
	m.Handle("GET /panel", http.RedirectHandler("/panel/", http.StatusFound))
	return s.logAPI(m)
}

// handleVersion answers {"version","proto"} for monitoring. It needs no login and reveals nothing else.
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	v := s.cfg.Version
	if v == "" {
		v = "unknown"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"version": v, "proto": proto.Version})
}

// logAPI logs one line per /v1/ request with the client IP used for rate limits and bans
// (client_ip) next to the socket peer (remote_addr), so trusted_proxies can be verified.
func (s *Server) logAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") && s.Log != nil {
			s.Log.Info("http request", "method", r.Method, "path", r.URL.Path,
				"client_ip", s.ClientIP(r), "remote_addr", r.RemoteAddr)
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP is the real client address (X-Forwarded-For only from trusted proxies).
func (s *Server) ClientIP(r *http.Request) string { return s.cfg.TrustedProxies.ClientIP(r) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	ip := s.ClientIP(r)
	if !s.Lim.Allow(limits.API, ip) {
		writeJSON(w, 429, proto.RegisterResponse{Code: proto.CodeRateLimited, Message: "too many requests"})
		return
	}
	var req proto.RegisterRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, proto.RegisterResponse{Code: proto.CodeBadRequest, Message: "invalid json"})
		return
	}
	if err := req.Validate(); err != nil {
		writeJSON(w, 400, proto.RegisterResponse{Code: proto.CodeBadRequest, Message: err.Error()})
		return
	}
	if !proto.ValidName(req.Name) {
		writeJSON(w, 400, proto.RegisterResponse{Code: proto.CodeNameInvalid, Message: "name must match ^[a-zA-Z][a-zA-Z0-9-]{2,31}$"})
		return
	}
	if _, err := ssh.ParsePublicKey(mustAuthorized(req.HostKey)); err != nil {
		writeJSON(w, 400, proto.RegisterResponse{Code: proto.CodeBadRequest, Message: "invalid host_key"})
		return
	}
	if !proto.Verify(req.DevicePubkey, proto.RegistrationBytes(req.Name, req.HostKey, req.RegisteredAt), req.HostKeySig) {
		writeJSON(w, 400, proto.RegisterResponse{Code: proto.CodeBadRequest, Message: "host_key_sig does not verify"})
		return
	}
	nd, err := s.St.Register(&req)
	switch {
	case err == nil:
		st := "active"
		if nd.Status == "pending" {
			st = "pending"
		}
		s.Log.Info("node registered", "node", nd.Name, "status", st, "src_ip", ip)
		writeJSON(w, 201, proto.RegisterResponse{Code: proto.CodeRegistered, NodeID: nd.ID, Status: st})
	case errors.Is(err, store.ErrNameTaken):
		writeJSON(w, 409, proto.RegisterResponse{Code: proto.CodeNameTaken, Message: proto.NameTakenMessage(req.Name)})
	case errors.Is(err, store.ErrTokenInvalid):
		s.Log.Warn("register with invalid token", "src_ip", ip)
		s.Lim.Strike(limits.BadToken, ip)
		writeJSON(w, 401, proto.RegisterResponse{Code: proto.CodeTokenInvalid, Message: "token expired, revoked, or used up"})
	case errors.Is(err, store.ErrBanned):
		writeJSON(w, 403, proto.RegisterResponse{Code: proto.CodeNodeBanned, Message: "node banned"})
	default:
		s.Log.Error("register failed", "err", err.Error())
		writeJSON(w, 500, proto.RegisterResponse{Code: "INTERNAL", Message: "internal error"})
	}
}

func mustAuthorized(line string) []byte {
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil
	}
	return pk.Marshal()
}

func (s *Server) handleName(w http.ResponseWriter, r *http.Request) {
	if !s.Lim.Allow(limits.API, s.ClientIP(r)) {
		writeJSON(w, 429, map[string]any{"code": proto.CodeRateLimited})
		return
	}
	n := r.PathValue("name")
	if !proto.ValidName(n) {
		writeJSON(w, 400, map[string]any{"available": false, "code": proto.CodeNameInvalid})
		return
	}
	if s.St.IsBanned("node", proto.NameKey(n)) {
		writeJSON(w, 200, map[string]any{"available": false, "code": proto.CodeNodeBanned})
		return
	}
	ok, err := s.St.NameAvailable(n)
	if err != nil {
		writeJSON(w, 500, map[string]any{"code": "INTERNAL"})
		return
	}
	writeJSON(w, 200, map[string]any{"available": ok})
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if !s.Lim.Allow(limits.API, s.ClientIP(r)) {
		writeJSON(w, 429, map[string]any{"code": proto.CodeRateLimited})
		return
	}
	writeJSON(w, 200, s.Rows())
}

// Rows builds protocol §4.3 rows. No credentials are ever included (only public keys + signatures).
func (s *Server) Rows() []proto.NodeRow {
	nodes, _ := s.St.Nodes()
	now := time.Now().Unix()
	rows := []proto.NodeRow{}
	for _, n := range nodes {
		if n.Status == "pending" {
			continue // not approved yet: not routable, not listed
		}
		r := proto.NodeRow{Name: n.Name, Status: n.Status, OS: n.OS, Specs: n.Specs, Limits: n.Limits,
			LastSeen: n.LastSeen, HostKey: n.HostKey, HostKeySig: n.HostKeySig, DevicePubkey: n.DevicePubkey, RegisteredAt: n.RegisteredAt}
		if c := s.Hub.Get(n.ID); c != nil {
			if c.Paused() {
				r.Status = "paused"
			} else if n.Status != "banned" {
				r.Status = "online"
			}
			r.LastSeen = c.LastPing.Load()
			r.UptimeS = now - n.OnlineSince
			if st := c.Stats(); st != nil {
				cpu := st.CPUPct
				ram := st.RAMPct
				r.Load.Now, r.RAMPct, r.GPUPct = &cpu, &ram, st.GPUPct
				// show what the owner shares, not the computer's totals (umbrella card 016 R3, R4)
				r.Specs, r.Host, r.Unlimited = proto.SharedView(n.Specs, st.Shared)
			}
		} else if n.Status != "banned" {
			r.Status = "offline"
		}
		r.Load.Avg24h = s.St.Avg24h(n.ID)
		rows = append(rows, r)
	}
	return rows
}

// ---------------- Tunnel ----------------

func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	ip := s.ClientIP(r)
	if !s.Lim.Allow(limits.Tunnel, ip) {
		http.Error(w, "rate limited", 429)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}
	ws.SetReadLimit(1 << 20)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	_, msg, err := ws.Read(ctx)
	cancel()
	if err != nil {
		ws.Close(websocket.StatusPolicyViolation, "expected Hello")
		return
	}
	var h proto.Hello
	reject := func(code, m string) {
		b, _ := json.Marshal(proto.Welcome{Proto: proto.Version, Type: proto.TypeError, Code: code, Message: m})
		ws.Write(context.Background(), websocket.MessageText, b)
		ws.Close(websocket.StatusPolicyViolation, code)
		s.Log.Warn("tunnel rejected", "code", code, "src_ip", ip)
	}
	if json.Unmarshal(msg, &h) != nil {
		reject(proto.CodeBadRequest, "bad hello")
		return
	}
	if h.Proto != proto.Version {
		reject(proto.CodeBadRequest, fmt.Sprintf("protocol %d not supported; this server speaks protocol %d: upgrade csnode", h.Proto, proto.Version))
		return
	}
	nd, err := s.St.NodeByID(h.NodeID)
	if err != nil {
		reject("NODE_UNKNOWN", "unknown node; register again")
		return
	}
	if !proto.SkewOK(time.Now().Unix(), h.TS) {
		reject(proto.CodeClockSkew, "clock skew too large")
		return
	}
	if !proto.Verify(nd.DevicePubkey, proto.HelloBytes(h.NodeID, h.TS), h.Sig) {
		reject(proto.CodeBadRequest, "bad signature")
		return
	}
	if nd.Status == "banned" || s.St.IsBanned("node", proto.NameKey(nd.Name)) {
		reject(proto.CodeNodeBanned, "node banned")
		return
	}
	if nd.Status == "pending" {
		reject(proto.CodeNodePending, "waiting for admin approval")
		return
	}
	b, _ := json.Marshal(proto.Welcome{Proto: proto.Version, Type: proto.TypeWelcome, Name: nd.Name})
	if err := ws.Write(r.Context(), websocket.MessageText, b); err != nil {
		return
	}
	nc := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
	ycfg := yamux.DefaultConfig()
	ycfg.LogOutput = nil
	ycfg.Logger = slog.NewLogLogger(s.Log.Handler(), slog.LevelDebug)
	ycfg.EnableKeepAlive = true
	ycfg.KeepAliveInterval = 20 * time.Second
	sess, err := yamux.Server(nc, ycfg)
	if err != nil {
		return
	}
	actx, acancel := context.WithTimeout(context.Background(), 15*time.Second)
	ctrlCh := make(chan net.Conn, 1)
	go func() {
		c, err := sess.Accept()
		if err == nil {
			ctrlCh <- c
		}
	}()
	var ctrl net.Conn
	select {
	case ctrl = <-ctrlCh:
	case <-actx.Done():
	}
	acancel()
	if ctrl == nil {
		sess.Close()
		return
	}
	c := hub.NewConn(nd.ID, nd.Name, sess, ctrl)
	s.Hub.Add(c)
	s.St.SetStatus(nd.ID, "online")
	s.Log.Info("node online", "node", nd.Name, "src_ip", ip)
	s.controlLoop(c, nd)
	if s.Hub.Remove(c) {
		if cur, err := s.St.NodeByID(nd.ID); err == nil && cur.Status != "banned" {
			s.St.SetStatus(nd.ID, "offline")
		}
		s.Log.Info("node offline", "node", nd.Name)
	}
	c.Close()
}

func (s *Server) controlLoop(c *hub.Conn, nd *store.Node) {
	sc := bufio.NewScanner(c.Ctrl)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	go func() { <-c.Closed; c.Ctrl.Close() }()
	for sc.Scan() {
		line := sc.Bytes()
		typ, err := proto.PeekType(line)
		if err != nil {
			continue
		}
		c.LastPing.Store(time.Now().Unix())
		switch typ {
		case proto.TypePing:
			var p proto.Ping
			json.Unmarshal(line, &p)
			c.SetStats(p.Stats)
			s.St.Touch(nd.ID)
			s.St.AddSample(nd.ID, p.Stats)
			c.Send(proto.Pong{Type: proto.TypePong, TS: time.Now().Unix()})
		case proto.TypeStatus:
			var st proto.Status
			json.Unmarshal(line, &st)
			c.SetPaused(st.Status == "paused")
			if st.Status == "paused" || st.Status == "online" {
				s.St.SetStatus(nd.ID, st.Status)
			}
		case proto.TypeLeave:
			var l proto.Leave
			json.Unmarshal(line, &l)
			if proto.SkewOK(time.Now().Unix(), l.TS) && proto.Verify(nd.DevicePubkey, proto.LeaveBytes(nd.ID, l.TS), l.Sig) {
				s.St.Release(nd.ID)
				s.Log.Info("node left, name freed", "node", nd.Name)
				c.Send(proto.Pong{Type: proto.TypePong, TS: time.Now().Unix()})
				return
			}
			s.Log.Warn("invalid leave signature", "node", nd.Name)
		case proto.TypeUpdateHostKey:
			var u proto.UpdateHostKey
			json.Unmarshal(line, &u)
			if proto.Verify(nd.DevicePubkey, proto.RegistrationBytes(nd.Name, u.HostKey, nd.RegisteredAt), u.HostKeySig) {
				s.St.UpdateHostKey(nd.ID, u.HostKey, u.HostKeySig)
			}
		}
	}
}

// bridge opens a stream and copies bytes; records metadata only.
func (s *Server) bridge(client io.ReadWriteCloser, nodeID, name, via, ip string) error {
	if !s.Lim.Acquire(ip, nodeID) {
		s.Log.Warn("stream refused: too many concurrent connections", "node", name, "src_ip", ip)
		client.Close()
		return errors.New("too many concurrent streams")
	}
	defer s.Lim.Release(ip, nodeID)
	st, err := s.Hub.Open(nodeID, via, ip)
	if err != nil {
		client.Close()
		return err
	}
	sid := s.St.SessionStart(nodeID, via, ip)
	s.Log.Info("session start", "node", name, "via", via, "src_ip", ip)
	up, down, why := hub.PipeWatch(client, st, hub.PipeOptions{Idle: max(s.cfg.SessionIdle, 0), Stall: max(s.cfg.StallAfter, 0)})
	s.St.SessionEnd(sid, up, down)
	s.Log.Info("session end", "node", name, "via", via, "src_ip", ip, "bytes_up", up, "bytes_down", down, "reason", why)
	return nil
}
