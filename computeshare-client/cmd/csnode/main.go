//go:build !windows

// csnode: ComputeShare node agent and owner CLI (Apache-2.0). See rodo/computeshare-client.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/alishafiee1/computeshare-client/internal/node/gpu"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/alishafiee1/computeshare-client/internal/node/approval"
	"github.com/alishafiee1/computeshare-client/internal/node/config"
	"github.com/alishafiee1/computeshare-client/internal/node/control"
	"github.com/alishafiee1/computeshare-client/internal/node/gateway"
	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/internal/node/register"
	"github.com/alishafiee1/computeshare-client/internal/node/setup"
	"github.com/alishafiee1/computeshare-client/internal/node/users"
	"github.com/alishafiee1/computeshare-client/proto"
	"golang.org/x/term"
)

var version = "0.2.0-dev"

// Exit codes (cli.md).
const (
	exitOK = iota
	exitErr
	exitUsage
	exitUnreachable
	exitNameTaken
	exitToken
)

func die(code int, f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(code)
}

func usage() {
	fmt.Fprint(os.Stderr, `csnode `+version+` — ComputeShare node
  setup [flags]                      interactive install (or non-interactive with --yes and flags)
  join --server URL --token T --name N   register an existing space
  run                                run the tunnel agent (used by the service)
  status | resources | doctor | who
  stop | start | share on|off  (pause/resume = share off/on)
  panel                              print the local web panel link (127.0.0.1:7777)
  kick <session-id|client-ip>        disconnect a live session now
  block add <ip|cidr> [--for 24h] [--reason text] | block ls | block rm <ip|cidr>   refuse a client address (and end its live sessions)
  bandwidth <20Mbit|off> | quota <20GB/day|off>
  limits set [--cpu N] [--memory 16GiB] [--disk 100GiB] [--gpu on|off]
  config approval on|off | pending | approve <id> [--once] | deny <id>
  keys add <label> <pubkey> | keys ls | keys remove <label>
  users add|ls|passwd|keys|disable|enable|remove|migrate   per-user spaces (csnode users for help)
  passwd
  leave [--purge]
  service install|uninstall
  version
`)
	os.Exit(exitUsage)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "setup":
		cmdSetup(args)
	case "join":
		cmdJoin(args)
	case "run":
		cmdRun(args)
	case "status":
		cmdStatus()
	case "resources":
		cmdResources()
	case "doctor":
		cmdDoctor(args)
	case "who":
		cmdWho()
	case "stop":
		cmdStop()
	case "start":
		cmdStart()
	case "pause", "resume":
		cmdShare([]string{map[string]string{"pause": "off", "resume": "on"}[cmd]})
	case "share":
		cmdShare(os.Args[2:])
	case "kick":
		cmdKick(os.Args[2:])
	case "block":
		cmdBlock(args)
	case "panel":
		cmdPanel()
	case "bandwidth":
		cmdBandwidth(args)
	case "quota":
		cmdQuota(args)
	case "limits":
		cmdLimits(args)
	case "config":
		cmdConfig(args)
	case "pending":
		cmdPending()
	case "approve":
		cmdApprove(args)
	case "deny":
		if len(args) < 1 {
			usage()
		}
		if err := approval.Deny(args[0]); err != nil {
			die(exitErr, "%v", err)
		}
		if strings.HasPrefix(args[0], "s") {
			setup.Decide(mustCfg().Name, args[0], "deny")
		}
		fmt.Println("denied")
	case "keys":
		cmdKeys(args)
	case "users":
		cmdUsers(args)
	case "relay":
		cmdRelay(args)
	case "passwd":
		cmdPasswd()
	case "leave":
		cmdLeave(args)
	case "service":
		cmdService(args)
	case "version", "--version":
		fmt.Println("csnode", version, "proto", proto.Version)
	default:
		usage()
	}
}

