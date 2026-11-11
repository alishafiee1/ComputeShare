// Package setup creates and manages the isolated space (node 001) and applies owner limits (node 005).
package setup

import (
	"fmt"
	"github.com/alishafiee1/computeshare-client/internal/node/gpu"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/proto"
)

const (
	Bridge = "csbr0"
	ACL    = "csacl"
	// ACLQuota is applied while the daily quota is exceeded: only SSH from the host stays.
	ACLQuota = "csacl-quota"
)

var blockedNets = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10",
	// client 016 R2: "this network", IETF protocol, benchmark, multicast and reserved ranges
	"0.0.0.0/8", "192.0.0.0/24", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4"}

// DefaultGateway is csbr0's address when csnode creates the bridge; nodes set up before
// client 016 have no space.gateway in config and use it.
const DefaultGateway = "10.77.0.1"

// ErrPasswordShort is the exact R4 message.
var ErrPasswordShort = fmt.Errorf("password too short (min 12)")

func CheckPassword(p string) error {
	if p != "" && len([]rune(p)) < 12 {
		return ErrPasswordShort
	}
	return nil
}

// EnsureIncus implements R1 (install from mirror on Debian/Ubuntu, init minimal).
func EnsureIncus(mirror string, out func(string)) error {
	if _, err := exec.LookPath("incus"); err != nil {
		osr := incus.OSRelease()
		id := osr["ID"]
		if id != "debian" && id != "ubuntu" {
			return fmt.Errorf("unsupported distribution: %s; install Incus manually and rerun", id)
		}
		out("installing Incus...")
		if mirror != "" {
			m, cn := strings.TrimRight(mirror, "/"), osr["VERSION_CODENAME"]
			// Signed by Debian; the host's archive keyring verifies it (014 R1).
			os.WriteFile(AptSourceFile, []byte(AptSource(m, cn)), 0o644)
		}
		up := exec.Command("apt-get", "update", "-q")
		up.Stdout, up.Stderr = os.Stdout, os.Stderr
		if err := up.Run(); err != nil {
			if mirror != "" {
				os.Remove(AptSourceFile) // 014 R6
			}
			return fmt.Errorf("package signature check failed; install stopped (%v)", err)
		}
		c := exec.Command("sh", "-c", "DEBIAN_FRONTEND=noninteractive apt-get install -y -q -o Dpkg::Options::=--force-confold incus || apt-get install -y -q -t $(. /etc/os-release; echo $VERSION_CODENAME)-backports incus")
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("incus install failed: %v", err)
		}
	}
	fixRootPerm(out)
	if err := initIncus(out); err != nil {
		return err
	}
	EnsureLXCFS(out) // card 016 R1: the space shows its own limits in free / nproc
	return nil
}

// fixRootPerm makes "/" traversable. A rootfs built from a private temp dir has "/" as 0700 owned
// by a normal user; then LXC cannot start any container ("Could not access /var").
func fixRootPerm(out func(string)) {
	if st, err := os.Stat("/"); err == nil && st.IsDir() && st.Mode().Perm()&0o055 != 0o055 {
		out("fixing permissions of / (was " + st.Mode().Perm().String() + ")")
		os.Chmod("/", 0o755)
		os.Chown("/", 0, 0)
	}
}

// initIncus replaces `incus admin init --minimal`: its automatic subnet search fails on hosts with
// many networks/VPNs and on WSL2 mirrored networking. Each step checks first, so a rerun after a
// partial failure just continues.
func initIncus(out func(string)) error {
	if o, _ := incus.Run(nil, "storage", "list", "--format", "csv"); strings.TrimSpace(o) == "" {
		if _, err := incus.Run(nil, "storage", "create", "default", "dir"); err != nil {
			return err
		}
	}
	if _, err := incus.Run(nil, "network", "show", "incusbr0"); err != nil {
		sub := PickSubnet(usedNets())
		if sub == "" {
			return fmt.Errorf("no free private IPv4 subnet for incusbr0")
		}
		out("creating network incusbr0 on " + sub)
		if _, err := incus.Run(nil, "network", "create", "incusbr0", "ipv4.address="+sub, "ipv4.nat=true", "ipv6.address=none"); err != nil {
			return err
		}
	}
	if o, _ := incus.Run(nil, "profile", "device", "list", "default"); !strings.Contains(o, "root") {
		if _, err := incus.Run(nil, "profile", "device", "add", "default", "root", "disk", "path=/", "pool=default"); err != nil {
			return err
		}
	}
	if o, _ := incus.Run(nil, "profile", "device", "list", "default"); !strings.Contains(o, "eth0") {
		if _, err := incus.Run(nil, "profile", "device", "add", "default", "eth0", "nic", "network=incusbr0", "name=eth0"); err != nil {
			return err
		}
	}
	return nil
}

