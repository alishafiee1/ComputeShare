// csbroker: ComputeShare relay server (AGPL-3.0).
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/alishafiee1/computeshare-client/proto"
	"github.com/alishafiee1/computeshare-server/internal/broker/blog"
	"github.com/alishafiee1/computeshare-server/internal/broker/limits"
	"github.com/alishafiee1/computeshare-server/internal/broker/netutil"
	"github.com/alishafiee1/computeshare-server/internal/broker/panel"
	"github.com/alishafiee1/computeshare-server/internal/broker/server"
	"github.com/alishafiee1/computeshare-server/internal/broker/store"
	"golang.org/x/term"
)

var version = "0.2.0-dev"

func usage() {
	fmt.Fprint(os.Stderr, `csbroker `+version+`
usage:
  csbroker serve [--config /etc/csbroker.yaml] [--http 127.0.0.1:8088] [--ssh :2222] [--state DIR]
                 [--trusted-proxies 127.0.0.1,172.18.0.0/16] [--require-approval true] [--panel true] [--tls-cert F --tls-key F]
  csbroker admin set-password [--user admin]        (password read from the terminal or stdin)
  csbroker admin 2fa enable|disable|status [--user admin]   (two-step panel login: authenticator app + backup codes)
  csbroker token create [--label L] [--ttl 168h] [--uses 1]
  csbroker token ls | token revoke <id|label>
  csbroker node ls | node approve <name> | node ban <name> [--reason R] | node release <name> | node unban <name>
  csbroker version
common: --state DIR (default /var/lib/csbroker), or env CSBROKER_STATE
`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "token":
		token(os.Args[2:])
	case "node":
		node(os.Args[2:])
	case "admin":
		admin(os.Args[2:])
	case "version", "--version":
		fmt.Println("csbroker", version, "proto", proto.Version)
	default:
		usage()
	}
}

func stateFlag(fs *flag.FlagSet) *string {
	def := os.Getenv("CSBROKER_STATE")
	if def == "" {
		def = "/var/lib/csbroker"
	}
	return fs.String("state", def, "state directory")
}