func mustCfg() *config.Config {
	c, err := config.Load()
	if err != nil {
		die(exitErr, "no config in %s (run csnode setup): %v", config.Dir(), err)
	}
	return c
}

var stdin = bufio.NewReader(os.Stdin)

func ask(q, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", q, def)
	} else {
		fmt.Printf("%s: ", q)
	}
	l, _ := stdin.ReadString('\n')
	l = strings.TrimSpace(l)
	if l == "" {
		return def
	}
	return l
}

// askOptional shows a suggestion but an empty answer means "no limit" (R2 / 005 R1).
func askOptional(q, suggest string) string {
	fmt.Printf("%s (suggested %s, empty = unlimited): ", q, suggest)
	l, _ := stdin.ReadString('\n')
	return strings.TrimSpace(l)
}

func askSecret(q string) string {
	fmt.Print(q + ": ")
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, _ := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return string(b)
	}
	l, _ := stdin.ReadString('\n')
	return strings.TrimRight(l, "\r\n")
}

const consent = `ComputeShare will create an isolated Linux space (Incus container) on this computer.
People who receive the space user's password or key from you can log in to that space
over the network and use the CPU/RAM/GPU you allow. They can NOT reach your files,
your home network or this host. You can stop it any time with "csnode stop".
Type "yes" to continue`

func cmdSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	yes := fs.Bool("yes", false, "non-interactive: accept consent and use flags")
	name := fs.String("name", "", "node name")
	user := fs.String("user", "user", "space user (sudo)")
	pwStdin := fs.Bool("password-stdin", false, "read space password from stdin")
	key := fs.String("key", "", "public key (authorized_keys line) for the space user")
	server := fs.String("server", "", "broker URL, e.g. https://cs.example.ir")
	token := fs.String("token", "", "join token")
	cpu := fs.String("cpu", "", "CPU limit (cores), empty = no limit")
	mem := fs.String("memory", "", "memory limit, e.g. 8GiB")
	disk := fs.String("disk", "", "disk size, e.g. 100GiB")
	gpu := fs.Bool("gpu", false, "pass the GPU into the space")
	bw := fs.String("bandwidth", "", "bandwidth limit, e.g. 20Mbit (empty = unlimited)")
	q := fs.String("quota", "", "daily quota, e.g. 20GB/day (empty = unlimited)")
	image := fs.String("image", "images:debian/12", "Incus image")
	mirror := fs.String("mirror", "auto", "mirror base URL (auto = <server>/mirror if present, off = upstream)")
	imgFP := fs.String("image-fingerprint", "", "use this exact image fingerprint from the mirror (for images not in the signed images.lock)")
	ca := fs.String("ca", "", "pinned CA file for the broker")
	fs.Parse(args)
	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		die(exitErr, "csnode setup must run as root (sudo csnode setup)")
	}
	out := func(s string) { fmt.Println(s) }
	if !*yes {
		if ask(consent, "") != "yes" {
			die(exitErr, "aborted")
		}
	}
	m, err := setup.ResolveMirror(*mirror, *server, out)
	if err != nil {
		die(exitErr, "%v", err)
	}
	*mirror = m
	if err := setup.EnsureIncus(*mirror, out); err != nil {
		die(exitErr, "%v", err)
	}
	res := setup.Detect()
	fmt.Print(res.Table())
	if !*yes {
		*cpu = askOptional("CPU cores for the space", res.SuggestCPU())
		*mem = askOptional("Memory for the space", res.SuggestMemory())
		*disk = askOptional("Disk for the space", "100GiB")
		if res.GPU != "" {
			*gpu = strings.HasPrefix(strings.ToLower(ask("Share GPU "+res.GPU+"? (y/N)", "n")), "y")
		}
	}
	if *server == "" {
		*server = ask("Server address", config.DefaultServer)
	}
	hc := register.HTTPClient(*ca)
	for {
		if *name == "" {
			*name = ask("Node name", "")
		}
		if !proto.ValidName(*name) {
			fmt.Println("name must match ^[a-zA-Z][a-zA-Z0-9-]{2,31}$")
		} else if ok, err := register.NameAvailable(hc, *server, *name); err != nil {
			if errors.Is(err, register.ErrUnreachable) {
				die(exitUnreachable, "broker unreachable: %v", err)
			}
			fmt.Println(err)
		} else if !ok {
			fmt.Println(proto.NameTakenMessage(*name))
			if *yes {
				os.Exit(exitNameTaken)
			}
		} else {
			break
		}
		*name = ""
	}
	if !*yes {
		*user = ask("Space user (gets sudo)", *user)
	}
	var pw string
	if *pwStdin {
		b, _ := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		pw = strings.TrimRight(string(b), "\r\n")
		if err := setup.CheckPassword(pw); err != nil {
			die(exitErr, "%v", err)
		}
	} else if !*yes {
		for {
			pw = askSecret("Space password (min 12, empty = keys only)")
			if err := setup.CheckPassword(pw); err != nil {
				fmt.Println(err)
				continue
			}
			if pw != "" && askSecret("Repeat password") != pw {
				fmt.Println("passwords do not match")
				continue
			}
			break
		}
		if *key == "" {
			*key = ask("Public key for the user (optional)", "")
		}
	}
	if *token == "" {
		*token = askSecret("Join token")
	}
	if !*yes {
		*bw = askOptional("Internet bandwidth limit for the space", "20Mbit")
		*q = askOptional("Daily data quota", "20GB/day")
	}
	if _, err := proto.ParseBandwidth(*bw); err != nil {
		die(exitErr, "%v", err)
	}
	if _, err := proto.ParseQuota(*q); err != nil {
		die(exitErr, "%v", err)
	}
	if _, err := config.DeviceKey(); err != nil {
		die(exitErr, "device key: %v", err)
	}
	gw, err := setup.EnsureNetwork()
	if err != nil {
		die(exitErr, "network: %v", err)
	}
	var keys []string
	if *key != "" {
		keys = append(keys, *key)
	}
	hasPw := pw != ""
	if *imgFP != "" {
		out("using image fingerprint " + *imgFP + " chosen by the owner (--image-fingerprint)")
	}
	if img, ierr := setup.MirrorImage(*mirror, *image, *imgFP); ierr == nil {
		*image = img
	} else if errors.Is(ierr, setup.ErrImageTrust) {
		die(exitErr, "%v", ierr)
	} else {
		out("warning: mirror images unusable, using upstream: " + ierr.Error())
	}
	hk, err := setup.CreateSpace(setup.SpaceOpts{Mirror: *mirror, Name: *name, User: *user, Password: pw, Image: *image, Keys: keys,
		CPU: *cpu, Memory: *mem, Disk: *disk, GPU: *gpu, Bandwidth: *bw}, out)
	pw = ""
	if err != nil {
		die(exitErr, "%v", err)
	}
	c := &config.Config{Server: *server, CAFile: *ca, Name: *name, Mirror: *mirror,
		Space:  config.Space{Instance: "cs-" + *name, User: *user, Image: *image, ImageFingerprint: *imgFP, Gateway: gw, Mode: "container", PasswordAuth: hasPw},
		Limits: config.Limits{CPU: *cpu, Memory: *mem, Disk: *disk, GPU: *gpu, Bandwidth: *bw, Quota: *q}}
	c.Save()
	if code := doRegister(c, *token, hk, res); code != 0 {
		os.Exit(code)
	}
	fp, _ := proto.Fingerprint(hk)
	fmt.Printf("\nDone. Node %s registered.\nConnect with: ssh -J join@%s:2222 %s@%s   (or: cs setup-ssh, then ssh %s.cs)\nHost key fingerprint (share it with your users): %s\nInvite line for your users: %s\n", c.Name, config.JumpHost(c.Server), c.Space.User, c.Name, c.Name, fp, inviteLine(c.Name, fp, c.Server))
	fmt.Println("Start the tunnel with: csnode service install   (or: csnode run)")
}

