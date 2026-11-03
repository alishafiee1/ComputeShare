# Deploying csbroker (v2)

csbroker is the ComputeShare **server**: it routes SSH between users and nodes and serves the registration API, the node tunnel and the admin panel. It never executes user code and it cannot read user traffic (end-to-end encrypted through ProxyJump).

v2 changes compared to the first deployment:
- **No per-node ports.** All SSH goes through **one port, 2222**: `ssh -J join@node.n3xus.ir:2222 user@<node>` (or `cs setup-ssh` then `ssh <node>.cs`). The `port_range` / `port_bind` keys, the `ip_local_reserved_ports` sysctl and the `40000-40199` ufw rule are gone. A leftover `port_range` key only prints a warning.
- The API, tunnel and panel are published at **`https://node.n3xus.ir`** through nginx on 443, **path-filtered** (`deploy/nginx/node.n3xus.ir.conf`).
- `trusted_proxies`: only nginx may set `X-Forwarded-For`, so rate limits and logs see the real client IP and clients cannot spoof it.
- Admin web panel at `/panel/` (password login, login rate limit, audit log). Optional node approval (`require_approval`).

Placeholders: `<SERVER_LAN_IP>`, `<PUBLIC_IP>`. No secret ever appears in this file; tokens and passwords are written to 0600 files or typed, never printed into logs.

Files: `deploy/broker/{csbroker.service,csbroker.yaml,jail.local}`, `deploy/nginx/node.n3xus.ir.conf`, `deploy/systemd/10-docker-bridge.conf` (only when nginx runs in Docker), `deploy/mirror/*`.


## 0. Prerequisites

- Linux x86_64 or arm64 with systemd and sudo; about 50 MB RAM (service capped at 512M).
- Free TCP `2222` (the only public port of csbroker) and one private HTTP port (`8088`).
- An nginx with TLS on 443 (an existing nginx on the host).
- `csbroker-linux-<arch>` and `SHA256SUMS` from a release (`dist/`).

## 1. Checks before install (read-only)

```bash
ss -tlnp; ss -tlnH | grep -E ':(2222|8088)\b' && echo BUSY
ufw status verbose; docker ps; docker network inspect bridge -f '{{(index .IPAM.Config 0).Gateway}}'
ip -4 addr; ip route
```
- Port 22 belongs to the host sshd, so the jump uses **2222**.
- Port 443 belongs to nginx, so csbroker HTTP stays private and nginx proxies to it.
- **ufw vs Docker**: Docker-published ports bypass ufw, so csbroker runs as a plain systemd service, not in Docker.
- NAT: forward only TCP 2222 on the router (step 10).

## 2. Backup

```bash
TS=$(date +%Y%m%d-%H%M%S); B=/root/csbroker-backup-$TS; mkdir -p $B
nft list ruleset > $B/nft.txt; ufw status verbose > $B/ufw.txt; cp -a /etc/ufw $B/; cp -a /etc/fstab $B/
# SQLite: copy the state only with the service stopped (a live copy can be inconsistent; ~1 s downtime)
if [ -d /var/lib/csbroker ]; then systemctl stop csbroker 2>/dev/null; cp -a /var/lib/csbroker $B/state; systemctl start csbroker 2>/dev/null; fi   # DB + SSH host key: keep private
```

## 3. Binary (checksum verified)

```bash
sha256sum -c --ignore-missing SHA256SUMS          # csbroker-linux-amd64: OK
install -m0755 -o root -g root csbroker-linux-amd64 /usr/local/bin/csbroker
csbroker version
```
SHA256SUMS proves integrity only. Proof of the publisher needs the signed `SHA256SUMS.sig` (card 012, see RELEASING.md; not implemented yet).

## 4. User and directories

```bash
id csbroker 2>/dev/null || useradd --system --no-create-home --home-dir /var/lib/csbroker --shell /usr/sbin/nologin csbroker
```
`/var/lib/csbroker` (0700) is created by `StateDirectory=`.

## 5. Config `/etc/csbroker.yaml`

