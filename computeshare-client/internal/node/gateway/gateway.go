//go:build !windows

// Package gateway is the node's own SSH server for per-user spaces (client 015).
//
// The broker still only forwards encrypted bytes; csnode terminates the inner SSH here,
// checks the user against the node-local store (internal/node/users), applies lockout and
// owner approval outside any space, and runs the session inside that user's container.
// There is never a shell on the host: every command goes through Space.
package gateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"

	"github.com/alishafiee1/computeshare-client/internal/node/lockout"
	"github.com/alishafiee1/computeshare-client/internal/node/users"
	"github.com/alishafiee1/computeshare-client/proto"
)

// Space runs things inside one user's container, always as that user (R6).
type Space interface {
	// Cmd runs command (empty = login shell); tty asks for a terminal.
	Cmd(ctx context.Context, user, command string, tty bool, env []string) *exec.Cmd
	// Sftp runs the sftp server as the user.
	Sftp(ctx context.Context, user string) *exec.Cmd
	// Relay connects to 127.0.0.1:port inside the space (direct-tcpip, R2).
	Relay(ctx context.Context, port uint32) (io.ReadWriteCloser, error)
}

// Spaces creates, starts and counts use of per-user spaces (R5, R7, R8).
type Spaces interface {
	// Ensure makes the user's space exist and run. notice gets one-line progress messages.
	Ensure(ctx context.Context, user string, notice io.Writer) (Space, error)
	// Attach marks one live connection; the returned func ends it (idle stop counts these).
	Attach(user string) func()
}

// Approver holds a login until the owner decides (R9). Both return true when allowed.
type Approver interface {
	Key(user, fingerprint, ip string) bool
	Password(user, ip string) bool
}

type Gateway struct {
	HostKey  ssh.Signer
	Users    func() *users.Store
	Spaces   Spaces
	Approval func() bool // approval on?
	Approver Approver
	IPLock   *lockout.Tracker // 5 failures / 10 min per client IP (R10)
	PwLock   *lockout.Tracker // 30 password failures / 1 h per user (R10)
	Log      *slog.Logger
	OnLogin  func(user, ip string)
}

// NewLocks returns the two R10 trackers.
func NewLocks() (ip, pw *lockout.Tracker) {
	ip = lockout.New()
	pw = lockout.New()
	pw.Max, pw.Window, pw.BlockFor = 30, time.Hour, time.Hour
	return
}

// conn is the per-connection auth bookkeeping for R10.
type conn struct {
	ip          string
	keysOffered bool
	counted     bool
}

const authFail = "permission denied"

func (g *Gateway) config(c *conn) *ssh.ServerConfig {
	cfg := &ssh.ServerConfig{
		MaxAuthTries:  6,
		ServerVersion: "SSH-2.0-csnode",
		PasswordCallback: func(m ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			name := m.User()
			u, ok := g.Users().Get(name)
			if g.PwLock.Blocked(name) {
				return nil, errors.New(authFail) // password logins paused for this user; keys still work
			}
			if !ok || !u.CanLogin() || !u.CheckPassword(string(pw)) {
				g.IPLock.Fail(c.ip)
				c.counted = true
				if ok {
					g.PwLock.Fail(name)
				}
				g.Log.Info("auth failure", "src_ip", c.ip, "user", name, "method", "password")
				return nil, errors.New(authFail)
			}
			return &ssh.Permissions{Extensions: map[string]string{"method": "password"}}, nil
		},
		PublicKeyCallback: func(m ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			c.keysOffered = true
			u, ok := g.Users().Get(m.User())
			if !ok || !u.CanLogin() || !u.HasKey(k) {
				return nil, errors.New(authFail) // not a failure by itself (R10)
			}
			return &ssh.Permissions{Extensions: map[string]string{"method": "publickey", "fp": ssh.FingerprintSHA256(k)}}, nil
		},
	}
	cfg.AddHostKey(g.HostKey)
	return cfg
}