func doRegister(c *config.Config, token, hostKey string, res setup.Resources) int {
	key, err := config.DeviceKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitErr
	}
	req := register.Build(key, c.Name, token, hostKey, res.Specs(c.Limits.GPU),
		proto.Limits{Bandwidth: c.Limits.Bandwidth, Quota: c.Limits.Quota}, runtime.GOOS, runtime.GOARCH, version)
	resp, err := register.Register(register.HTTPClient(c.CAFile), c.Server, req)
	if err != nil {
		var ae *register.APIError
		switch {
		case errors.Is(err, register.ErrUnreachable):
			fmt.Fprintln(os.Stderr, "broker unreachable; the space stays created, retry with: csnode join")
			return exitUnreachable
		case errors.As(err, &ae) && ae.Code == proto.CodeNameTaken:
			fmt.Fprintln(os.Stderr, ae.Message)
			return exitNameTaken
		case errors.As(err, &ae) && ae.Code == proto.CodeTokenInvalid:
			fmt.Fprintln(os.Stderr, "TOKEN_INVALID: token expired, revoked, or used up")
			return exitToken
		}
		fmt.Fprintln(os.Stderr, err)
		return exitErr
	}
	c.NodeID, c.RegisteredAt = resp.NodeID, req.RegisteredAt
	c.Save()
	return 0
}

func cmdJoin(args []string) {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	server := fs.String("server", "", "broker URL")
	token := fs.String("token", "", "join token (or env CS_TOKEN)")
	name := fs.String("name", "", "node name")
	hkFile := fs.String("host-key-file", "", "lab: space host public key file (when no Incus space)")
	target := fs.String("target", "", "lab only: stream destination host:port instead of <space-ip>:22")
	user := fs.String("user", "user", "space user")
	fs.Parse(args)
	c, err := config.Load()
	if err != nil {
		c = &config.Config{Space: config.Space{User: *user, Mode: "container"}}
	}
	if *server != "" {
		c.Server = *server
	}
	if *name != "" {
		c.Name = *name
		c.Space.Instance = "cs-" + *name
	}
	if *target != "" {
		c.Space.Target = *target
	}
	if *token == "" {
		*token = os.Getenv("CS_TOKEN")
	}
	if c.Server == "" || c.Name == "" || *token == "" {
		die(exitUsage, "usage: csnode join --server URL --token T --name N")
	}
	if !proto.ValidName(c.Name) {
		die(exitErr, "NAME_INVALID: name must match ^[a-zA-Z][a-zA-Z0-9-]{2,31}$")
	}
	hc := register.HTTPClient(c.CAFile)
	ok, err := register.NameAvailable(hc, c.Server, c.Name)
	if err != nil {
		if errors.Is(err, register.ErrUnreachable) {
			die(exitUnreachable, "broker unreachable: %v", err)
		}
		die(exitErr, "%v", err)
	}
	if !ok {
		die(exitNameTaken, "%s", proto.NameTakenMessage(c.Name))
	}
	var hk string
	if *hkFile != "" {
		b, err := os.ReadFile(*hkFile)
		if err != nil {
			die(exitErr, "%v", err)
		}
		f := strings.Fields(string(b))
		if len(f) < 2 {
			die(exitErr, "bad host key file")
		}
		hk = f[0] + " " + f[1]
	} else {
		hk, err = setup.HostKey(c.Name)
		if err != nil {
			die(exitErr, "cannot read space host key: %v", err)
		}
	}
	c.Save()
	if code := doRegister(c, *token, hk, setup.Detect()); code != 0 {
		os.Exit(code)
	}
	fp, _ := proto.Fingerprint(hk)
	fmt.Printf("registered %s, host key %s\n", c.Name, fp)
}

