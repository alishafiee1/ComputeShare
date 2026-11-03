#!/usr/bin/env bash
# Full-system e2e on one Linux host with Incus: real csbroker + real csnode + real Incus space.
# Covers: install/register (with admin approval), connect through the single jump port,
# root refused, sudo user, isolation (LAN/host blocked), host-key pinning, share off/on,
# instant kick, local panel auth, broker admin panel login.
# Needs: root via sudo, incus, sshpass, curl, go; a server checkout next to this repo
# (../computeshare-server). Env: IMAGE (Incus image with openssh-server; default images:debian/12),
# KEEP=1 keeps the lab after the run. Secrets are generated per run and never printed.
set -u
HERE=$(cd "$(dirname "$0")/.." && pwd); SRV=${SERVER_REPO:-$HERE/../computeshare-server}
W=${W:-/tmp/cs-sys-e2e}; BIN=$W/bin; IMAGE=${IMAGE:-images:debian/12}; NAME=e2eNode
HP=127.0.0.1:18088; SP=12222; TU=cse2e
pass=0; fail=0; skip=0
ok(){ echo "PASS $*"; pass=$((pass+1)); }; ko(){ echo "FAIL $*"; fail=$((fail+1)); }; sk(){ echo "SKIP $*"; skip=$((skip+1)); }
N="sudo env CSNODE_DIR=$W/node CSNODE_AUTHKEYS=$BIN/csnode-authkeys CSNODE_LAB_NO_CGROUP=${CSNODE_LAB_NO_CGROUP:-0} $BIN/csnode"
B="$BIN/csbroker"; BS="--state $W/broker"
S="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o PubkeyAuthentication=no -o ConnectTimeout=15"
J=(-o "ProxyCommand=ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -W %h:%p -p $SP join@127.0.0.1")
cleanup(){ sudo pkill -f "$BIN/csnode run" 2>/dev/null; pkill -f "$BIN/csbroker serve" 2>/dev/null; sudo incus delete -f cs-$NAME >/dev/null 2>&1; }
cleanup; sudo rm -rf $W; mkdir -p $BIN $W/broker
(cd "$SRV" && go build -o $BIN/csbroker ./cmd/csbroker) && (cd "$HERE" && go build -o $BIN/ ./cmd/csnode ./cmd/cs ./cmd/csnode-authkeys) || { echo "build failed"; exit 1; }
PW="E2e-$(openssl rand -hex 8)-Pw"; APW="Adm-$(openssl rand -hex 8)-Xy"
printf '%s\n' "$APW" | $B admin set-password $BS >/dev/null 2>&1
TOK=$($B token create $BS 2>/dev/null)
$B serve $BS --config /dev/null --http $HP --ssh 127.0.0.1:$SP --require-approval true --panel true --panel-secure-cookie false --limit-conn-per-min 1000 >$W/broker.log 2>&1 &
for i in $(seq 1 50); do curl -sf http://$HP/healthz >/dev/null && break; sleep 0.2; done

# 1 install + register (pending until approved)
printf '%s' "$PW" | $N setup --yes --name $NAME --password-stdin --server http://$HP --token "$TOK" --image "$IMAGE" --mirror off >$W/setup.log 2>&1
rc=$?; [[ $rc != 0 ]] && { echo "setup failed rc=$rc:"; tail -3 $W/setup.log; }; [[ $rc == 0 ]] && sudo incus list cs-$NAME -cs -f csv | grep -q RUNNING && ok "install: space created, setup rc=0" || { ko "install rc=$rc (see $W/setup.log)"; tail -5 $W/setup.log; }
curl -s http://$HP/v1/nodes | grep -q "\"$NAME\"" && ko "pending node visible in list" || ok "register: pending node hidden from list"
$B node approve $BS $NAME >/dev/null 2>&1 && ok "admin approve" || ko "approve"
$N run >$W/node.log 2>&1 &
t=$(date +%s); until curl -s http://$HP/v1/nodes | grep -q "\"name\":\"$NAME\",\"status\":\"online\"" || (( $(date +%s)-t > 60 )); do sleep 1; done
curl -s http://$HP/v1/nodes | grep -q "\"name\":\"$NAME\",\"status\":\"online\"" && ok "connect: node online over outbound tunnel" || ko "node not online"

# 2 connect through the single port, root refused, sudo user
h=$(SSHPASS=$PW sshpass -e ssh $S "${J[@]}" user@$NAME hostname 2>&1); [[ $h == cs-$NAME ]] && ok "ssh -J join@:$SP user@$NAME (single port)" || ko "jump ssh: $h"
SSHPASS=$PW sshpass -e ssh $S "${J[@]}" root@$NAME true 2>/dev/null && ko "root SSH allowed" || ok "root SSH refused"
sudo incus exec cs-$NAME -- id user </dev/null | grep -q '(sudo)' && ok "user is in sudo" || ko "sudo group"

# 3 isolation
gw=$(ip route | awk '/default/{print $3; exit}'); hostip=$(ip route get 1.1.1.1 | awk '{for(i=1;i<NF;i++) if($i=="src") print $(i+1)}')
for tgt in 192.168.1.1 $gw $hostip; do
  r=$(sudo incus exec cs-$NAME -- sh -c "timeout 4 bash -c '</dev/tcp/$tgt/22' 2>/dev/null && echo open || echo blocked" </dev/null)
  [[ $r == blocked ]] && ok "isolation: $tgt:22 blocked" || ko "isolation: $tgt $r"