// AptSourceFile is the host apt source csnode writes when installing Incus from the mirror.
var AptSourceFile = "/etc/apt/sources.list.d/computeshare.list"

// AptSource never marks the mirror trusted (014 R1).
func AptSource(m, codename string) string {
	return fmt.Sprintf("deb %s/apt/debian %s main\ndeb %s/apt/debian %s-backports main\n", m, codename, m, codename)
}

// FixAptSource drops "[trusted=yes]" left by csnode versions before 014 (edge case:
// existing nodes). It reports whether the file was changed.
func FixAptSource() (bool, error) {
	b, err := os.ReadFile(AptSourceFile)
	if err != nil || !strings.Contains(string(b), "trusted=yes") {
		return false, nil
	}
	return true, os.WriteFile(AptSourceFile, []byte(strings.ReplaceAll(string(b), "[trusted=yes] ", "")), 0o644)
}

func hostAddrs() []string {
	var out []string
	ifs, _ := net.InterfaceAddrs()
	for _, a := range ifs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			out = append(out, ipn.IP.String()+"/32")
		}
	}
	for _, n := range routeNets(readFile("/proc/net/route")) { // every network the host can reach, not only RFC1918
		out = append(out, n.String())
	}
	return append(out, gateways(readFile("/proc/net/route"))...)
}

func readFile(p string) string { b, _ := os.ReadFile(p); return string(b) }

// gateways parses /proc/net/route default routes. On WSL2 the default gateway is the
// Windows host (vEthernet WSL), which node 003 R5 requires to be blocked.
func gateways(route string) []string {
	var out []string
	for _, l := range strings.Split(route, "\n")[1:] {
		f := strings.Fields(l)
		if len(f) > 2 && f[1] == "00000000" && f[2] != "00000000" {
			v, err := strconv.ParseUint(f[2], 16, 32)
			if err == nil {
				out = append(out, fmt.Sprintf("%d.%d.%d.%d/32", v&255, v>>8&255, v>>16&255, v>>24))
			}
		}
	}
	return out
}

// ACLRules builds the egress/ingress rules of R5. gw is the csbr0 host address.
func ACLRules(gw string, quotaReached bool) (egress, ingress [][]string) {
	// Incus evaluates reject before allow regardless of order.
	seen := map[string]bool{}
	dsts := append(append([]string{}, blockedNets...), hostAddrs()...)
	if gw != "" {
		dsts = append(dsts, gw+"/32")
	}
	for _, n := range dsts {
		if seen[n] {
			continue
		}
		seen[n] = true
		egress = append(egress, []string{"action=reject", "destination=" + n})
	}
	egress = append(egress, []string{"action=reject", "protocol=tcp", "destination_port=25"})
	if quotaReached {
		egress = append(egress, []string{"action=reject", "destination=0.0.0.0/0"}, []string{"action=reject", "destination=::/0"})
	} else {
		egress = append(egress, []string{"action=allow"})
	}
	// ingress only from the host to 22 (the node agent dials from the bridge gateway)
	if gw != "" {
		ingress = append(ingress, []string{"action=allow", "source=" + gw + "/32", "protocol=tcp", "destination_port=22"})
	}
	return
}

// EnsureNetwork creates csbr0 + csacl (R5). Idempotent.
func EnsureNetwork() (string, error) {
	if _, err := incus.Run(nil, "network", "show", Bridge); err != nil {
		sub := PickSubnet(usedNets(), DefaultGateway+"/24")
		if sub == "" {
			return "", fmt.Errorf("no free private IPv4 subnet for %s", Bridge)
		}
		if _, err := incus.Run(nil, "network", "create", Bridge, "ipv4.address="+sub, "ipv4.nat=true", "ipv6.address=none"); err != nil {
			return "", err
		}
	}
	// 016 R3: use the bridge's real address (the owner may have changed it).
	gw := DefaultGateway
	if a, err := incus.Run(nil, "network", "get", Bridge, "ipv4.address"); err == nil {
		if ip, _, ok := strings.Cut(strings.TrimSpace(a), "/"); ok && net.ParseIP(ip) != nil {
			gw = ip
		}
	}
	if err := EnsureDNS(gw, func(m string) { fmt.Println(m) }); err != nil {
		return "", err
	}
	if err := ApplyACL(gw, false); err != nil {
		return "", err
	}
	if _, err := incus.Run(nil, "network", "set", Bridge, "security.acls="+ACL,
		"security.acls.default.egress.action=reject", "security.acls.default.ingress.action=reject"); err != nil {
		return "", err
	}
	return gw, nil
}