```bash
install -m0644 deploy/broker/csbroker.yaml /etc/csbroker.yaml
```
```yaml
state: /var/lib/csbroker
ssh: 0.0.0.0:2222                       # the only SSH port
http: 172.18.0.1:8088                   # example: Docker bridge gateway, so the nginx container can reach it
trusted_proxies: 127.0.0.1/32, 172.18.0.0/16
require_approval: false                 # true = admin approves each new node
panel: true
panel_secure_cookie: true
```
Flags override keys: `--ssh --http --trusted-proxies --require-approval --panel --panel-secure-cookie --state`. Defaults: `ssh :2222`, `http 127.0.0.1:8088`, no trusted proxies, panel on.

Only peers in `trusted_proxies` may set `X-Forwarded-For`/`X-Real-IP`. Any container on `172.18.0.0/16` can therefore claim any client IP; once nginx has a fixed address (`ipv4_address` in compose, or `docker network connect --ip`), narrow the list to that one address, e.g. `127.0.0.1/32, 172.18.0.10/32` (`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' nginx`). csbroker walks `X-Forwarded-For` from the right and takes the first address that is not a trusted proxy. With an empty list the socket address is used. If csbroker listens on the Docker gateway, make sure only that bridge can reach 8088 (ufw does not open it; step 9).

## 6. systemd unit

```bash
install -m0644 deploy/broker/csbroker.service /etc/systemd/system/csbroker.service
systemd-analyze verify /etc/systemd/system/csbroker.service
systemctl daemon-reload && systemctl enable --now csbroker && systemctl is-active csbroker
```
**nginx in Docker :** csbroker listens on the Docker bridge gateway, which exists only after Docker has started. Install the drop-in so it starts after (and with) Docker:
```bash
install -d /etc/systemd/system/csbroker.service.d
install -m0644 deploy/systemd/10-docker-bridge.conf /etc/systemd/system/csbroker.service.d/10-docker-bridge.conf
systemctl daemon-reload && systemctl restart csbroker
```
(`After=docker.service`, `Requires=docker.service`, `RestartSec=10s`; `Restart=on-failure` covers the moment before the compose network exists.) Not needed when nginx runs on the host and csbroker uses `127.0.0.1:8088`.

Hardened: `User=csbroker`, empty capability set (all ports >1024), `ProtectSystem=strict`, `MemoryDenyWriteExecute`, `SystemCallFilter=@system-service`, `MemoryMax=512M`. rodi score (v1): `systemd-analyze security csbroker` = 1.2 OK.

## 7. Admin panel password and join tokens

```bash
sudo -u csbroker csbroker admin set-password --state /var/lib/csbroker          # on a terminal: prompts twice, nothing echoed
( umask 077; sudo -u csbroker csbroker token create --label join-default --state /var/lib/csbroker > /var/lib/csbroker/join-token )
```
From a pipe/file (no terminal) exactly **one line** is read and there is no repeat prompt; the trailing newline is stripped. Example without echo or shell history: `sudo -u csbroker csbroker admin set-password --state /var/lib/csbroker < /root/csbroker-panel-admin.pass` (file 0600). Note: `curl --data-urlencode password@file` sends the file's trailing newline too, so a login test that way fails with 401 — use `password=$(cat file)` or a file without newline.

Password policy: at least 12 characters, at most 72 bytes, 3 of 4 classes (lower, upper, digit, symbol), must not contain the username. Stored as bcrypt (cost 12). Login: max 5 failures per IP and 10 per user in 15 minutes; every login, failure and admin action goes to the audit log (panel → Audit). 2FA is a planned card (006).

Tokens can also be created in the panel (shown once). Only hashes are stored. Hand tokens to node owners privately.

## 8. nginx (`https://node.n3xus.ir`)

