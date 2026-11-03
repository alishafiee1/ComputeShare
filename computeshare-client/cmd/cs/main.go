// cs: ComputeShare client helper for users and bots (Apache-2.0). Needs no root (node 002 R6).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/client/knownhosts"
	"github.com/alishafiee1/computeshare-client/internal/client/sshconfig"
	"github.com/alishafiee1/computeshare-client/internal/client/trust"
	"github.com/alishafiee1/computeshare-client/proto"
	"golang.org/x/crypto/ssh"
)

var version = "0.2.0-dev"

// DefaultServer is the public broker; `cs add` lines omit --server for it (client 017 R1).
const DefaultServer = "node.n3xus.ir:2222"

type Config struct {
	Server string `json:"server"` // ssh host[:port] of the broker jump
	API    string `json:"api"`    // https://host (GET /v1/nodes)
	User   string `json:"user"`
	// APIHost is the DNS name the HTTPS certificate must match when API uses an IP (client 022 R3).
	APIHost string `json:"api_host,omitempty"`
	// Identity is the private key `cs setup-ssh` writes as IdentityFile (for keys not named id_ed25519, id_rsa ...).
	Identity string `json:"identity,omitempty"`
}

func cfgDir() string {
	if d := os.Getenv("CS_CONFIG_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "cs")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "cs")
}

func sshDir() string {
	if d := os.Getenv("CS_SSH_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".ssh")
}

func load() Config {
	c, err := loadOpt()
	if err != nil {
		die(1, "run: cs setup   (or: cs init --server <address>)")
	}
	return c
}

// loadOpt reads the config and reports a missing file as an error instead of exiting.
func loadOpt() (Config, error) {
	var c Config
	b, err := os.ReadFile(filepath.Join(cfgDir(), "config.json"))
	if err != nil {
		return c, err
	}
	json.Unmarshal(b, &c)
	if c.User == "" {
		c.User = "user"
	}
	return c, nil
}

func die(code int, f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(code)
}

func usage() {
	fmt.Fprint(os.Stderr, `cs `+version+` — ComputeShare client
  cs setup [--server host:port] [--server-host name]   (no invite needed: init + setup-ssh + known-hosts in one go)
  cs add <name> <SHA256:...> [--server host:port]   (the line a node owner gives you: sets up everything)
  cs init [--server node.n3xus.ir:2222] [--api https://node.n3xus.ir] [--user user] [--server-host name]
  cs setup-ssh [--identity ~/.ssh/key]
  cs nodes [--json]
  cs known-hosts
  cs trust <name> <SHA256:...>
  cs ssh <name> [-- cmd...]
  cs scp <src> <dst>        (use <name>.cs:path for remote paths)
  cs proxy <name>           (stdio, for ProxyCommand)
  cs bench <name> [--run] [--json]   (node facts and the last test; --run tests CPU/RAM/disk/GPU in your space, max once per 10 min)
  cs release keygen [--dir d]            (once, on the owner's laptop: release.key + release.pub)
  cs release sign <file> [--key f]       (writes <file>.sig, e.g. SHA256SUMS.sig or images.lock.sig)
  cs release verify <file> [--pub f]     (checks <file>.sig; default key = the one built into cs)
`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	a := os.Args[2:]
	switch os.Args[1] {
	case "init":
		fs := flag.NewFlagSet("init", flag.ExitOnError)
		srv := fs.String("server", DefaultServer, "broker SSH address host[:port] (port 2222 if omitted)")
		api := fs.String("api", "", "broker HTTPS URL (default https://<host>)")
		user := fs.String("user", "user", "default space user")
		host := fs.String("server-host", "", "DNS name for the TLS certificate when --server is an IP")
		fs.Parse(a)
		if *srv == "" {
			usage()
		}
		initCfg(*srv, *api, *user, *host)
	case "setup":
		setupCmd(a)
	case "add":
		add(a)
	case "release":
		releaseCmd(a)
	case "setup-ssh":
		fs := flag.NewFlagSet("setup-ssh", flag.ExitOnError)
		id := fs.String("identity", "", "private key file ssh should use for *.cs (needed when it is not a default name)")
		fs.Parse(a)
		saveIdentity(*id)
		setupSSH(load())
		fmt.Println("ssh config updated; next: cs known-hosts, then ssh <name>.cs")
	case "nodes":
		rows := fetch(load())
		if len(a) > 0 && a[0] == "--json" {
			json.NewEncoder(os.Stdout).Encode(rows)
			return
		}
		for _, r := range rows {
			fmt.Printf("%-20s %-8s os=%s gpu=%s  ssh %s.cs\n", r.Name, r.Status, r.OS, r.Specs.GPU, r.Name)
		}
	case "known-hosts":
		if knownHosts(load()) > 0 {
			os.Exit(1)
		}
	case "trust":
		if len(a) != 2 {
			usage()
		}
		if !validFingerprint(a[1]) {
			die(2, "%s", badFingerprint)
		}
		ts := trust.Load(cfgDir())
		ts.Trust(a[0], a[1])
		ts.Save()
		fmt.Printf("trusted %s = %s (run cs known-hosts)\n", a[0], a[1])
	case "ssh":
		if len(a) < 1 {
			usage()
		}
		c := load()
		if trust.Load(cfgDir()).Fingerprint(a[0]) == "" {
			fmt.Fprintln(os.Stderr, tofuWarning(a[0]))
		}
		args := append(sshBase(c), c.User+"@"+strings.ToLower(a[0])+".cs")
		if len(a) > 1 {
			rest := a[1:]
			if rest[0] == "--" {
				rest = rest[1:]
			}
			args = append(args, rest...)
		}
		run("ssh", args)
	case "scp":
		if len(a) != 2 {
			usage()
		}
		c := load()
		fix := func(s string) string {
			if h, p, ok := strings.Cut(s, ":"); ok && strings.HasSuffix(strings.ToLower(h), ".cs") && !strings.Contains(h, "@") {
				return c.User + "@" + strings.ToLower(h) + ":" + p
			}
			return s
		}
		run("scp", append(sshBase(c), fix(a[0]), fix(a[1])))
	case "bench":
		benchCmd(a)
	case "proxy":
		if len(a) != 1 {
			usage()
		}
		proxy(load(), a[0])
	case "version":
		fmt.Println("cs", version)
	default:
		usage()
	}
}

func jumpArgs(c Config) (host, port string) {
	host, port = c.Server, "22"
	if h, p, ok := strings.Cut(c.Server, ":"); ok {
		host, port = h, p
	}
	return
}

func sshBase(c Config) []string {
	h, p := jumpArgs(c)
	self, _ := os.Executable()
	_ = self
	args := []string{
		"-o", fmt.Sprintf("ProxyCommand=ssh -p %s -o StrictHostKeyChecking=accept-new -W %%n:22 join@%s", p, h),
		"-o", "UserKnownHostsFile=" + filepath.Join(sshDir(), "cs_known_hosts"),
		"-o", "StrictHostKeyChecking=yes",
	}
	if sshconfig.Mux(runtime.GOOS) {
		for _, o := range sshconfig.MuxOptions {
			args = append(args, "-o", strings.Replace(o, " ", "=", 1))
		}
	}
	return args
}

func initCfg(srv, api, user, apiHost string) {
	if _, _, err := net.SplitHostPort(srv); err != nil {
		srv = net.JoinHostPort(srv, "2222") // v2: all SSH goes through port 2222
	}
	if api == "" {
		h, _, _ := net.SplitHostPort(srv)
		api = "https://" + h
	}
	if isIPHost(srv) && apiHost == "" {
		fmt.Fprintln(os.Stderr, "server given as an IP: add --server-host <name> so the certificate can be checked")
	}
	os.MkdirAll(cfgDir(), 0o700)
	b, _ := json.MarshalIndent(Config{Server: srv, API: api, User: user, APIHost: apiHost}, "", "  ")
	os.WriteFile(filepath.Join(cfgDir(), "config.json"), b, 0o600)
	fmt.Println("saved", filepath.Join(cfgDir(), "config.json"))
}

// saveIdentity remembers the key path in config.json (an empty path keeps what is there).
func saveIdentity(path string) {
	if path == "" {
		return
	}
	c := load()
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if _, err := os.Stat(path); err != nil {
		die(1, "identity file not found: %s", path)
	}
	c.Identity = filepath.ToSlash(path)
	b, _ := json.MarshalIndent(c, "", "  ")
	os.WriteFile(filepath.Join(cfgDir(), "config.json"), b, 0o600)
}

func setupSSH(c Config) {
	w, err := sshconfig.Apply(filepath.Join(sshDir(), "config"), sshconfig.BlockWith(c.Server, c.User, sshconfig.Mux(runtime.GOOS), c.Identity))
	if err != nil {
		die(1, "%v", err)
	}
	if w != "" {
		fmt.Fprintln(os.Stderr, w)
	}
}

// add runs the whole first-time flow from an owner's invite line (client 017 R2). Idempotent.
func add(a []string) {
	var pos []string
	srv := ""
	for i := 0; i < len(a); i++ {
		if a[i] == "--server" && i+1 < len(a) {
			srv = a[i+1]
			i++
		} else if v, ok := strings.CutPrefix(a[i], "--server="); ok {
			srv = v
		} else {
			pos = append(pos, a[i])
		}
	}
	if len(pos) != 2 {
		usage()
	}
	if !validFingerprint(pos[1]) {
		die(2, "%s", badFingerprint)
	}
	name, fp := pos[0], pos[1]
	if _, err := os.Stat(filepath.Join(cfgDir(), "config.json")); err != nil {
		if srv == "" {
			srv = DefaultServer
		}
		initCfg(srv, "", "user", "")
	}
	c := load()
	if srv != "" && !sameServer(srv, c.Server) {
		die(1, "this cs is set up for %s, but the invite is for %s; run: cs init --server %s", c.Server, srv, srv)
	}
	setupSSH(c)
	ts := trust.Load(cfgDir())
	ts.Trust(name, fp)
	ts.Save()
	if knownHosts(c) > 0 {
		os.Exit(1)
	}
	fmt.Printf("ready: ssh %s.cs\n", strings.ToLower(name))
}

func sameServer(a, b string) bool {
	if _, _, err := net.SplitHostPort(a); err != nil {
		a = net.JoinHostPort(a, "2222")
	}
	return strings.EqualFold(a, b)
}

func tofuWarning(name string) string {
	return fmt.Sprintf("warning: %s is trusted only on the server's word; ask the owner for 'cs add' line", name)
}

func run(bin string, args []string) {
	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		die(1, "%v", err)
	}
}

func fetch(c Config) []proto.NodeRow {
	req, err := nodesRequest(c)
	if err != nil {
		die(3, "broker unreachable: %v", err)
	}
	resp, err := httpClient(c).Do(req)
	if err != nil {
		die(3, "broker unreachable: %v", err)
	}
	defer resp.Body.Close()
	if msg := statusMessage(resp.StatusCode); msg != "" {
		die(3, "%s", msg)
	}
	var rows []proto.NodeRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		die(1, "bad response: %v", err)
	}
	return rows
}

// statusMessage explains a non-200 /v1/nodes answer (client 017 R5); "" means OK.
func statusMessage(code int) string {
	switch {
	case code == 200:
		return ""
	case code == 429:
		return "rate limited by the server; wait a minute"
	case code == 403:
		return "your IP is temporarily blocked by the server"
	}
	return fmt.Sprintf("server error %d", code)
}

// knownHosts rewrites cs_known_hosts and returns the number of problems printed.
func knownHosts(c Config) int {
	rows := fetch(c)
	ts := trust.Load(cfgDir())
	path := filepath.Join(sshDir(), "cs_known_hosts")
	old, _ := os.ReadFile(path)
	res := knownhosts.Build(rows, ts, knownhosts.Parse(string(old)))
	for _, p := range res.Problems {
		fmt.Fprintln(os.Stderr, p)
	}
	ts.Save()
	os.MkdirAll(sshDir(), 0o700)
	content := "# managed by cs known-hosts; only device-signed keys\n" + strings.Join(res.Lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		die(1, "%v", err)
	}
	fmt.Printf("%d host keys written to %s\n", len(res.Lines), path)
	for _, n := range knownhosts.TOFUOnly(res.Lines, ts) {
		fmt.Fprintln(os.Stderr, tofuWarning(n)) // client 017 R3: never blocks
	}
	return len(res.Problems)
}

// proxy dials the jump with user `join` and opens direct-tcpip to <name>:22, piping stdio.
// The broker's own host key is not pinned here: end-to-end trust comes from the inner
// SSH host key verified against cs_known_hosts.
func proxy(c Config, name string) {
	h, p := jumpArgs(c)
	cl, err := ssh.Dial("tcp", net.JoinHostPort(h, p), &ssh.ClientConfig{User: "join", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 15 * time.Second})
	if err != nil {
		die(3, "jump: %v", err)
	}
	conn, err := cl.Dial("tcp", net.JoinHostPort(strings.TrimSuffix(name, ".cs"), "22"))
	if err != nil {
		die(1, "%v", err)
	}
	go func() { io.Copy(conn, os.Stdin); conn.Close() }()
	io.Copy(os.Stdout, conn)
}
