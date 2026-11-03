//go:build !windows

package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/blocklist"
	"github.com/alishafiee1/computeshare-client/internal/node/config"
	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/internal/node/lockout"
	"github.com/alishafiee1/computeshare-client/internal/node/quota"
	"github.com/alishafiee1/computeshare-client/internal/node/register"
	"github.com/alishafiee1/computeshare-client/internal/node/setup"
	"github.com/alishafiee1/computeshare-client/internal/node/spaces"
	"github.com/alishafiee1/computeshare-client/internal/node/stats"
	"github.com/alishafiee1/computeshare-client/internal/node/stream"
	"github.com/alishafiee1/computeshare-client/internal/node/tunnel"
	"github.com/alishafiee1/computeshare-client/proto"
)

type agent struct {
	c    *config.Config
	log  *slog.Logger
	key  ed25519.PrivateKey
	lock *lockout.Tracker
	h    *stream.Handler
	// spaces is set in per-user mode (client 015).
	spaces *spaces.Manager
}

func newAgent(c *config.Config, log *slog.Logger) *agent {
	return &agent{c: c, log: log, lock: lockout.New()}
}

func (a *agent) hasIncus() bool {
	return a.c.Space.Target == "" && incus.Available() && incus.Exists(a.c.Space.Instance)
}

// target returns the ONLY destination a stream can reach: <space-ip>:22 (006 R4).
func (a *agent) target() (string, error) {
	if a.c.Space.Target != "" { // lab override, see config.Space.Target
		return a.c.Space.Target, nil
	}
	ip, err := incus.IPv4(a.c.Space.Instance)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(ip, "22"), nil
}

func (a *agent) Run(ctx context.Context, ping, maxBack time.Duration) error {
	key, err := config.DeviceKey()
	if err != nil {
		return err
	}
	a.key = key
	coll := &stats.Collector{}
	if a.hasIncus() {
		inst := a.c.Space.Instance
		coll.SpaceCPU = func() (int64, float64, float64, bool) {
			s, err := incus.GetState(inst)
			if err != nil {
				return 0, 0, 0, false
			}
			cores := float64(setup.Detect().CPUs)
			if a.c.Limits.CPU != "" {
				var n float64
				if _, err := fmtSscan(a.c.Limits.CPU, &n); err == nil && n > 0 {
					cores = n
				}
			}
			mp := 0.0
			if s.Memory.Total > 0 {
				mp = 100 * float64(s.Memory.Usage) / float64(s.Memory.Total)
			}
			return s.CPU.Usage, cores, mp, true
		}
		coll.NetBytes = func() (int64, int64, bool) { return spaceNet(inst) }
		go a.quotaLoop(ctx)
		go a.sshdLogLoop(ctx)
		go a.pendingLoop(ctx)
	}
	bl := blocklist.Open(config.Dir()) // the owner's block list (card 015 R11)
	a.h = &stream.Handler{Target: a.target, Log: a.log, MaxStreams: a.c.MaxStreams,
		Idle: sessionLimit(a.c.SessionIdle), Stall: sessionLimit(a.c.SessionStall),
		// the `csnode who` snapshot is rewritten whenever a session is added or removed (umbrella card 021 R4)
		OnChange: a.writeSessions,
		OnDial: func(p int, so *proto.StreamOpen) {
			a.lock.Map(p, so.SrcIP)
			if c, err := config.Load(); err == nil && c.Approval && a.hasIncus() {
				setup.MapPort(a.c.Name, p, so.SrcIP)
			}
		},
		OnEnd: func(p int) {
			a.lock.Unmap(p)
			if c, err := config.Load(); err == nil && c.Approval && a.hasIncus() {
				setup.UnmapPort(a.c.Name, p)
			}
		},
		Gates: []stream.Gate{
			func(so *proto.StreamOpen) (bool, string) {
				st := config.LoadState()
				if st.Stopped || st.Paused {
					return false, "node paused"
				}
				return true, ""
			},
			func(so *proto.StreamOpen) (bool, string) {
				if bl.Blocked(so.SrcIP) {
					return false, "you are blocked on this node"
				}
				return true, ""
			},
			func(so *proto.StreamOpen) (bool, string) {
				if a.lock.Blocked(so.SrcIP) {
					return false, "too many failed logins; try again later"
				}
				return true, ""
			},
		}}
	go a.sessionsLoop(ctx)
	go a.blockLoop(ctx, bl)
	var announce func(*tunnel.Client)
	if a.c.Gateway {
		if announce, err = a.startGateway(ctx); err != nil {
			return err
		}
	}
	a.startPanel(ctx)
	gpuName := setup.Detect().GPU
	diskEnforced := !a.hasIncus() || setup.QuotaDriver(setup.StorageDriver()) // card 016 R2: never claim a limit that nothing enforces
	withShared := func() proto.Stats {                                        // what the owner shares, so the list shows it instead of the host totals (card 016 R3)
		st := coll.Collect()
		l := a.c.Limits
		if c, err := config.Load(); err == nil {
			l = c.Limits
		}
		sh := l.SharedSpecs(gpuName)
		if !diskEnforced {
			sh.DiskGB = 0 // shown as the host value with a star, not as a limit
		}
		st.Shared = &sh
		return st
	}
	t := &tunnel.Client{Server: a.c.Server, NodeID: a.c.NodeID, Key: key, HTTP: register.HTTPClient(a.c.CAFile),
		PingEvery: ping, MaxBack: maxBack, Stats: withShared, Log: a.log,
		Paused:   func() bool { s := config.LoadState(); return s.Paused || s.Stopped },
		OnStream: a.h.Handle,
		// last good broker address, so a failing DNS lookup (WSL) does not stop a reconnect (card 017 R2)
		LastIP: func() string {
			b, _ := os.ReadFile(config.Path("broker_ip"))
			return strings.TrimSpace(string(b))
		},
		RememberIP: func(ip string) { os.WriteFile(config.Path("broker_ip"), []byte(ip+"\n"), 0o600) },
	}
	t.Online = func(on bool) {
		os.WriteFile(config.Path("online"), []byte(map[bool]string{true: "online", false: "offline"}[on]), 0o600)
		if on && announce != nil {
			go announce(t)
		}
	}
	// kill switch: while stopped, keep the tunnel down (005 / 001 R8)
	for {
		if config.LoadState().Stopped {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
				continue
			}
		}
		rctx, cancel := context.WithCancel(ctx)
		go func() {
			for rctx.Err() == nil {
				time.Sleep(time.Second)
				if config.LoadState().Stopped {
					cancel()
				}
			}
		}()
		err := t.Run(rctx)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, tunnel.ErrFatal) {
			return err
		}
	}
}

