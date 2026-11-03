package setup

// Per-user spaces (client 015 R5, R8, R11). The gateway in csnode does all SSH work, so these
// containers run no sshd: only sudo, the sftp server and csnode's relay helper.

import (
	"fmt"
	"os"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/node/gpu"
	"github.com/alishafiee1/computeshare-client/internal/node/incus"
	"github.com/alishafiee1/computeshare-client/proto"
)

// RelayPath is where csnode copies itself inside each user space for direct-tcpip (R2).
const RelayPath = "/usr/local/lib/csnode/csnode"

// CreateUserSpace creates <inst> for user with the node limits; disk overrides o.Disk (R8).
func CreateUserSpace(inst, user string, o SpaceOpts, out func(string)) error {
	if !strings.HasPrefix(inst, "cs-") || !proto.ValidUser(user) {
		return fmt.Errorf("invalid space %q for user %q", inst, user)
	}
	args := []string{"launch", o.Image, inst, "--network", Bridge,
		"-c", "security.privileged=false", "-c", "security.nesting=false", "-c", "user.computeshare.user=" + user}
	if os.Getenv("CSNODE_LAB_NO_CGROUP") != "1" {
		args = append(args, "-c", "limits.processes=4096")
		if o.CPU != "" {
			args = append(args, "-c", "limits.cpu="+o.CPU)
		}
		if o.Memory != "" {
			args = append(args, "-c", "limits.memory="+o.Memory)
		}
	}
	if _, err := incus.Run(nil, args...); err != nil {
		if strings.Contains(err.Error(), "no space left") {
			return fmt.Errorf("node out of disk")
		}
		return err
	}
	if o.Disk != "" {
		incus.Run(nil, "config", "device", "override", inst, "root", "size="+o.Disk)
	}
	if o.GPU {
		if err := gpu.Enable(inst, gpu.Probe(), 0); err != nil {
			out("warning: GPU not shared: " + err.Error())
		}
	}
	if err := SetBandwidth(strings.TrimPrefix(inst, "cs-"), o.Bandwidth); err != nil {
		out("warning: bandwidth: " + err.Error())
	}
	script := "set -e\n" + SpaceSourcesScript(o.Mirror) + `
for i in $(seq 1 30); do getent hosts deb.debian.org >/dev/null 2>&1 && break; sleep 1; done
export DEBIAN_FRONTEND=noninteractive
{ command -v sudo && test -x /usr/lib/openssh/sftp-server; } >/dev/null 2>&1 || { apt-get update -q && apt-get install -y -q sudo openssh-sftp-server; }
id -u "$CS_USER" >/dev/null 2>&1 || useradd -m -s /bin/bash -G sudo "$CS_USER"
echo "$CS_USER ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/90-computeshare; chmod 440 /etc/sudoers.d/90-computeshare
passwd -l root >/dev/null
`
	if _, err := incus.Exec(inst, []byte(script), "env", "CS_USER="+user, "sh", "-s"); err != nil {
		return err
	}
	return PushRelay(inst)
}

// PushRelay copies the running csnode binary into the space as the relay helper.
func PushRelay(inst string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return incus.PushFile(inst, RelayPath, b, "0755")
}

// Migrated is what MigrateSingleSpace found in the old space, for users.json.
type Migrated struct {
	User string
	// HadPassword: the old space had a password. Its crypt(3)/yescrypt hash cannot be checked
	// by the gateway (argon2id), so the owner sets a new one with `csnode users passwd`.
	HadPassword bool
	Keys        []string // authorized_keys lines
	Instance    string
}

// MigrateSingleSpace turns the v2 space cs-<node> into the space of its Linux user (R11):
// it reads the user's keys (and whether a password was set), renames the instance to cs-<node>-<user>,
// removes the in-space approval hooks and stops sshd. The caller stores the result.
func MigrateSingleSpace(node, user, newInst string) (*Migrated, error) {
	old := "cs-" + node
	if !incus.Exists(old) {
		return nil, fmt.Errorf("no v2 space %s to migrate", old)
	}
	if incus.Exists(newInst) {
		return nil, fmt.Errorf("%s already exists", newInst)
	}
	m := &Migrated{User: user, Instance: newInst}
	if b, err := incus.PullFile(old, "/home/"+user+"/.ssh/authorized_keys"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				m.Keys = append(m.Keys, l)
			}
		}
	}
	if sh, err := incus.Exec(old, nil, "getent", "shadow", user); err == nil {
		if f := strings.Split(strings.TrimSpace(sh), ":"); len(f) > 1 && strings.HasPrefix(f[1], "$") {
			m.HadPassword = true
		}
	}
	cleanup := `sed -i '/csnode-pamcheck/d' /etc/pam.d/sshd 2>/dev/null
rm -f /usr/local/bin/csnode-pamcheck /usr/local/bin/csnode-authkeys /etc/ssh/sshd_config.d/10-computeshare.conf
rm -rf /var/lib/csnode /etc/csnode /run/csnode
systemctl disable --now ssh 2>/dev/null || true
echo "` + user + ` ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/90-computeshare; chmod 440 /etc/sudoers.d/90-computeshare
export DEBIAN_FRONTEND=noninteractive
test -x /usr/lib/openssh/sftp-server || apt-get install -y -q openssh-sftp-server
`
	if _, err := incus.Exec(old, []byte(cleanup), "sh", "-s"); err != nil {
		return nil, err
	}
	if err := incus.Stop(old, true); err != nil {
		return nil, err
	}
	if _, err := incus.Run(nil, "rename", old, newInst); err != nil {
		return nil, err
	}
	incus.SetConfig(newInst, "user.computeshare.user="+user)
	if err := incus.Start(newInst); err != nil {
		return nil, err
	}
	return m, PushRelay(newInst)
}