// ApplyACL (re)writes csacl; quotaReached blocks internet egress but keeps SSH ingress (node 005 R3).
func ApplyACL(gw string, quotaReached bool) error {
	incus.Run(nil, "network", "acl", "create", ACL)
	eg, in := ACLRules(gw, quotaReached)
	var y strings.Builder
	y.WriteString("description: ComputeShare space isolation (managed by csnode)\negress:\n")
	for _, r := range eg {
		y.WriteString(rule(r))
	}
	y.WriteString("ingress:\n")
	for _, r := range in {
		y.WriteString(rule(r))
	}
	y.WriteString("config: {}\n")
	_, err := incus.Run([]byte(y.String()), "network", "acl", "edit", ACL)
	return err
}

func rule(kv []string) string {
	var b strings.Builder
	for i, s := range kv {
		k, v, _ := strings.Cut(s, "=")
		if i == 0 {
			b.WriteString("- ")
		} else {
			b.WriteString("  ")
		}
		fmt.Fprintf(&b, "%s: %q\n", k, v)
	}
	b.WriteString("  state: enabled\n")
	return b.String()
}

type SpaceOpts struct {
	Name, User, Password, Image string
	Mirror                      string
	Keys                        []string
	CPU, Memory, Disk           string
	GPU                         bool
	Bandwidth                   string
}

// CreateSpace implements R3/R4. The password goes only into the instance via chpasswd stdin (R6).
func CreateSpace(o SpaceOpts, out func(string)) (hostKey string, err error) {
	inst := "cs-" + o.Name
	if !proto.ValidUser(o.User) {
		return "", fmt.Errorf("invalid user name %q", o.User)
	}
	if err := CheckPassword(o.Password); err != nil {
		return "", err
	}
	if o.Password == "" && len(o.Keys) == 0 {
		return "", fmt.Errorf("set a password or at least one public key")
	}
	if incus.Exists(inst) {
		// leftover of an earlier failed setup: a stopped space that cannot start is deleted and recreated
		if st, err := incus.GetState(inst); err == nil && st.Status != "Running" {
			if err := incus.Start(inst); err != nil {
				out("old space " + inst + " cannot start (" + err.Error() + "); deleting it and creating it again")
				if err := incus.Delete(inst); err != nil {
					return "", err
				}
			}
		}
	}
	if !incus.Exists(inst) {
		out("creating space " + inst + " ...")
		args := []string{"launch", o.Image, inst, "--network", Bridge,
			"-c", "security.privileged=false", "-c", "security.nesting=false"}
		labNoCgroup := os.Getenv("CSNODE_LAB_NO_CGROUP") == "1"
		if labNoCgroup {
			// Lab hosts without cgroup v2 delegation cannot apply limits; never set in production.
			out("WARNING: CSNODE_LAB_NO_CGROUP=1: cpu/memory/process limits NOT applied (lab only)")
			o.CPU, o.Memory = "", ""
		} else {
			args = append(args, "-c", "limits.processes=4096")
		}
		if o.CPU != "" {
			args = append(args, "-c", "limits.cpu="+o.CPU)
		}
		if o.Memory != "" {
			args = append(args, "-c", "limits.memory="+o.Memory)
		}
		if _, err := incus.Run(nil, args...); err != nil {
			return "", err
		}
	}
	if o.Disk != "" {
		incus.Run(nil, "config", "device", "override", inst, "root", "size="+o.Disk)
	}
	if o.GPU { // node 004: never fail setup because of the GPU (R2)
		if err := gpu.Enable(inst, gpu.Probe(), 0); err != nil {
			out("warning: GPU not shared: " + err.Error())
		}
	}
	if err := SetBandwidth(o.Name, o.Bandwidth); err != nil {
		out("warning: bandwidth: " + err.Error())
	}
	out("installing openssh-server and sudo inside the space ...")
	script := "set -e\n" + SpaceSourcesScript(o.Mirror) + `
for i in $(seq 1 30); do getent hosts deb.debian.org >/dev/null 2>&1 && break; sleep 1; done
export DEBIAN_FRONTEND=noninteractive
command -v sshd >/dev/null || { apt-get update -q && apt-get install -y -q openssh-server sudo; }
id -u "$CS_USER" >/dev/null 2>&1 || useradd -m -s /bin/bash -G sudo "$CS_USER"
usermod -aG sudo "$CS_USER"
# unique host keys per space even when the image was cloned
if [ ! -f /etc/ssh/.cs-hostkeys ]; then rm -f /etc/ssh/ssh_host_*; ssh-keygen -A >/dev/null; touch /etc/ssh/.cs-hostkeys; systemctl reset-failed ssh 2>/dev/null; systemctl restart ssh || true; fi
passwd -l root >/dev/null
install -d -m 700 -o "$CS_USER" -g "$CS_USER" /home/$CS_USER/.ssh
touch /home/$CS_USER/.ssh/authorized_keys; chown $CS_USER: /home/$CS_USER/.ssh/authorized_keys; chmod 600 /home/$CS_USER/.ssh/authorized_keys
`
	if _, err := incus.Exec(inst, []byte(script), "env", "CS_USER="+o.User, "sh", "-s"); err != nil {
		return "", err
	}
	if o.Password != "" {
		if _, err := incus.Exec(inst, []byte(o.User+":"+o.Password+"\n"), "chpasswd"); err != nil {
			return "", fmt.Errorf("setting password failed")
		}
	}
	for _, k := range o.Keys {
		if err := AddKey(o.Name, o.User, k); err != nil {
			return "", err
		}
	}
	if err := WriteSSHDConfig(o.Name, o.Password != "", false); err != nil {
		return "", err
	}
	return HostKey(o.Name)
}