func logger() *slog.Logger { return slog.New(slog.NewJSONHandler(os.Stderr, nil)) }

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	ping := fs.Duration("ping", 15*time.Second, "ping interval")
	maxBack := fs.Duration("max-backoff", 30*time.Second, "max reconnect backoff (card 017 R3: a long outage is retried at least every 30 s)")
	fs.Parse(args)
	c := mustCfg()
	if c.NodeID == "" {
		die(exitErr, "node not registered; run csnode join")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a := newAgent(c, logger())
	if err := a.Run(ctx, *ping, *maxBack); err != nil {
		die(exitErr, "%v", err)
	}
}

// inviteLine is what the owner sends to users: one command that sets up cs, ssh and the
// pinned host key (client 017 R1). --server is left out for the default broker.
func inviteLine(name, fp, server string) string {
	l := "cs add " + name + " " + fp
	if h := config.JumpHost(server); h != "" && h != "node.n3xus.ir" {
		l += " --server " + h + ":2222"
	}
	return l
}

func cmdStatus() {
	c := mustCfg()
	st := config.LoadState()
	fmt.Printf("name:      %s\nserver:    %s\nssh:       ssh -J join@%s:2222 %s@%s\nregistered: %v\n", c.Name, c.Server, config.JumpHost(c.Server), c.Space.User, c.Name, c.NodeID != "")
	conn := "stopped"
	if !st.Stopped {
		conn = "running"
	}
	if st.Paused {
		conn += " (paused)"
	}
	if b, err := os.ReadFile(config.Path("online")); err == nil {
		conn += ", tunnel " + strings.TrimSpace(string(b))
	}
	fmt.Printf("agent:     %s\n", conn)
	if c.Gateway { // client 015: one node host key, one space per user
		if hk, err := gateway.LoadOrCreateHostKey(config.Path(gateway.HostKeyFile)); err == nil {
			fp, _ := proto.Fingerprint(gateway.AuthorizedHostKey(hk))
			fmt.Printf("host key:  %s\ninvite:    %s\n", fp, inviteLine(c.Name, fp, c.Server))
		}
		fmt.Printf("spaces:    one per user (%d users; csnode users ls)\n", len(users.Load().Users))
	} else if incus.Available() && incus.Exists(c.Space.Instance) {
		s, _ := incus.GetState(c.Space.Instance)
		ip, _ := incus.IPv4(c.Space.Instance)
		fmt.Printf("space:     %s %s ip=%s user=%s\n", c.Space.Instance, strings.ToUpper(s.Status), ip, c.Space.User)
		if hk, err := setup.HostKey(c.Name); err == nil {
			fp, _ := proto.Fingerprint(hk)
			fmt.Printf("host key:  %s\n", fp)
			fmt.Printf("invite:    %s\n", inviteLine(c.Name, fp, c.Server))
		}
	} else {
		fmt.Printf("space:     %s (not found)\n", c.Space.Instance)
	}
	show := func(v string) string {
		if v == "" {
			return "unlimited"
		}
		return v
	}
	fmt.Printf("limits:    cpu=%s memory=%s disk=%s gpu=%v\n", show(c.Limits.CPU), show(c.Limits.Memory), show(c.Limits.Disk), c.Limits.GPU)
	fmt.Printf("bandwidth: %s\nquota:     %s\n", show(c.Limits.Bandwidth), show(c.Limits.Quota))
	q := loadQuota()
	if q.Day != "" {
		fmt.Printf("usage:     %.3f GB today (%s)\n", float64(q.Used)/1e9, q.Day)
	}
	if q.Reached {
		fmt.Println("quota reached")
	}
	fmt.Printf("approval:  %s\n", map[bool]string{true: "on", false: "off"}[c.Approval])
}

