//go:build !windows

package main

// Wiring of the per-user gateway into the agent (client 015).

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/approval"
	"github.com/alishafiee1/computeshare-client/internal/node/config"
	"github.com/alishafiee1/computeshare-client/internal/node/gateway"
	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/internal/node/localpanel"
	"github.com/alishafiee1/computeshare-client/internal/node/setup"
	"github.com/alishafiee1/computeshare-client/internal/node/spaces"
	"github.com/alishafiee1/computeshare-client/internal/node/tunnel"
	"github.com/alishafiee1/computeshare-client/internal/node/users"
	"github.com/alishafiee1/computeshare-client/proto"
)

// idleStop parses config idle_stop (default 30m, "off" = never).
func idleStop(v string) time.Duration {
	if v == "off" {
		return 0
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	return 30 * time.Minute
}

// startGateway builds the gateway and returns its Serve function and host key line.
func (a *agent) startGateway(ctx context.Context) (func(*tunnel.Client), error) {
	hk, err := gateway.LoadOrCreateHostKey(config.Path(gateway.HostKeyFile))
	if err != nil {
		return nil, err
	}
	c := a.c
	mgr := &spaces.Manager{Node: c.Name, MaxRunning: c.MaxRunningSpaces, IdleStop: idleStop(c.IdleStop), Log: a.log,
		Opts: func(user string) setup.SpaceOpts {
			disk := c.DefaultDisk
			if u, ok := users.Load().Get(user); ok && u.Disk != "" {
				disk = u.Disk
			}
			return setup.SpaceOpts{Image: c.Space.Image, Mirror: c.Mirror, CPU: c.Limits.CPU, Memory: c.Limits.Memory,
				Disk: disk, GPU: c.Limits.GPU, Bandwidth: c.Limits.Bandwidth}
		}}
	go mgr.IdleLoop(ctx)
	a.spaces = mgr
	_, pw := gateway.NewLocks()
	g := &gateway.Gateway{HostKey: hk, Users: users.Load, Spaces: mgr, IPLock: a.lock, PwLock: pw, Log: a.log,
		Approval: func() bool { cc, err := config.Load(); return err == nil && cc.Approval },
		Approver: fileApprover{},
		OnLogin: func(user, ip string) {
			users.Update(func(s *users.Store) error {
				if u, ok := s.Get(user); ok {
					u.LastSeen = time.Now().UTC()
				}
				return nil
			})
		}}
	a.h.Serve = g.Serve
	line := gateway.AuthorizedHostKey(hk)
	// R1/R11: the registered host key is the gateway's; tell the broker on every connect.
	return func(t *tunnel.Client) {
		t.Send(proto.UpdateHostKey{Type: proto.TypeUpdateHostKey, HostKey: line,
			HostKeySig: proto.Sign(a.key, proto.RegistrationBytes(c.Name, line, c.RegisteredAt))})
	}, nil
}

// fileApprover holds logins in approvals.json for `csnode pending/approve/deny` (015 R9).
type fileApprover struct{}

func (fileApprover) wait(id string) string {
	for i := 0; i < 60; i++ {
		if d := approval.TakeDecision(id); d != "" {
			return d
		}
		time.Sleep(time.Second)
	}
	approval.Withdraw(id)
	return "timeout"
}

func (f fileApprover) Key(user, fp, ip string) bool {
	return f.wait(approval.AddGateway("key", user, fp, ip)) == "key"
}

func (f fileApprover) Password(user, ip string) bool {
	d := approval.Load()
	if d.IPGranted(ip, time.Now()) {
		return true
	}
	v := f.wait(approval.AddGateway("session", user, "", ip))
	return v == "once" || v == "ip"
}

// Users and SetUserDisabled make the agent a localpanel.UsersBackend (015 R12).
func (a *agent) Users() []localpanel.UserRow {
	if a.spaces == nil {
		return nil
	}
	live := a.spaces.Live()
	s := users.Load()
	var out []localpanel.UserRow
	for _, n := range s.Names() {
		u, _ := s.Get(n)
		r := localpanel.UserRow{Name: n, State: "not created", LastLogin: "-", Disk: u.Disk, Sessions: live[n], Disabled: u.Disabled}
		if st, err := incus.GetState(users.Instance(a.c.Name, n)); err == nil {
			r.State = strings.ToLower(st.Status)
			if st.Disk != nil {
				if d, ok := st.Disk["root"]; ok {
					r.Disk = fmt.Sprintf("%.1f GB / %s", float64(d.Usage)/1e9, orDash(u.Disk))
				}
			}
		}
		if u.Disabled {
			r.State = "disabled"
		}
		if !u.LastSeen.IsZero() {
			r.LastLogin = u.LastSeen.Local().Format("2006-01-02 15:04")
		}
		out = append(out, r)
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (a *agent) SetUserDisabled(name string, disabled bool) error {
	err := users.Update(func(s *users.Store) error {
		u, ok := s.Get(name)
		if !ok {
			return users.ErrNotFound
		}
		u.Disabled = disabled
		return nil
	})
	if err == nil && disabled {
		incus.Stop(users.Instance(a.c.Name, name), true)
	}
	return err
}