// WriteSSHDConfig writes the managed drop-in (R4 + node 005 R4).
func WriteSSHDConfig(name string, passwordAuth, approval bool) error {
	inst := "cs-" + name
	pa := "no"
	if passwordAuth {
		pa = "yes"
	}
	c := "# managed by csnode\nPermitRootLogin no\nPasswordAuthentication " + pa + "\nKbdInteractiveAuthentication no\nMaxAuthTries 3\nLogLevel VERBOSE\n"
	if approval {
		c += "AuthorizedKeysFile none\nAuthorizedKeysCommand /usr/local/bin/csnode-authkeys %u %f\nAuthorizedKeysCommandUser root\nBanner /etc/csnode/banner\n"
	}
	if err := incus.PushFile(inst, "/etc/ssh/sshd_config.d/10-computeshare.conf", []byte(c), "0644"); err != nil {
		return err
	}
	// password approval hold (005 R5) via PAM, only active with approval on
	pam := `sed -i '/csnode-pamcheck/d' /etc/pam.d/sshd`
	if approval && passwordAuth {
		pam += ` && sed -i '1i auth requisite pam_exec.so stdout quiet /usr/local/bin/csnode-pamcheck' /etc/pam.d/sshd`
	}
	if _, err := incus.Exec(inst, nil, "sh", "-c", pam); err != nil {
		return err
	}
	_, err := incus.Exec(inst, nil, "sh", "-c", "install -d -m 755 /run/sshd && sshd -t && (systemctl reload ssh || { systemctl reset-failed ssh; systemctl restart ssh; })")
	return err
}

func HostKey(name string) (string, error) {
	b, err := incus.PullFile("cs-"+name, "/etc/ssh/ssh_host_ed25519_key.pub")
	if err != nil {
		return "", err
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return "", fmt.Errorf("no ed25519 host key in space")
	}
	return f[0] + " " + f[1], nil
}

// SetBandwidth sets limits.ingress/egress on eth0 (node 005 R2). Empty = unlimited.
func SetBandwidth(name, bw string) error {
	if _, err := proto.ParseBandwidth(bw); err != nil {
		return err
	}
	inst := "cs-" + name
	v := strings.TrimSpace(bw)
	if strings.EqualFold(v, "off") || strings.EqualFold(v, "unlimited") {
		v = ""
	}
	incus.Run(nil, "config", "device", "override", inst, "eth0")
	_, err := incus.Run(nil, "config", "device", "set", inst, "eth0", "limits.ingress="+v, "limits.egress="+v)
	return err
}

func AddKey(name, user, key string) error {
	if _, _, _, _, err := parseAuthorized(key); err != nil {
		return fmt.Errorf("invalid public key")
	}
	_, err := incus.Exec("cs-"+name, []byte(strings.TrimSpace(key)+"\n"), "sh", "-c",
		`f=/home/$1/.ssh/authorized_keys; k=$(cat); grep -qxF "$k" "$f" || echo "$k" >> "$f"`, "_", user)
	return err
}