func cmdResources() {
	r := setup.Detect()
	fmt.Print(r.Table())
	if c, err := config.Load(); err == nil {
		fmt.Printf("allowed:  cpu=%s memory=%s disk=%s gpu=%v\n", c.Limits.CPU, c.Limits.Memory, c.Limits.Disk, c.Limits.GPU)
	}
}

func cmdDoctor(args []string) {
	ok := func(b bool) string { return map[bool]string{true: "ok", false: "FAIL"}[b] }
	if len(args) == 2 && args[0] == "--model" { // broker 005 R6
		bad, err := setup.VerifyModel(args[1])
		if err != nil {
			die(exitErr, "model: %v", err)
		}
		if len(bad) > 0 {
			die(exitErr, "model FAIL: %s", strings.Join(bad, ", "))
		}
		fmt.Println("model: ok")
		return
	}
	fmt.Println("incus:", ok(incus.Available()))
	if fixed, err := setup.FixAptSource(); fixed || err != nil { // client 014: old trusted=yes source
		fmt.Println("apt source: removed [trusted=yes] from", setup.AptSourceFile, ok(err == nil))
	}
	if e := gpu.Probe(); e.Driver || e.WSL {
		if _, _, err := gpu.Plan(e, 0); err != nil {
			fmt.Println("gpu: FAIL", err)
		} else {
			fmt.Println("gpu: ok", gpu.Info(0))
		}
	}
	c, err := config.Load()
	if err != nil {
		fmt.Println("config: FAIL", err)
		return
	}
	if incus.Available() {
		_, nerr := incus.Run(nil, "network", "show", setup.Bridge)
		_, aerr := incus.Run(nil, "network", "acl", "show", setup.ACL)
		fmt.Println("network csbr0:", ok(nerr == nil), " acl csacl:", ok(aerr == nil))
		fmt.Println("space:", ok(incus.Exists(c.Space.Instance)))
		// umbrella card 016: say plainly when a limit is set but not enforced or not visible in the space
		drv := setup.StorageDriver()
		fmt.Println("storage pool driver:", orDash(drv))
		if c.Limits.Disk != "" && !setup.DiskEnforced() {
			fmt.Printf("limits: WARN disk limit %s is NOT enforced: the pool driver %q has no quotas, so the space can fill the whole disk. Use a btrfs, zfs or lvm pool (create one with `incus storage create` and move the root disk with the owner's consent).\n", c.Limits.Disk, drv)
		}
		if !setup.LXCFSActive() {
			fmt.Println("limits: WARN lxcfs is missing: `free`, `nproc` and `df` inside the space show the whole computer, not the shared limits. Fix: sudo apt-get install lxcfs, then sudo csnode stop && sudo csnode start")
		}
	}
	_, err = register.NameAvailable(register.HTTPClient(c.CAFile), c.Server, c.Name)
	fmt.Println("broker reachable:", ok(err == nil || !errors.Is(err, register.ErrUnreachable)))
	if msg := setup.PowerAdvice(); msg != "" {
		fmt.Println("sleep: WARN", msg) // umbrella card 017 R5
	}
	fmt.Println("gpu:", setup.Detect().GPU)
}

func cmdStop() {
	c := mustCfg()
	st := config.LoadState()
	st.Stopped = true
	st.Save()
	t := time.Now()
	if incus.Available() && incus.Exists(c.Space.Instance) {
		if err := incus.Stop(c.Space.Instance, true); err != nil {
			die(exitErr, "%v", err)
		}
	}
	fmt.Printf("stopped in %.1fs; tunnel closes and new connections are refused (csnode start to undo)\n", time.Since(t).Seconds())
}

func cmdStart() {
	c := mustCfg()
	st := config.LoadState()
	st.Stopped = false
	st.Save()
	if incus.Available() && incus.Exists(c.Space.Instance) {
		incus.Start(c.Space.Instance)
	}
	fmt.Println("started")
}

