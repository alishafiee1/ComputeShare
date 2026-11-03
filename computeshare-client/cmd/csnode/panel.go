//go:build !windows

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/config"
	"github.com/alishafiee1/computeshare-client/internal/node/control"
	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/internal/node/localpanel"
	"github.com/alishafiee1/computeshare-client/internal/node/setup"
	"github.com/alishafiee1/computeshare-client/internal/node/stream"
	"github.com/alishafiee1/computeshare-client/proto"
)

func panelAddr(c *config.Config) string {
	if c.Panel == "" {
		return localpanel.DefaultAddr
	}
	return c.Panel
}

func panelTokenPath() string { return config.Path("panel-token") }

// startPanel serves the local panel until ctx ends (client card 007).
func (a *agent) startPanel(ctx context.Context) {
	addr := panelAddr(a.c)
	if addr == "off" {
		return
	}
	if err := localpanel.CheckLoopback(addr); err != nil {
		a.log.Error("local panel disabled", "err", err.Error())
		return
	}
	tok, err := localpanel.LoadOrCreateToken(panelTokenPath())
	if err != nil {
		a.log.Error("local panel token", "err", err.Error())
		return
	}
	srv := &http.Server{Addr: addr, Handler: localpanel.New(a, addr, tok), ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); srv.Close() }()
	go func() {
		a.log.Info("local panel listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.log.Error("local panel", "err", err.Error())
		}
	}()
}

// ---- localpanel.Backend ----

func (a *agent) Status() localpanel.Status {
	c, err := config.Load()
	if err != nil {
		c = a.c
	}
	st := localpanel.Status{Name: c.Name, Server: c.Server, JumpHost: config.JumpHost(c.Server), User: c.Space.User,
		Registered: c.NodeID != "", Sharing: control.Sharing(), Space: c.Space.Instance,
		CPU: c.Limits.CPU, Memory: c.Limits.Memory, Bandwidth: c.Limits.Bandwidth, Quota: c.Limits.Quota}
	if b, err := os.ReadFile(config.Path("online")); err == nil {
		st.Tunnel = strings.TrimSpace(string(b)) == "online"
	}
	if a.hasIncus() {
		st.SpaceIP, _ = incus.IPv4(c.Space.Instance)
		if hk, err := setup.HostKey(c.Name); err == nil {
			st.HostKeyFP, _ = proto.Fingerprint(hk)
		}
	}
	q := loadQuota()
	st.UsedTodayGB, st.QuotaReached = float64(q.Used)/1e9, q.Reached
	return st
}

func (a *agent) Sessions() []stream.Session {
	if a.h == nil {
		return nil
	}
	return a.h.Sessions()
}
func (a *agent) Kick(id uint32) bool { return a.h != nil && a.h.Kick(id) }
func (a *agent) KickIP(ip string) int {
	if a.h == nil {
		return 0
	}
	return a.h.KickIP(ip)
}
func (a *agent) KickAll() int {
	if a.h == nil {
		return 0
	}
	return a.h.KickAll()
}
func (a *agent) SetShare(on bool) error {
	return control.SetShare(on)
}
func (a *agent) ApplyLimits(l control.Limits) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	if err := control.ApplyLimits(c, l, control.Default, a.hasIncus()); err != nil {
		return err
	}
	a.c.Limits = c.Limits
	return nil
}

// ---- CLI: csnode panel | kick | share ----

func cmdPanel() {
	c := mustCfg()
	addr := panelAddr(c)
	if addr == "off" {
		die(exitErr, "local panel is disabled (panel: off in config.yaml)")
	}
	tok, err := localpanel.LoadOrCreateToken(panelTokenPath())
	if err != nil {
		die(exitErr, "cannot read the panel token (run as root/sudo): %v", err)
	}
	fmt.Println("Open this link in a browser on this computer (it signs you in; keep it private):")
	fmt.Println(localpanel.URL(addr, tok))
}

// panelPost sends a CLI action to the running agent's panel with the bearer token.
func panelPost(path string, form url.Values) {
	c := mustCfg()
	tok, err := localpanel.LoadOrCreateToken(panelTokenPath())
	if err != nil {
		die(exitErr, "cannot read the panel token (run as root/sudo): %v", err)
	}
	r, _ := http.NewRequest("POST", "http://"+panelAddr(c)+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Authorization", "Bearer "+tok)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(r)
	if err != nil {
		die(exitErr, "agent not reachable (is csnode running?): %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		die(exitErr, "%s", strings.TrimSpace(string(b)))
	}
	fmt.Print(string(b))
}

func cmdKick(args []string) {
	if len(args) != 1 {
		usage()
	}
	if strings.ContainsAny(args[0], ".:") {
		panelPost("/kick", url.Values{"ip": {args[0]}})
	} else {
		panelPost("/kick", url.Values{"id": {args[0]}})
	}
}

func cmdShare(args []string) {
	if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
		usage()
	}
	on := map[string]string{"on": "1", "off": "0"}[args[0]]
	c := mustCfg()
	if _, err := http.Get("http://" + panelAddr(c) + "/static/app.css"); err != nil {
		// agent not running: just flip the state file
		control.SetShare(on == "1")
		fmt.Println("sharing:", args[0])
		return
	}
	panelPost("/share", url.Values{"on": {on}})
}