// Serve runs one SSH connection that arrived as a broker stream.
func (g *Gateway) Serve(nc net.Conn, so *proto.StreamOpen) {
	defer nc.Close()
	c := &conn{ip: so.SrcIP}
	if g.IPLock.Blocked(c.ip) {
		return
	}
	nc.SetDeadline(time.Now().Add(2 * time.Minute)) // auth must finish in time
	sc, chans, reqs, err := ssh.NewServerConn(nc, g.config(c))
	if err != nil {
		if c.keysOffered && !c.counted { // offered keys, none worked, gave up (R10)
			g.IPLock.Fail(c.ip)
		}
		return
	}
	defer sc.Close()
	nc.SetDeadline(time.Time{})
	user := sc.User()
	g.Log.Info("gateway login", "src_ip", c.ip, "user", user, "method", sc.Permissions.Extensions["method"])
	go ssh.DiscardRequests(reqs) // no tcpip-forward, no host agent (R2)
	done := g.Spaces.Attach(user)
	defer done()
	if g.OnLogin != nil {
		g.OnLogin(user, c.ip)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { sc.Wait(); cancel() }()

	// The first channel waits for approval and the space; later ones reuse the result.
	var once sync.Once
	var space Space
	var readyErr error
	ready := func(notice io.Writer) (Space, error) {
		once.Do(func() {
			if g.Approval != nil && g.Approval() && !g.approve(sc, user, c.ip, notice) {
				readyErr = errors.New("not approved by the node owner")
				return
			}
			space, readyErr = g.Spaces.Ensure(ctx, user, notice)
		})
		return space, readyErr
	}
	for nch := range chans {
		switch nch.ChannelType() {
		case "session":
			ch, rq, err := nch.Accept()
			if err != nil {
				continue
			}
			go g.session(ctx, ch, rq, user, ready)
		case "direct-tcpip":
			go g.directTCPIP(ctx, nch, ready)
		default:
			nch.Reject(ssh.Prohibited, "not allowed on this node")
		}
	}
}

func (g *Gateway) approve(sc *ssh.ServerConn, user, ip string, notice io.Writer) bool {
	ext := sc.Permissions.Extensions
	if ext["method"] == "publickey" {
		if u, ok := g.Users().Get(user); ok && u.KeyApproved(ext["fp"]) {
			return true
		}
		fmt.Fprintln(notice, "waiting for the node owner to approve this key (up to 60 s)...")
		return g.Approver.Key(user, ext["fp"], ip)
	}
	fmt.Fprintln(notice, "waiting for the node owner to approve this login (up to 60 s)...")
	return g.Approver.Password(user, ip)
}

// allowedEnv is what a client may set; everything else is ignored (no LD_PRELOAD games).
func allowedEnv(k string) bool { return k == "LANG" || k == "TERM" || strings.HasPrefix(k, "LC_") }

type ptyReq struct {
	Term          string
	Cols, Rows    uint32
	Width, Height uint32
	Modes         string
}

func (g *Gateway) session(ctx context.Context, ch ssh.Channel, reqs <-chan *ssh.Request, user string, ready func(io.Writer) (Space, error)) {
	defer ch.Close()
	var env []string
	var tty *ptyReq
	t := &term{}
	var cmd *exec.Cmd
	var startOnce sync.Once
	start := func(build func(Space) *exec.Cmd) bool {
		ok := false
		startOnce.Do(func() {
			sp, err := ready(ch.Stderr())
			if err != nil {
				fmt.Fprintln(ch.Stderr(), err.Error())
				return
			}
			cmd = build(sp)
			ok = true
		})
		return ok
	}
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			var p ptyReq
			if ssh.Unmarshal(req.Payload, &p) != nil {
				req.Reply(false, nil)
				continue
			}
			tty = &p
			env = append(env, "TERM="+p.Term)
			req.Reply(true, nil)
		case "env":
			var kv struct{ K, V string }
			if ssh.Unmarshal(req.Payload, &kv) == nil && allowedEnv(kv.K) {
				env = append(env, kv.K+"="+kv.V)
				req.Reply(true, nil)
				continue
			}
			req.Reply(false, nil)
		case "shell", "exec", "subsystem":
			var arg struct{ S string }
			if req.Type != "shell" && ssh.Unmarshal(req.Payload, &arg) != nil {
				req.Reply(false, nil)
				continue
			}
			if req.Type == "subsystem" && arg.S != "sftp" {
				req.Reply(false, nil)
				continue
			}
			ok := start(func(sp Space) *exec.Cmd {
				if req.Type == "subsystem" {
					return sp.Sftp(ctx, user)
				}
				return sp.Cmd(ctx, user, arg.S, tty != nil, env)
			})
			// Accept even on failure so the client shows the reason, then end with status 1.
			req.Reply(true, nil)
			if !ok {
				ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{1}))
				return
			}
			go func() {
				code := g.run(ch, cmd, tty, t)
				ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{uint32(code)}))
				ch.Close()
			}()
		case "window-change":
			var w struct{ Cols, Rows, Width, Height uint32 }
			if ssh.Unmarshal(req.Payload, &w) == nil {
				t.resize(tty, w.Cols, w.Rows)
			}
			if req.WantReply {
				req.Reply(false, nil)
			}
		case "signal":
			var s struct{ Name string }
			if ssh.Unmarshal(req.Payload, &s) == nil && cmd != nil && cmd.Process != nil {
				if sig, ok := signals[s.Name]; ok {
					cmd.Process.Signal(sig)
				}
			}
		default:
			req.Reply(false, nil) // auth-agent-req@openssh.com, x11-req, ... (R2)
		}
	}
}