done
acl=$(sudo incus network acl show csacl 2>/dev/null)
[[ $acl == *"192.168.0.0/16"* && $acl == *"10.0.0.0/8"* && $(printf '%s\n' "$acl" | grep -B0 -A1 -- '- action: allow' | grep -c 'state: enabled') -ge 1 ]] && ok "isolation: ACL rejects LAN ranges, final allow = internet" || ko "isolation ACL"
sk "internet egress from space (this sandbox does not forward container egress)"

# 4 cs client + host-key pinning
id $TU >/dev/null 2>&1 || sudo useradd -m $TU
sudo cp $BIN/cs /usr/local/bin/cs-e2e
sudo -u $TU -H bash -c "rm -rf ~/.ssh ~/.config/cs; cs-e2e init --server 127.0.0.1:$SP --api http://$HP >/dev/null && cs-e2e setup-ssh >/dev/null && ssh-keyscan -p $SP 127.0.0.1 2>/dev/null > ~/.ssh/known_hosts && cs-e2e known-hosts >/dev/null"
h=$(sudo -u $TU -H env SSHPASS=$PW sshpass -e ssh -o PubkeyAuthentication=no $NAME.cs hostname 2>&1); [[ $h == cs-$NAME ]] && ok "cs setup-ssh + ssh $NAME.cs" || ko "ssh $NAME.cs: $h"
out=$(sudo -u $TU -H bash -c "cs-e2e trust $NAME SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA >/dev/null; cs-e2e known-hosts 2>&1; SSHPASS=x sshpass -e ssh -o PubkeyAuthentication=no -o BatchMode=yes $NAME.cs true 2>&1; echo rc=\$?")
[[ $out == *"fingerprint mismatch"* && $out != *rc=0* ]] && ok "impostor/fake host key refused" || ko "pinning: $out"
sudo -u $TU -H bash -c 'rm -f ~/.config/cs/trust.json'

# 5 kick + share off/on
(SSHPASS=$PW sshpass -e ssh $S "${J[@]}" user@$NAME 'sleep 60' >/dev/null 2>&1; echo done >$W/kicked) & sleep 4
[[ -f $W/kicked ]] && ko "kick: session ended before kick (setup problem)"
$N kick 127.0.0.1 >/dev/null 2>&1; t=$(date +%s); until [[ -f $W/kicked ]] || (( $(date +%s)-t > 10 )); do sleep 0.5; done
[[ -f $W/kicked ]] && ok "kick: live session closed in $(( $(date +%s)-t ))s" || ko "kick did not close session"
$N share off >/dev/null 2>&1; sleep 2
SSHPASS=$PW sshpass -e ssh $S "${J[@]}" user@$NAME true 2>/dev/null && ko "share off still allows ssh" || ok "share off refuses new sessions"
$N share on >/dev/null 2>&1; sleep 2
h=$(SSHPASS=$PW sshpass -e ssh $S "${J[@]}" user@$NAME hostname 2>&1); [[ $h == cs-$NAME ]] && ok "share on restores access" || ko "share on: $h"

# 6 local panel
url=$($N panel 2>/dev/null | grep -o 'http://127.0.0.1[^ ]*')
c=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:7777/); [[ $c == 401 || $c == 403 ]] && ok "local panel: no token -> $c" || ko "local panel no-token $c"
c=$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: evil.example' "$url"); [[ $c == 403 || $c == 400 || $c == 421 ]] && ok "local panel: foreign Host rejected ($c)" || ko "local panel host check $c"
body=$(curl -s -L -c $W/lp.jar -b $W/lp.jar "$url"); [[ $body == *$NAME* ]] && ok "local panel: token link signs in, shows node" || ko "local panel login"
ss -ltn | grep -q '0.0.0.0:7777\|\*:7777' && ko "local panel listens beyond loopback" || ok "local panel bound to loopback only"

# 7 broker admin panel
c=$(curl -s -o /dev/null -w '%{http_code}' -c $W/ap.jar -H "Origin: http://$HP" --data-urlencode username=admin --data-urlencode "password=wrong-Password-1" http://$HP/panel/login)
[[ $c != 303 ]] && ok "admin panel: wrong password refused ($c)" || ko "wrong pw accepted"
c=$(curl -s -o /dev/null -w '%{http_code}' -c $W/ap.jar -b $W/ap.jar -H "Origin: http://$HP" --data-urlencode username=admin --data-urlencode "password=$APW" http://$HP/panel/login)
body=$(curl -s -b $W/ap.jar http://$HP/panel/); [[ $c == 303 && $body == *$NAME* ]] && ok "admin panel: login + node list" || ko "admin panel login $c"
sk "service autostart (box has no systemd; verify manually on a real host: csnode service install, reboot)"
sk "GPU passthrough (no GPU in sandbox; gpu package unit-tested)"
sk "Windows/WSL2 installer (needs Windows host)"
[[ ${KEEP:-0} == 1 ]] || cleanup
echo "== $pass passed, $fail failed, $skip skipped"; exit $fail
