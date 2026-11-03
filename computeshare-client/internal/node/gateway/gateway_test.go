//go:build !windows

package gateway

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/alishafiee1/computeshare-client/internal/node/stream"
	"github.com/alishafiee1/computeshare-client/internal/node/users"
	"github.com/alishafiee1/computeshare-client/proto"
)

// fakeSpace runs commands on the test host in a per-user directory; it stands in for incus exec.
type fakeSpace struct {
	dir   string
	relay string // address Relay dials
}

func (f *fakeSpace) Cmd(ctx context.Context, user, command string, tty bool, env []string) *exec.Cmd {
	if command == "" {
		command = "echo shell-for-" + user
	}
	c := exec.CommandContext(ctx, "sh", "-c", command)
	c.Dir, c.Env = f.dir, append(env, "SPACE_USER="+user)
	return c
}
func (f *fakeSpace) Sftp(ctx context.Context, user string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", "echo sftp")
}
func (f *fakeSpace) Relay(ctx context.Context, port uint32) (io.ReadWriteCloser, error) {
	return net.Dial("tcp", f.relay)
}

type fakeSpaces struct {
	mu      sync.Mutex
	ensured map[string]int
	live    map[string]int
	dirs    map[string]*fakeSpace
	relay   string
}

func (f *fakeSpaces) Ensure(ctx context.Context, user string, notice io.Writer) (Space, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ensured[user] == 0 {
		io.WriteString(notice, "creating your space (first login, about 1 min)...\n")
	}
	f.ensured[user]++
	if f.dirs[user] == nil {
		f.dirs[user] = &fakeSpace{dir: "/", relay: f.relay}
	}
	return f.dirs[user], nil
}
func (f *fakeSpaces) Attach(user string) func() {
	f.mu.Lock()
	f.live[user]++
	f.mu.Unlock()
	return func() { f.mu.Lock(); f.live[user]--; f.mu.Unlock() }
}

type fakeApprover struct{ allow bool }

func (a fakeApprover) Key(user, fp, ip string) bool  { return a.allow }
func (a fakeApprover) Password(user, ip string) bool { return a.allow }

type rig struct {
	g      *Gateway
	sp     *fakeSpaces
	hk     ssh.PublicKey
	key    ssh.Signer
	other  ssh.Signer
	approv bool
}