func (a *agent) writeSessions() {
	config.WriteJSON("sessions.json", a.h.Sessions())
}

// sessionsLoop keeps the byte and idle counters in sessions.json fresh while sessions are live.
func (a *agent) sessionsLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if len(a.h.Sessions()) > 0 {
				a.writeSessions()
			}
		}
	}
}

// sessionLimit turns a config value ("15m", "90s", "off", "") into the Handler convention:
// zero = default, negative = off.
func sessionLimit(v string) time.Duration {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "":
		return 0
	case "off", "0":
		return -1
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil || d < 0 {
		return 0
	}
	return d
}

func spaceNet(inst string) (int64, int64, bool) {
	s, err := incus.GetState(inst)
	if err != nil {
		return 0, 0, false
	}
	n, ok := s.Network["eth0"]
	if !ok {
		return 0, 0, false
	}
	return n.Counters.BytesReceived, n.Counters.BytesSent, true
}

func loadQuota() quota.Counter { return quota.Load() }

// quotaLoop: 005 R3 — count rx+tx per Tehran day; block internet egress when reached.
func (a *agent) quotaLoop(ctx context.Context) {
	applied := false
	first := true
	for {
		c, err := config.Load()
		if err == nil {
			limit, _ := proto.ParseQuota(c.Limits.Quota)
			q := quota.Load()
			if rx, tx, ok := spaceNet(c.Space.Instance); ok {
				reached := q.Update(time.Now(), rx+tx, limit)
				q.Save()
				if reached != applied || first {
					gw := c.Space.Gateway
					if gw == "" {
						gw = setup.DefaultGateway
					}
					if err := setup.ApplyACL(gw, reached); err == nil {
						applied, first = reached, false
						a.log.Info("quota state", "reached", reached, "used", q.Used)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// sshdLogLoop follows sshd logs inside the space for lockout (005 R7).
func (a *agent) sshdLogLoop(ctx context.Context) {
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, incus.Bin, "exec", a.c.Space.Instance, "--", "journalctl", "-u", "ssh", "-f", "-n", "0", "-o", "cat")
		out, err := cmd.StdoutPipe()
		if err == nil && cmd.Start() == nil {
			sc := bufio.NewScanner(out)
			for sc.Scan() {
				if ip := a.lock.Line(sc.Text()); ip != "" {
					a.log.Info("auth failure", "src_ip", ip, "locked", a.lock.Blocked(ip))
				}
			}
			cmd.Wait()
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (a *agent) pendingLoop(ctx context.Context) {
	for {
		if c, err := config.Load(); err == nil && c.Approval {
			setup.CollectPending(c.Name)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func leave(c *config.Config) error {
	key, err := config.DeviceKey()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	var t *tunnel.Client
	t = &tunnel.Client{Server: c.Server, NodeID: c.NodeID, Key: key, HTTP: register.HTTPClient(c.CAFile),
		PingEvery: time.Hour, Log: slog.New(slog.NewTextHandler(discard{}, nil)),
		OnStream: func(n net.Conn) { n.Close() },
		Online: func(on bool) {
			if on {
				go func() { done <- t.Leave() }()
			}
		}}
	go t.Run(ctx)
	select {
	case err := <-done:
		time.Sleep(500 * time.Millisecond)
		return err
	case <-ctx.Done():
		return errors.New("broker unreachable")
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func fmtSscan(s string, v *float64) (int, error) { return fmt.Sscan(s, v) }