var signals = map[string]syscall.Signal{"INT": syscall.SIGINT, "TERM": syscall.SIGTERM, "KILL": syscall.SIGKILL,
	"HUP": syscall.SIGHUP, "QUIT": syscall.SIGQUIT, "USR1": syscall.SIGUSR1, "USR2": syscall.SIGUSR2}

// term is the session's pty master once started; window changes before that update the request.
type term struct {
	mu sync.Mutex
	f  *os.File
}

func (t *term) resize(tty *ptyReq, cols, rows uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f != nil {
		pty.Setsize(t.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	} else if tty != nil {
		tty.Cols, tty.Rows = cols, rows
	}
}

// run starts cmd wired to the channel and returns its exit code.
func (g *Gateway) run(ch ssh.Channel, cmd *exec.Cmd, tty *ptyReq, t *term) int {
	if tty != nil {
		t.mu.Lock()
		f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(tty.Cols), Rows: uint16(tty.Rows)})
		t.f = f
		t.mu.Unlock()
		if err != nil {
			fmt.Fprintln(ch.Stderr(), "cannot start session")
			return 255
		}
		go io.Copy(f, ch)
		io.Copy(ch, f) // ends when the process exits (EIO on the master)
		t.mu.Lock()
		f.Close()
		t.f = nil
		t.mu.Unlock()
	} else {
		cmd.Stdout, cmd.Stderr = ch, ch.Stderr()
		in, err := cmd.StdinPipe()
		if err != nil || cmd.Start() != nil {
			fmt.Fprintln(ch.Stderr(), "cannot start session")
			return 255
		}
		go func() { io.Copy(in, ch); in.Close() }()
	}
	err := cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		return 255
	}
	return 0
}

func (g *Gateway) directTCPIP(ctx context.Context, nch ssh.NewChannel, ready func(io.Writer) (Space, error)) {
	var d struct {
		Host     string
		Port     uint32
		OrigHost string
		OrigPort uint32
	}
	if ssh.Unmarshal(nch.ExtraData(), &d) != nil {
		nch.Reject(ssh.ConnectionFailed, "bad request")
		return
	}
	// Only the user's own space: its loopback (R2). Never the host, other spaces or the internet.
	if h := strings.ToLower(d.Host); h != "localhost" && h != "127.0.0.1" && h != "::1" || d.Port == 0 || d.Port > 65535 {
		nch.Reject(ssh.Prohibited, "only localhost inside your space can be forwarded")
		return
	}
	sp, err := ready(io.Discard)
	if err != nil {
		nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	r, err := sp.Relay(ctx, d.Port)
	if err != nil {
		nch.Reject(ssh.ConnectionFailed, "connection refused")
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		r.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	go func() { io.Copy(r, ch); r.Close() }()
	io.Copy(ch, r)
	ch.Close()
}

// HostKeyFile is the gateway's node-level host key in the csnode config dir (R1).
const HostKeyFile = "gateway_host_ed25519_key"

// LoadOrCreateHostKey reads or makes the gateway's Ed25519 host key (OpenSSH PEM, mode 0600).
func LoadOrCreateHostKey(path string) (ssh.Signer, error) {
	if b, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(b)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	blk, err := ssh.MarshalPrivateKey(priv, "csnode gateway")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(blk), 0o600); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

// AuthorizedHostKey is the "ssh-ed25519 AAAA..." form csnode registers as host_key.
func AuthorizedHostKey(s ssh.Signer) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.PublicKey())))
}