func newRig(t *testing.T) *rig {
	t.Setenv("CSNODE_DIR", t.TempDir())
	hs, err := LoadOrCreateHostKey(filepath.Join(t.TempDir(), HostKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	mk := func() ssh.Signer {
		_, p, _ := ed25519.GenerateKey(rand.Reader)
		s, _ := ssh.NewSignerFromKey(p)
		return s
	}
	r := &rig{key: mk(), other: mk(), hk: hs.PublicKey()}
	err = users.Update(func(s *users.Store) error {
		if err := s.Add("ali", "correct horse battery", []string{string(ssh.MarshalAuthorizedKey(r.key.PublicKey()))}, ""); err != nil {
			return err
		}
		return s.Add("sara", "another long password", nil, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { c.Write([]byte("inside-space\n")); c.Close() }()
		}
	}()
	r.sp = &fakeSpaces{ensured: map[string]int{}, live: map[string]int{}, dirs: map[string]*fakeSpace{}, relay: l.Addr().String()}
	ipl, pwl := NewLocks()
	r.g = &Gateway{HostKey: hs, Users: users.Load, Spaces: r.sp, IPLock: ipl, PwLock: pwl,
		Approval: func() bool { return r.approv }, Approver: fakeApprover{false},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return r
}

func (r *rig) dial(t *testing.T, ip, user string, auth ...ssh.AuthMethod) (*ssh.Client, error) {
	// net.Pipe is unbuffered and both SSH sides send their version first: use loopback TCP.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go func() {
		if b, err := l.Accept(); err == nil {
			r.g.Serve(b, &proto.StreamOpen{Kind: "ssh", SrcIP: ip})
		}
	}()
	a, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(a, "box.cs:22", &ssh.ClientConfig{User: user, Auth: auth,
		HostKeyCallback: ssh.FixedHostKey(r.hk), Timeout: 5 * time.Second})
	if err != nil {
		a.Close()
		return nil, err
	}
	return ssh.NewClient(cc, chans, reqs), nil
}

func run(t *testing.T, c *ssh.Client, cmd string) (string, string, error) {
	s, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var o, e bytes.Buffer
	s.Stdout, s.Stderr = &o, &e
	err = s.Run(cmd)
	return o.String(), e.String(), err
}

// 015 R1, R5, R6: key and password logins land in the user's own space; exit codes pass through.
func TestLoginRunsInOwnSpace(t *testing.T) {
	r := newRig(t)
	c, err := r.dial(t, "1.1.1.1", "ali", ssh.PublicKeys(r.key))
	if err != nil {
		t.Fatal(err)
	}
	out, errOut, err := run(t, c, "echo $SPACE_USER")
	if err != nil || out != "ali\n" || !strings.Contains(errOut, "creating your space") {
		t.Fatalf("%q %q %v", out, errOut, err)
	}
	_, _, err = run(t, c, "exit 7")
	if ee, ok := err.(*ssh.ExitError); !ok || ee.ExitStatus() != 7 {
		t.Fatal("exit status", err)
	}
	c.Close()
	c2, err := r.dial(t, "1.1.1.1", "sara", ssh.Password("another long password"))
	if err != nil {
		t.Fatal(err)
	}
	if out, _, _ := run(t, c2, "echo $SPACE_USER"); out != "sara\n" {
		t.Fatal(out)
	}
	c2.Close()
}

// 015 R2: root, unknown users and wrong secrets fail the same way; no other user's key works.
func TestRefusals(t *testing.T) {
	r := newRig(t)
	for _, tc := range []struct {
		user string
		auth ssh.AuthMethod
	}{{"root", ssh.Password("correct horse battery")}, {"nobody2", ssh.Password("x")}, {"ali", ssh.Password("wrong password!!")},
		{"sara", ssh.PublicKeys(r.key)}, {"ali", ssh.PublicKeys(r.other)}} {
		if _, err := r.dial(t, "2.2.2."+tc.user[:1], tc.user, tc.auth); err == nil {
			t.Fatal("login allowed for", tc.user)
		}
	}
}

// 015 R2: forwarding only to the space's own localhost; no remote forwards, no agent.
func TestForwarding(t *testing.T) {
	r := newRig(t)
	c, err := r.dial(t, "1.1.1.1", "ali", ssh.PublicKeys(r.key))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	conn, err := c.Dial("tcp", "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(conn)
	if string(b) != "inside-space\n" {
		t.Fatal(string(b))
	}
	for _, dst := range []string{"10.77.0.1:22", "8.8.8.8:53", "cs-box-sara:22"} {
		if _, err := c.Dial("tcp", dst); err == nil {
			t.Fatal("forward allowed to", dst)
		}
	}
	if _, err := c.Listen("tcp", "0.0.0.0:9000"); err == nil {
		t.Fatal("remote forward allowed")
	}
}

// 015 R10: wrong keys before the right one are not failures; five wrong passwords block the IP.
func TestLockout(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 8; i++ {
		c, err := r.dial(t, "3.3.3.3", "ali", ssh.PublicKeys(r.other, r.key))
		if err != nil {
			t.Fatal("key-then-success counted as failure at", i, err)
		}
		c.Close()
	}
	for i := 0; i < 5; i++ {
		r.dial(t, "4.4.4.4", "ali", ssh.Password("wrong password!!"))
	}
	if !r.g.IPLock.Blocked("4.4.4.4") || r.g.IPLock.Blocked("3.3.3.3") {
		t.Fatal("ip lockout")
	}
	if _, err := r.dial(t, "4.4.4.4", "ali", ssh.PublicKeys(r.key)); err == nil {
		t.Fatal("blocked ip logged in")
	}
	for i := 0; i < 4; i++ { // only a wrong key, then give up: counts once per connection
		r.dial(t, "5.5.5.5", "ali", ssh.PublicKeys(r.other))
	}
	time.Sleep(100 * time.Millisecond)
	if r.g.IPLock.Blocked("5.5.5.5") {
		t.Fatal("4 failed connections blocked")
	}
	r.dial(t, "5.5.5.5", "ali", ssh.PublicKeys(r.other))
	for i := 0; i < 50 && !r.g.IPLock.Blocked("5.5.5.5"); i++ {
		time.Sleep(20 * time.Millisecond) // the server notices the client gave up a moment later
	}
	if !r.g.IPLock.Blocked("5.5.5.5") {
		t.Fatal("5th failed key connection did not block")
	}
}

// 015 R10: 30 wrong passwords for one user pause password logins for that user; keys still work.
func TestUserPasswordLock(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 30; i++ {
		r.dial(t, "6.6."+string(rune('0'+i/10))+"."+string(rune('0'+i%10)), "ali", ssh.Password("wrong password!!"))
	}
	if _, err := r.dial(t, "7.7.7.7", "ali", ssh.Password("correct horse battery")); err == nil {
		t.Fatal("password login allowed while paused")
	}
	if c, err := r.dial(t, "7.7.7.8", "ali", ssh.PublicKeys(r.key)); err != nil {
		t.Fatal("key login refused", err)
	} else {
		c.Close()
	}
}

// 015 R9: with approval on, an unapproved key waits and is refused when the owner says no.
func TestApproval(t *testing.T) {
	r := newRig(t)
	r.approv = true
	c, err := r.dial(t, "1.1.1.1", "ali", ssh.PublicKeys(r.key))
	if err != nil {
		t.Fatal(err)
	}
	_, errOut, err := run(t, c, "echo hi")
	if err == nil || !strings.Contains(errOut, "waiting for the node owner") || !strings.Contains(errOut, "not approved") {
		t.Fatalf("%q %v", errOut, err)
	}
	c.Close()
	users.Update(func(s *users.Store) error {
		u, _ := s.Get("ali")
		u.ApproveKey(ssh.FingerprintSHA256(r.key.PublicKey()))
		return nil
	})
	c, _ = r.dial(t, "1.1.1.1", "ali", ssh.PublicKeys(r.key))
	if out, _, err := run(t, c, "echo hi"); err != nil || out != "hi\n" {
		t.Fatal("approved key refused", out, err)
	}
	c.Close()
}

// 015 R7: Attach counts live connections for idle stop.
func TestAttachCounts(t *testing.T) {
	r := newRig(t)
	c, _ := r.dial(t, "1.1.1.1", "ali", ssh.PublicKeys(r.key))
	run(t, c, "true")
	r.sp.mu.Lock()
	n := r.sp.live["ali"]
	r.sp.mu.Unlock()
	if n != 1 {
		t.Fatal("live", n)
	}
	c.Close()
	time.Sleep(100 * time.Millisecond)
	r.sp.mu.Lock()
	defer r.sp.mu.Unlock()
	if r.sp.live["ali"] != 0 {
		t.Fatal("not released")
	}
}

// 015 R6: interactive shells get a pty; window changes are accepted.
func TestPTYShell(t *testing.T) {
	r := newRig(t)
	c, err := r.dial(t, "1.1.1.1", "ali", ssh.PublicKeys(r.key))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, _ := c.NewSession()
	var o bytes.Buffer
	s.Stdout = &o
	if err := s.RequestPty("xterm", 40, 100, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}
	s.WindowChange(50, 120)
	if err := s.Wait(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o.String(), "shell-for-ali") {
		t.Fatalf("%q", o.String())
	}
}

// 015 R1: a broker stream (StreamOpen line + raw bytes) reaches the gateway, never a space sshd.
func TestThroughStreamHandler(t *testing.T) {
	r := newRig(t)
	h := &stream.Handler{Target: func() (string, error) { t.Fatal("dialed a space directly"); return "", nil },
		Serve: r.g.Serve, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go func() {
		if b, err := l.Accept(); err == nil {
			h.Handle(b)
		}
	}()
	a, _ := net.Dial("tcp", l.Addr().String())
	a.Write([]byte(`{"stream_id":9,"kind":"ssh","via":"jump","src_ip":"9.9.9.9"}` + "\n"))
	cc, chans, reqs, err := ssh.NewClientConn(a, "box.cs:22", &ssh.ClientConfig{User: "ali", Auth: []ssh.AuthMethod{ssh.PublicKeys(r.key)},
		HostKeyCallback: ssh.FixedHostKey(r.hk), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c := ssh.NewClient(cc, chans, reqs)
	defer c.Close()
	if len(h.Sessions()) != 1 || h.Sessions()[0].SrcIP != "9.9.9.9" {
		t.Fatal("session not tracked for csnode who/kick", h.Sessions())
	}
	if out, _, err := run(t, c, "echo $SPACE_USER"); err != nil || out != "ali\n" {
		t.Fatal(out, err)
	}
}