1. DNS: `A node.n3xus.ir → <PUBLIC_IP>` (Cloudflare: DNS only / grey cloud, the tunnel is a long-lived WebSocket).
2. TLS certificate for `node.n3xus.ir` (certbot `--webroot` through the ACME path on port 80, or the existing ACME setup).
3. Add the **443 server block** from `deploy/nginx/node.n3xus.ir.conf`; set the certificate paths and the upstream (`172.18.0.1:8088` when nginx is in Docker).
4. **Port 80:** if nginx already has a `:80` server (example: another site's), add `node.n3xus.ir` to its `server_name` and keep its `/.well-known/acme-challenge/` location. Do **not** add a second `:80` server with the same name. Only without an existing `:80` block, use the commented `:80` block in the file (ACME path + redirect).

**Where to put it depends on how nginx runs:**
- **nginx on the host:** put the file in `/etc/nginx/conf.d/`, then `nginx -t && systemctl reload nginx`.
- **nginx in a container (example: `nginx-proxy-1`)**: `conf.d` inside the container is not persistent. Recommended: **mount the block as its own file**, e.g. in the app's `docker-compose.prod.yml`: `- /opt/csbroker/nginx/node.n3xus.ir.conf:/etc/nginx/conf.d/node.n3xus.ir.conf:ro` (and make sure the main nginx.conf includes `conf.d/*.conf`), then recreate the container. If the container only bind-mounts a **single** `nginx.conf` (example: `/path/to/nginx.conf`), the block must go into that file.
  - Test first in a throwaway container: `docker run --rm -v /path/nginx.conf:/etc/nginx/nginx.conf:ro -v /etc/letsencrypt:/etc/letsencrypt:ro nginx:alpine nginx -t` (create the file **before** `docker run`, otherwise Docker creates a directory with that name).
  - Then `docker restart <proxy-container>`. **`nginx -s reload` is not enough** when the file was replaced (new inode): a single-file bind mount keeps pointing at the old inode until the container restarts. (Editing in place, e.g. `cat new > nginx.conf`, keeps the inode, but restart anyway.)
  - **Risk:** another app's deploy can rewrite its `deploy/nginx.conf` and silently drops the node block (it has happened). Either add the block to the app repo's `deploy/nginx.conf`, or — better — use the separately mounted file above so app deploys cannot remove it. Re-check `https://node.n3xus.ir/healthz` after every app deploy.

Only these paths reach csbroker: `/healthz`, `POST /v1/register`, `GET /v1/names/*`, `GET /v1/nodes`, `/v1/tunnel` (WebSocket, 1h timeouts), `/panel/`. Everything else returns 404. nginx **replaces** `X-Forwarded-For` with `$remote_addr`. You can restrict `/panel/` to your own IPs with `allow`/`deny`.

**Verify trusted_proxies:** every `/v1/` request logs `msg="http request" … client_ip=<real client> remote_addr=<nginx container IP>:<port>`: `journalctl -u csbroker -n 20 | grep "http request"`. If `client_ip` shows a `172.18.x.x` address, `trusted_proxies` does not include nginx's network.

## 9. Firewall

```bash
ufw limit 2222/tcp comment 'csbroker ssh'
# only when nginx runs in Docker: ufw's INPUT policy drops container→host traffic, so allow the docker network to the API
ufw allow from 172.18.0.0/16 to 172.18.0.1 port 8088 proto tcp comment 'csbroker api from nginx container'
```
Never open 8088 to anyone else (no `ufw allow 8088`). Find the network with `docker network inspect <net> -f '{{(index .IPAM.Config 0).Subnet}}'`. v1 leftovers to remove: `ufw delete allow 40000:40199/tcp`.

## 10. Router

Forward only TCP **2222** to `<SERVER_LAN_IP>` (443 already goes to nginx). Test from outside (phone hotspot):
```bash
nc -vz <PUBLIC_IP> 2222; ssh -p 2222 list@node.n3xus.ir       # node list
curl -s https://node.n3xus.ir/healthz                         # ok
```

## 11. fail2ban (host sshd only)

```bash
apt-get install -y fail2ban && install -m0644 deploy/broker/jail.local /etc/fail2ban/jail.local && systemctl restart fail2ban
```
csbroker has its own limits (card 008). Each client IP has separate token buckets: SSH 60/min (burst 20), node tunnel 12/min (burst 6), API 60/min (burst 30); 40 concurrent streams per IP and 50 per node. An empty bucket or a full slot table only refuses that request, so many users behind one carrier IP (CGNAT) or a busy bot are slowed down, never banned. 10-minute bans are only for abuse: 10 failed SSH handshakes or 10 requests for unknown node names in a minute, or 5 invalid join tokens in 10 minutes. Override the rates with `limit_ssh_per_min`, `limit_tunnel_per_min`, `limit_api_per_min` in `/etc/csbroker.yaml` (or the matching `-limit-*` flags).

Two-step panel login (server card 007): `sudo -u csbroker csbroker admin 2fa enable --user admin` prints a key and an `otpauth://` address for an authenticator app, asks for the first code, then shows ten single-use backup codes once. After that the panel asks for a code (or a backup code) after the password; wrong codes count toward the same login limits. `csbroker admin 2fa status|disable` work from the local terminal only. The secret is encrypted with `2fa.key` in the state directory (mode 0600): back that file up with the database.

Dead sessions are cleaned up (umbrella card 021): a jump session with no byte in either direction for `session_idle` (default `15m`) is closed with reason `idle`, and one whose write has been blocked for `session_stall` (default `90s`) with reason `stalled`; `0` turns a limit off. Every `session end` log line carries `reason` (`closed`, `idle`, `stalled`). Monitoring can read `GET /v1/version` (`{"version":"…","proto":2}`) without logging in.

## 12. Verification

```bash
systemctl is-active csbroker; ss -tlnp | grep -E ':(2222|8088)\b'
timeout 5 bash -c 'exec 3<>/dev/tcp/127.0.0.1/2222; head -c 32 <&3'        # SSH-2.0-csbroker
curl -s http://172.18.0.1:8088/healthz                                       # ok
curl -s -o /dev/null -w '%{http_code}\n' https://node.n3xus.ir/panel/login   # 200
curl -s -o /dev/null -w '%{http_code}\n' https://node.n3xus.ir/other         # 404 (nginx filter)
journalctl -u csbroker -n 20 --no-pager
```
Automated tests (no server needed): `make test` (unit), `make e2e` (in-process e2e: registration, approval, jump E2E with no plaintext on the wire, impostor host key, reconnect, bans, trusted proxies, panel login) and `make nginx-check` (runs this nginx config against a local csbroker: path/method filter, WebSocket upgrade, HTTPS panel login, X-Forwarded-For anti-spoofing).

## Upgrade from v1 

```bash
B=/root/csbroker-v2-backup-$(date +%Y%m%d-%H%M%S); mkdir -p $B
systemctl stop csbroker && cp -a /var/lib/csbroker $B/state && cp -a /etc/csbroker.yaml /usr/local/bin/csbroker $B/   # consistent SQLite copy
sha256sum -c --ignore-missing SHA256SUMS
install -m0755 csbroker-linux-amd64 /usr/local/bin/csbroker.new && mv /usr/local/bin/csbroker.new /usr/local/bin/csbroker
# /etc/csbroker.yaml: delete port_bind and port_range; add trusted_proxies, panel, require_approval (step 5).
# nginx in Docker: http MUST be the Docker bridge gateway, not the template default 127.0.0.1:8088:
#   http: 172.18.0.1:8088
#   trusted_proxies: 127.0.0.1/32, 172.18.0.0/16
rm -f /etc/sysctl.d/60-csbroker.conf && sysctl -w net.ipv4.ip_local_reserved_ports=
ufw delete allow 40000:40199/tcp
ufw allow from 172.18.0.0/16 to 172.18.0.1 port 8088 proto tcp comment 'csbroker api from nginx container'   # Docker nginx only
install -d /etc/systemd/system/csbroker.service.d && install -m0644 deploy/systemd/10-docker-bridge.conf /etc/systemd/system/csbroker.service.d/   # Docker nginx only
systemctl daemon-reload && systemctl start csbroker && csbroker version
ss -tlnp | grep -E ':(2222|8088)\b'          # expect 172.18.0.1:8088 and *:2222
sudo -u csbroker csbroker admin set-password --state /var/lib/csbroker
```
Then replace the v1 node block in nginx with the v2 block (step 8, including the container restart). The DB schema migrates on start (the per-node port column is dropped). Keep `ssh_host_ed25519_key`: changing it makes every client warn. Nodes must be upgraded to the v2 client (protocol 2); old nodes are refused with a clear version error.

## Rollback

```bash
systemctl disable --now csbroker
rm /etc/systemd/system/csbroker.service /etc/csbroker.yaml /usr/local/bin/csbroker
ufw delete limit 2222/tcp
# remove the node.n3xus.ir server block from nginx and reload
# optional: rm -rf /var/lib/csbroker (nodes, admins, host key); userdel csbroker
```
To roll back only the binary, reinstall the previous file; restore the backed-up state dir only with the service stopped (`systemctl stop csbroker; rm -rf /var/lib/csbroker; cp -a $B/state /var/lib/csbroker; systemctl start csbroker`).

## Mirror (optional)

`deploy/mirror/sync.sh` runs on a host with international access and fills `/srv/mirror` (40G LVM volume, hard cap); serve it static-only (`deploy/mirror/nginx.conf` or the commented `/mirror/` location). Model pre-fill is card 013.
