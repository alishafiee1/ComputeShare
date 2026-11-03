package setup

import (
	"fmt"
	"os"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/node/approval"
	"github.com/alishafiee1/computeshare-client/internal/node/incus"
)

// AuthKeysBinary is the path of the csnode-authkeys binary on the host to push into the space.
func AuthKeysBinary() string {
	if p := os.Getenv("CSNODE_AUTHKEYS"); p != "" {
		return p
	}
	exe, _ := os.Executable()
	return strings.TrimSuffix(exe, "csnode") + "csnode-authkeys"
}

// InstallAuthKeys pushes csnode-authkeys + banner + pending dir into the space (node 005 R4).
func InstallAuthKeys(name string) error {
	inst := "cs-" + name
	b, err := os.ReadFile(AuthKeysBinary())
	if err != nil {
		return fmt.Errorf("csnode-authkeys binary not found next to csnode: %v", err)
	}
	if err := incus.PushFile(inst, "/usr/local/bin/csnode-authkeys", b, "0755"); err != nil {
		return err
	}
	incus.PushFile(inst, "/etc/csnode/banner", []byte("This ComputeShare space requires owner approval for new keys.\nIf your key is rejected you are waiting for owner approval.\n"), "0644")
	if err := incus.PushFile(inst, "/usr/local/bin/csnode-pamcheck", []byte(PamCheck), "0755"); err != nil {
		return err
	}
	_, err = incus.Exec(inst, nil, "sh", "-c", "install -d -m 1733 /var/lib/csnode/pending && install -d -m 755 /var/lib/csnode/decisions /run/csnode/ports && touch /etc/csnode/approved /etc/csnode/granted_ips")
	return err
}

// PamCheck runs inside the space for password logins when approval is on (005 R5).
// sshd only sees the node agent as peer, so the real client IP is looked up from
// the local-port map the agent writes to /run/csnode/ports/<port>.
const PamCheck = `#!/bin/sh
[ "$PAM_TYPE" = "auth" ] || exit 0
port=$(ss -tnpH state established 2>/dev/null | grep "pid=$PPID," | awk '{print $4}' | head -1 | sed 's/.*://')
# client 016 R5: no guessing from "the last sshd connection"; unknown session = refuse.
[ -n "$port" ] || { echo "cannot identify session; try again"; exit 1; }
ip=$(cat /run/csnode/ports/$port 2>/dev/null)
[ -n "$ip" ] || { echo "cannot identify session; try again"; exit 1; }
now=$(date +%s)
if awk -v ip="$ip" -v now="$now" '$1==ip && $2>now {f=1} END {exit !f}' /etc/csnode/granted_ips 2>/dev/null; then exit 0; fi
echo "$ip" > /var/lib/csnode/pending/session-$port
echo "waiting for owner approval"
i=0
while [ $i -lt 60 ]; do
  d=$(cat /var/lib/csnode/decisions/$port 2>/dev/null)
  case "$d" in
    once|ip) rm -f /var/lib/csnode/decisions/$port /var/lib/csnode/pending/session-$port; exit 0;;
    deny) rm -f /var/lib/csnode/decisions/$port /var/lib/csnode/pending/session-$port; exit 1;;
  esac
  sleep 1; i=$((i+1))
done
rm -f /var/lib/csnode/pending/session-$port
exit 1
`

// MapPort tells the space which real client IP is behind a node->space local port.
func MapPort(name string, port int, ip string) {
	incus.Exec("cs-"+name, []byte(ip+"\n"), "sh", "-c", fmt.Sprintf("cat > /run/csnode/ports/%d", port))
}

func UnmapPort(name string, port int) {
	incus.Exec("cs-"+name, nil, "rm", "-f", fmt.Sprintf("/run/csnode/ports/%d", port))
}

// Decide writes an owner decision for a held password session.
func Decide(name, id, decision string) error {
	port := strings.TrimPrefix(id, "s")
	_, err := incus.Exec("cs-"+name, []byte(decision+"\n"), "sh", "-c", "cat > /var/lib/csnode/decisions/"+port)
	return err
}

// PushGrants writes "ip until" lines for 24 h IP grants.
func PushGrants(name string, g []approval.IPGrant) error {
	var b strings.Builder
	for _, x := range g {
		fmt.Fprintf(&b, "%s %d\n", x.IP, x.Until.Unix())
	}
	return incus.PushFile("cs-"+name, "/etc/csnode/granted_ips", []byte(b.String()), "0644")
}

func PushApproved(name string, fps []string) error {
	return incus.PushFile("cs-"+name, "/etc/csnode/approved", []byte(strings.Join(fps, "\n")+"\n"), "0644")
}

// CollectPending moves key fingerprints recorded inside the space into approvals.json.
func CollectPending(name string) {
	out, err := incus.Exec("cs-"+name, nil, "sh", "-c", `cd /var/lib/csnode/pending 2>/dev/null && for f in *; do [ -f "$f" ] && echo "$f $(cat $f)"; done; true`)
	if err != nil {
		return
	}
	d := approval.Load()
	live := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		if strings.HasPrefix(f[0], "session-") {
			id := "s" + strings.TrimPrefix(f[0], "session-")
			live[id] = true
			approval.AddPendingSession(id, f[1])
		} else if strings.HasPrefix(f[1], "SHA256:") && !d.KeyApproved(f[1]) {
			approval.AddPendingKey(f[1])
		}
	}
	approval.DropStaleSessions(live)
}

func ClearPending(name, fp string) {
	n := strings.NewReplacer("/", "_", "+", "-", ":", "_").Replace(fp)
	incus.Exec("cs-"+name, nil, "rm", "-f", "/var/lib/csnode/pending/"+n)
}

func ListKeys(name, user string) (string, error) {
	out, err := incus.Exec("cs-"+name, nil, "cat", "/home/"+user+"/.ssh/authorized_keys")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		fp, err := KeyFingerprint(l)
		if err != nil {
			continue
		}
		f := strings.Fields(l)
		label := "-"
		if len(f) > 2 {
			label = strings.TrimPrefix(f[len(f)-1], "cs:")
		}
		fmt.Fprintf(&b, "%-20s %s\n", label, fp)
	}
	return b.String(), nil
}

func RemoveKey(name, user, label string) error {
	if strings.ContainsAny(label, " \t\n/'\"") {
		return fmt.Errorf("invalid label")
	}
	_, err := incus.Exec("cs-"+name, nil, "sed", "-i", "/ cs:"+label+"$/d", "/home/"+user+"/.ssh/authorized_keys")
	return err
}
