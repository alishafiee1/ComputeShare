//go:build !windows

// Package spaces runs per-user Incus containers for the gateway (client 015 R5–R8).
package spaces

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/gateway"
	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/internal/node/setup"
	"github.com/alishafiee1/computeshare-client/internal/node/users"
)

// ErrBusy is shown to a user when max_running_spaces is reached (R8).
var ErrBusy = errors.New("node busy: too many active spaces, try later")

type Manager struct {
	Node       string
	Opts       func(user string) setup.SpaceOpts // image, mirror, limits; Disk = the user's quota
	MaxRunning int                               // default 3
	IdleStop   time.Duration                     // 0 = never
	Log        *slog.Logger

	mu       sync.Mutex
	live     map[string]int
	last     map[string]time.Time
	creating map[string]*sync.Mutex
}

func (m *Manager) init() {
	if m.live == nil {
		m.live, m.last, m.creating = map[string]int{}, map[string]time.Time{}, map[string]*sync.Mutex{}
	}
}

func (m *Manager) userLock(user string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	l := m.creating[user]
	if l == nil {
		l = &sync.Mutex{}
		m.creating[user] = l
	}
	return l
}

// running counts this node's running user spaces.
func (m *Manager) running() int {
	out, err := incus.Run(nil, "list", "^"+users.Instance(m.Node, ""), "--format", "csv", "-c", "ns")
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasSuffix(strings.TrimSpace(l), ",RUNNING") {
			n++
		}
	}
	return n
}

func (m *Manager) max() int {
	if m.MaxRunning <= 0 {
		return 3
	}
	return m.MaxRunning
}

// Ensure creates or starts the user's space. Two first logins at once create it once (edge case).
func (m *Manager) Ensure(ctx context.Context, user string, notice io.Writer) (gateway.Space, error) {
	l := m.userLock(user)
	l.Lock()
	defer l.Unlock()
	inst := users.Instance(m.Node, user)
	if !incus.Exists(inst) {
		if m.running() >= m.max() {
			return nil, ErrBusy
		}
		fmt.Fprintln(notice, "creating your space (first login, about 1 min)...")
		if err := setup.CreateUserSpace(inst, user, m.Opts(user), func(s string) { m.Log.Info(s, "space", inst) }); err != nil {
			m.Log.Warn("space create failed", "space", inst, "err", err)
			if err.Error() == "node out of disk" {
				return nil, err
			}
			return nil, errors.New("could not create your space; the node owner can see why in the csnode log")
		}
		m.Log.Info("space created", "space", inst, "user", user)
	} else if st, err := incus.GetState(inst); err == nil && !strings.EqualFold(st.Status, "running") {
		if m.running() >= m.max() {
			return nil, ErrBusy
		}
		if err := incus.Start(inst); err != nil {
			return nil, errors.New("could not start your space")
		}
	}
	return &space{inst: inst}, nil
}

func (m *Manager) Attach(user string) func() {
	m.mu.Lock()
	m.init()
	m.live[user]++
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		m.live[user]--
		m.last[user] = time.Now()
		m.mu.Unlock()
	}
}

// Live reports open connections per user (panel, csnode users ls).
func (m *Manager) Live() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]int{}
	for u, n := range m.live {
		if n > 0 {
			out[u] = n
		}
	}
	return out
}

// IdleLoop stops spaces nobody used for IdleStop (R7). The disk stays.
func (m *Manager) IdleLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		if m.IdleStop <= 0 {
			continue
		}
		m.mu.Lock()
		m.init()
		var idle []string
		for u, t := range m.last {
			if m.live[u] == 0 && time.Since(t) > m.IdleStop {
				idle = append(idle, u)
				delete(m.last, u)
			}
		}
		m.mu.Unlock()
		for _, u := range idle {
			if err := incus.Stop(users.Instance(m.Node, u), false); err == nil {
				m.Log.Info("idle space stopped", "user", u)
			}
		}
	}
}

// space runs commands in one container as its user, via `incus exec` (R6).
type space struct{ inst string }

func execArgs(inst string, tty bool, env []string) []string {
	a := []string{"exec", inst}
	if tty {
		a = append(a, "-t")
	} else {
		a = append(a, "-T")
	}
	for _, e := range env {
		a = append(a, "--env", e)
	}
	return append(a, "--")
}

func envNames(env []string) string {
	var n []string
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		n = append(n, k)
	}
	return strings.Join(n, ",")
}

func (s *space) Cmd(ctx context.Context, user, command string, tty bool, env []string) *exec.Cmd {
	a := append(execArgs(s.inst, tty, env), "runuser", "-l", user)
	if len(env) > 0 {
		a = append(a, "-w", envNames(env))
	}
	if command != "" {
		a = append(a, "-c", command)
	}
	return exec.CommandContext(ctx, incus.Bin, a...)
}

func (s *space) Sftp(ctx context.Context, user string) *exec.Cmd {
	return exec.CommandContext(ctx, incus.Bin, append(execArgs(s.inst, false, nil), "runuser", "-l", user, "-c", "/usr/lib/openssh/sftp-server")...)
}

func (s *space) Relay(ctx context.Context, port uint32) (io.ReadWriteCloser, error) {
	c := exec.CommandContext(ctx, incus.Bin, append(execArgs(s.inst, false, nil), setup.RelayPath, "relay", "127.0.0.1", strconv.Itoa(int(port)))...)
	in, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	return &relay{c: c, in: in, out: out}, nil
}

type relay struct {
	c   *exec.Cmd
	in  io.WriteCloser
	out io.Reader
}

func (r *relay) Read(p []byte) (int, error)  { return r.out.Read(p) }
func (r *relay) Write(p []byte) (int, error) { return r.in.Write(p) }
func (r *relay) Close() error {
	r.in.Close()
	r.c.Process.Kill()
	r.c.Wait()
	return nil
}