func cmdBandwidth(args []string) {
	if len(args) != 1 {
		usage()
	}
	applyLimits(control.Limits{Bandwidth: &args[0]})
	fmt.Println("bandwidth:", orUnlimited(mustCfg().Limits.Bandwidth))
}

func cmdQuota(args []string) {
	if len(args) != 1 {
		usage()
	}
	applyLimits(control.Limits{Quota: &args[0]})
	fmt.Println("quota:", orUnlimited(mustCfg().Limits.Quota), "(applies immediately against today's counter)")
}

func orUnlimited(v string) string {
	if v == "" {
		return "unlimited"
	}
	return v
}

func applyLimits(l control.Limits) {
	c := mustCfg()
	exists := incus.Available() && incus.Exists(c.Space.Instance)
	if err := control.ApplyLimits(c, l, control.Default, exists); err != nil {
		die(exitErr, "%v", err)
	}
}

func cmdLimits(args []string) {
	if len(args) < 1 || args[0] != "set" {
		usage()
	}
	fs := flag.NewFlagSet("limits", flag.ExitOnError)
	cpu := fs.String("cpu", "\x00", "")
	mem := fs.String("memory", "\x00", "")
	disk := fs.String("disk", "\x00", "")
	gpuFlag := fs.String("gpu", "", "on|off")
	fs.Parse(args[1:])
	var l control.Limits
	if *cpu != "\x00" {
		l.CPU = cpu
	}
	if *mem != "\x00" {
		l.Memory = mem
	}
	applyLimits(l)
	c := mustCfg()
	inst := c.Space.Instance
	if *disk != "\x00" {
		c.Limits.Disk = *disk
		incus.Run(nil, "config", "device", "set", inst, "root", "size="+*disk)
	}
	if *gpuFlag == "on" && !c.Limits.GPU {
		if err := gpu.Enable(inst, gpu.Probe(), 0); err != nil {
			die(1, "%v", err)
		}
		c.Limits.GPU = true
	} else if *gpuFlag == "off" && c.Limits.GPU {
		gpu.Disable(inst)
		c.Limits.GPU = false
	}
	c.Save()
	fmt.Println("limits updated")
}

func cmdConfig(args []string) {
	if len(args) != 2 || args[0] != "approval" || (args[1] != "on" && args[1] != "off") {
		usage()
	}
	c := mustCfg()
	on := args[1] == "on"
	if incus.Available() && incus.Exists(c.Space.Instance) {
		if on {
			if err := setup.InstallAuthKeys(c.Name); err != nil {
				die(exitErr, "%v", err)
			}
			setup.PushApproved(c.Name, approval.Load().Keys)
		}
		if err := setup.WriteSSHDConfig(c.Name, c.Space.PasswordAuth, on); err != nil {
			die(exitErr, "%v", err)
		}
	}
	c.Approval = on
	c.Save()
	fmt.Println("approval:", args[1])
}

func cmdPending() {
	c := mustCfg()
	if !c.Gateway && incus.Available() && incus.Exists(c.Space.Instance) {
		setup.CollectPending(c.Name)
	}
	d := approval.Load()
	if len(d.Pending) == 0 {
		fmt.Println("no pending requests")
		return
	}
	fmt.Printf("%-8s %-8s %-12s %-52s %s\n", "ID", "KIND", "USER", "FINGERPRINT/SRC-IP", "AT")
	for _, p := range d.Pending {
		w := p.Fingerprint
		if p.Kind == "session" {
			w = p.SrcIP
		} else if p.SrcIP != "" {
			w += " " + p.SrcIP
		}
		u := p.User
		if u == "" {
			u = "-"
		}
		fmt.Printf("%-8s %-8s %-12s %-52s %s\n", p.ID, p.Kind, u, w, p.At.Format(time.RFC3339))
	}
}