func openStore(dir string) *store.Store {
	os.MkdirAll(dir, 0o700)
	st, err := store.Open(filepath.Join(dir, "csbroker.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	return st
}

// minimal YAML (key: value) reader so the binary keeps zero extra deps.
func readConfig(path string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(strings.SplitN(l, "#", 2)[0])
		if k, v, ok := strings.Cut(l, ":"); ok {
			m[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return m
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "/etc/csbroker.yaml", "config file")
	state := stateFlag(fs)
	httpAddr := fs.String("http", "", "HTTP(S) listen address for registration/tunnel/panel (default 127.0.0.1:8088)")
	sshAddr := fs.String("ssh", "", "SSH jump listen address, the only SSH port (default :2222)")
	cert := fs.String("tls-cert", "", "TLS certificate (empty = plain HTTP behind a TLS proxy)")
	key := fs.String("tls-key", "", "TLS key")
	trusted := fs.String("trusted-proxies", "", "comma-separated IPs/CIDRs whose X-Forwarded-For is trusted (yaml trusted_proxies)")
	approval := fs.String("require-approval", "", "true = new nodes wait for admin approval (yaml require_approval)")
	panelOn := fs.String("panel", "", "true/false: admin web panel at /panel/ (yaml panel, default true)")
	secure := fs.String("panel-secure-cookie", "", "true/false: Secure cookie (yaml panel_secure_cookie, default true)")
	ping := fs.Duration("ping", 15*time.Second, "advertised ping interval")
	offline := fs.Duration("offline-after", 45*time.Second, "mark node offline after silence")
	connMin := fs.Int("limit-conn-per-min", 0, "legacy: one rate for ssh, tunnel and api (0 = per-kind defaults)")
	sshMin := fs.String("limit-ssh-per-min", "", "new SSH connections per IP per minute (yaml limit_ssh_per_min, default 60)")
	tunMin := fs.String("limit-tunnel-per-min", "", "node tunnel connects per IP per minute (yaml limit_tunnel_per_min, default 12)")
	apiMin := fs.String("limit-api-per-min", "", "API requests per IP per minute (yaml limit_api_per_min, default 60)")
	concIP := fs.Int("limit-concurrent-ip", 40, "concurrent streams per IP")
	concNode := fs.Int("limit-concurrent-node", 50, "concurrent streams per node")
	sessIdle := fs.String("session-idle", "", "close a jump session with no traffic for this long, e.g. 15m; 0 = never (yaml session_idle, default 15m)")
	stallAfter := fs.String("session-stall", "", "close a jump session whose write is blocked this long, e.g. 90s; 0 = never (yaml session_stall, default 90s)")
	fs.Parse(args)
	c := readConfig(*cfgPath)
	if _, ok := c["port_range"]; ok {
		fmt.Fprintln(os.Stderr, "warning: port_range is no longer used (v2 has no per-node ports; all SSH goes through the ssh port)")
	}
	pick := func(flagv, key, def string) string {
		if flagv != "" {
			return flagv
		}
		if v := c[key]; v != "" {
			return v
		}
		return def
	}
	tp, err := netutil.ParseTrusted(pick(*trusted, "trusted_proxies", ""))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	st := openStore(pick("", "state", *state))
	st.RequireApproval = isTrue(pick(*approval, "require_approval", "false"))
	cfg := server.Config{
		HTTPAddr: pick(*httpAddr, "http", "127.0.0.1:8088"), SSHAddr: pick(*sshAddr, "ssh", ":2222"),
		TLSCert: pick(*cert, "tls_cert", ""), TLSKey: pick(*key, "tls_key", ""), TrustedProxies: tp,
		StateDir: pick("", "state", *state), PingEvery: *ping, OfflineAfter: *offline, Version: version,
	}
	// session limits: unset keeps the server defaults, "0" turns the limit off (stored as a negative value)
	dur := func(flagv, key string) time.Duration {
		v := pick(flagv, key, "")
		if v == "" {
			return 0
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", key, err)
			os.Exit(1)
		}
		if d == 0 {
			return -1
		}
		return d
	}
	cfg.SessionIdle, cfg.StallAfter = dur(*sessIdle, "session_idle"), dur(*stallAfter, "session_stall")
	cfg.Limits.ConnPerMin, cfg.Limits.ConcurrentPerIP, cfg.Limits.ConcurrentPerNode, cfg.Limits.BanFor = *connMin, *concIP, *concNode, 10*time.Minute
	rate := func(flagv, key string) limits.Rate {
		n, err := strconv.Atoi(pick(flagv, key, "0"))
		if err != nil || n <= 0 {
			return limits.Rate{}
		}
		return limits.Rate{PerMin: n, Burst: max(n/3, 1)}
	}
	cfg.Limits.SSH = rate(*sshMin, "limit_ssh_per_min")
	cfg.Limits.Tunnel = rate(*tunMin, "limit_tunnel_per_min")
	cfg.Limits.API = rate(*apiMin, "limit_api_per_min")
	log := blog.New(os.Stdout)
	srv, err := server.New(cfg, st, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if isTrue(pick(*panelOn, "panel", "true")) {
		srv.SetPanel(panel.New(panel.Config{SecureCookie: isTrue(pick(*secure, "panel_secure_cookie", "true"))}, st, srv))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := srv.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// admin set-password: reads the password from the terminal (no echo) or stdin; never from argv.
func admin(args []string) {
	if len(args) >= 1 && args[0] == "2fa" {
		admin2FA(args[1:])
		return
	}
	if len(args) < 1 || args[0] != "set-password" {
		usage()
	}
	fs := flag.NewFlagSet("admin", flag.ExitOnError)
	state := stateFlag(fs)
	user := fs.String("user", "admin", "admin username")
	fs.Parse(args[1:])
	if !proto.ValidUser(*user) {
		fmt.Fprintln(os.Stderr, "error: username must match ^[a-z_][a-z0-9_-]{0,31}$")
		os.Exit(1)
	}
	pw := readSecret("New password: ")
	if term.IsTerminal(int(os.Stdin.Fd())) && readSecret("Repeat password: ") != pw {
		fmt.Fprintln(os.Stderr, "error: passwords do not match")
		os.Exit(1)
	}
	if err := panel.ValidatePassword(*user, pw); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	st := openStore(*state)
	defer st.Close()
	if err := st.AdminSetPassword(*user, pw); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	st.Audit("cli", "local", "admin.set-password", *user)
	fmt.Printf("password set for %s (existing panel sessions of this user were signed out)\n", *user)
}

func readSecret(prompt string) string {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		b, _ := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b)
	}
	r := bufio.NewReader(os.Stdin)
	l, _ := r.ReadString('\n')
	return strings.TrimRight(l, "\r\n")
}

func token(args []string) {
	if len(args) < 1 {
		usage()
	}
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	state := stateFlag(fs)
	label := fs.String("label", "", "label")
	ttl := fs.Duration("ttl", 0, "lifetime (0 = no expiry)")
	uses := fs.Int("uses", 0, "max uses (0 = unlimited)")
	fs.Parse(args[1:])
	st := openStore(*state)
	defer st.Close()
	switch args[0] {
	case "create":
		id, tok, err := st.TokenCreate(*label, *ttl, *uses)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "token %s created (shown once; only a hash is stored):\n", id)
		fmt.Println(tok)
	case "ls":
		ts, _ := st.TokenList()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tLABEL\tUSES\tMAX\tEXPIRES\tREVOKED")
		for _, t := range ts {
			mx, ex, rv := "-", "-", "-"
			if t.MaxUses.Valid {
				mx = fmt.Sprint(t.MaxUses.Int64)
			}
			if t.ExpiresAt.Valid {
				ex = time.Unix(t.ExpiresAt.Int64, 0).Format(time.RFC3339)
			}
			if t.Revoked.Valid {
				rv = "yes"
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n", t.ID, t.Label, t.Uses, mx, ex, rv)
		}
		w.Flush()
	case "revoke":
		if fs.NArg() < 1 {
			usage()
		}
		if err := st.TokenRevoke(fs.Arg(0)); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Println("revoked")
	default:
		usage()
	}
}

func node(args []string) {
	if len(args) < 1 {
		usage()
	}
	fs := flag.NewFlagSet("node", flag.ExitOnError)
	state := stateFlag(fs)
	reason := fs.String("reason", "", "ban reason")
	// allow flags after the name
	var pos []string
	rest := args[1:]
	for len(rest) > 0 {
		fs.Parse(rest)
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	st := openStore(*state)
	defer st.Close()
	need := func() string {
		if len(pos) < 1 {
			usage()
		}
		return pos[0]
	}
	switch args[0] {
	case "ls":
		ns, _ := st.Nodes()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSTATUS\tOS\tVERSION\tLAST-SEEN")
		for _, n := range ns {
			fmt.Fprintf(w, "%s\t%s\t%s/%s\t%s\t%s\n", n.Name, n.Status, n.OS, n.Arch, n.Version, time.Unix(n.LastSeen, 0).Format(time.RFC3339))
		}
		w.Flush()
	case "ban":
		n, err := st.NodeByName(need())
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: no such node")
			os.Exit(1)
		}
		st.BanNode(n, *reason)
		st.Audit("cli", "local", "node.ban", n.Name)
		fmt.Printf("node %s banned; the running broker closes its tunnel within 2s\n", n.Name)
	case "unban":
		name := need()
		st.UnbanNode(name)
		st.Audit("cli", "local", "node.unban", name)
		fmt.Println("unbanned")
	case "approve":
		n, err := st.NodeByName(need())
		if err != nil || st.Approve(n.ID) != nil {
			fmt.Fprintln(os.Stderr, "error: no such pending node")
			os.Exit(1)
		}
		st.Audit("cli", "local", "node.approve", n.Name)
		fmt.Printf("node %s approved\n", n.Name)
	case "release":
		n, err := st.NodeByName(need())
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: no such node")
			os.Exit(1)
		}
		st.Release(n.ID)
		fmt.Printf("name %s released\n", n.Name)
	default:
		usage()
	}
}