func cmdApprove(args []string) {
	once := false
	var id string
	for _, a := range args {
		if a == "--once" {
			once = true
		} else {
			id = a
		}
	}
	if id == "" {
		usage()
	}
	p, err := approval.Approve(id, once)
	if err != nil {
		die(exitErr, "%v", err)
	}
	c := mustCfg()
	if p.User != "" { // per-user gateway login (015 R9): the gateway is waiting for this answer
		if p.Kind == "key" {
			must(users.Update(func(s *users.Store) error {
				if u, ok := s.Get(p.User); ok {
					u.ApproveKey(p.Fingerprint)
				}
				return nil
			}))
		}
		fmt.Println("approved", id)
		return
	}
	if p.Kind == "key" && incus.Available() {
		setup.PushApproved(c.Name, approval.Load().Keys)
		setup.ClearPending(c.Name, p.Fingerprint)
	}
	if p.Kind == "session" && incus.Available() {
		setup.PushGrants(c.Name, approval.Load().IPs)
		setup.Decide(c.Name, id, p.Decision)
	}
	fmt.Println("approved", id)
}

func cmdKeys(args []string) {
	if len(args) < 1 {
		usage()
	}
	c := mustCfg()
	switch args[0] {
	case "add":
		if len(args) < 3 {
			usage()
		}
		key := strings.Join(args[2:], " ")
		if err := setup.AddKey(c.Name, c.Space.User, key+" cs:"+args[1]); err != nil {
			die(exitErr, "%v", err)
		}
		fp, _ := setup.KeyFingerprint(key)
		fmt.Println("added", args[1], fp)
	case "ls":
		out, err := setup.ListKeys(c.Name, c.Space.User)
		if err != nil {
			die(exitErr, "%v", err)
		}
		fmt.Print(out)
	case "remove":
		if len(args) < 2 {
			usage()
		}
		if err := setup.RemoveKey(c.Name, c.Space.User, args[1]); err != nil {
			die(exitErr, "%v", err)
		}
		fmt.Println("removed", args[1])
	default:
		usage()
	}
}

func cmdPasswd() {
	c := mustCfg()
	pw := askSecret("New space password (min 12)")
	if err := setup.CheckPassword(pw); err != nil || pw == "" {
		die(exitErr, "password too short (min 12)")
	}
	if askSecret("Repeat") != pw {
		die(exitErr, "passwords do not match")
	}
	if _, err := incus.Exec(c.Space.Instance, []byte(c.Space.User+":"+pw+"\n"), "chpasswd"); err != nil {
		die(exitErr, "failed")
	}
	if !c.Space.PasswordAuth {
		c.Space.PasswordAuth = true
		c.Save()
		setup.WriteSSHDConfig(c.Name, true, c.Approval)
	}
	fmt.Println("password changed")
}

func cmdWho() {
	var ss []map[string]any
	config.ReadJSON("sessions.json", &ss)
	if len(ss) == 0 {
		fmt.Println("no live sessions")
		return
	}
	fmt.Printf("%-16s %-5s %-8s %-10s %-10s %s\n", "SRC-IP", "VIA", "IDLE", "UP", "DOWN", "STARTED")
	for _, s := range ss {
		idle, _ := s["idle_secs"].(float64)
		up, _ := s["bytes_up"].(float64)
		down, _ := s["bytes_down"].(float64)
		fmt.Printf("%-16v %-5v %-8s %-10s %-10s %v\n", s["src_ip"], s["via"], fmt.Sprintf("%ds", int(idle)), humanBytes(int64(up)), humanBytes(int64(down)), s["started"])
	}
}

func cmdLeave(args []string) {
	purge := len(args) > 0 && args[0] == "--purge"
	c := mustCfg()
	if err := leave(c); err != nil {
		die(exitUnreachable, "leave failed: %v", err)
	}
	fmt.Printf("left the network; name %s is free\n", c.Name)
	c.NodeID = ""
	c.Save()
	if purge && incus.Available() && incus.Exists(c.Space.Instance) {
		incus.Delete(c.Space.Instance)
		fmt.Println("space deleted")
	}
}
