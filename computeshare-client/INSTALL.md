# Installing a ComputeShare node

A node shares part of a computer's CPU/RAM/GPU as an **isolated Linux space**. People you give the space password (or key) to can log in over SSH; they cannot reach your files, your home network, or the computer itself. The space has internet access. You can stop it any time.

You need: a **join token** from the server admin, and a password (≥ 12 characters) for the space user.

## Linux (Debian/Ubuntu and similar, x86_64 or arm64)

```bash
curl -fsSL https://node.n3xus.ir/install.sh | sudo sh
# or from GitHub (once a release is published):
curl -fsSL https://github.com/alishafiee1/ComputeShare/releases/latest/download/install.sh | sudo sh -s -- --from https://github.com/alishafiee1/ComputeShare/releases/latest/download
```
The installer downloads `csnode`, `cs`, `csnode-authkeys`, checks the release signature of `SHA256SUMS` (Ed25519, key built into the installer) and then the files against it, installs them in `/usr/local/bin`, and starts `csnode setup`. Unsigned or changed files stop the install.

`csnode setup` asks, in order: consent → installs Incus if missing → shows your resources → CPU / memory / disk for the space (empty = no limit) → share the GPU? → **server address (default `https://node.n3xus.ir`)** → node name → space user (default `user`, has sudo; root login is closed) → password (+ optional SSH public key) → join token → internet bandwidth limit (suggested `20Mbit`, empty = unlimited) → daily data quota (suggested `20GB/day`, empty = unlimited).

Then make it start automatically:
```bash
sudo csnode service install
```

Non-interactive example:
```bash
printf '%s' "$SPACE_PASSWORD" | sudo csnode setup --yes --name myBrotherPc --password-stdin \
  --server https://node.n3xus.ir --token "$TOKEN" --bandwidth 20Mbit --quota 20GB/day
```

## Windows 10 (21H2+) / 11

Run `csnode-setup-<version>.exe` as administrator. It enables WSL2 (one reboot may be needed; setup continues by itself), imports the `csnode-linux` distro, and registers a startup task. Then open a terminal and run `csnode setup` — the same questions as on Linux. All `csnode` commands work the same in PowerShell. (The Windows installer is built from `installer/windows/csnode.iss`; it is not yet tested on real Windows hardware.)

## After install

| What | Command |
|---|---|
| Status | `sudo csnode status` |
| Local web panel (status, share on/off, limits, sessions, kick) | `sudo csnode panel` → open the printed `http://127.0.0.1:7777/?t=…` link on this computer (keep it private) |
| Pause / resume sharing | `sudo csnode share off` / `sudo csnode share on` |
| Disconnect someone now | `sudo csnode kick <client-ip>` (or session id from the panel) |
| Limits | `sudo csnode limits set --cpu 4 --memory 8GiB`, `sudo csnode bandwidth 20Mbit`, `sudo csnode quota 20GB/day` (`off` = unlimited) |
| Approve each new key/login | `sudo csnode config approval on`, then `pending` / `approve <id>` |
| Stop / start the space | `sudo csnode stop` / `sudo csnode start` |
| Remove everything | `sudo csnode leave --purge` |

If the server requires admin approval, setup says the node is **pending**; it comes online automatically once the admin approves it.

## How users connect

```bash
ssh -J join@node.n3xus.ir:2222 user@myBrotherPc
# or, once: cs setup   (does init + setup-ssh + known-hosts in one go) ; then:
ssh myBrotherPc.cs
```
`cs setup` needs no invite. It lists the nodes and marks each as `trusted: owner` (pinned with the owner's fingerprint) or `trusted: server-only` (only the server's word). If the server is given as an IP, add `--server-host <dns-name>` so the HTTPS certificate can be checked.
Give users the invite line that setup (and `csnode status`) prints, for example `cs add myBrotherPc SHA256:…`. One command sets up `cs`, the ssh config and the pinned host key, then `ssh myBrotherPc.cs` works. Without it, users trust the node only on the server's word and `cs` warns about that.

## Several users, each with their own space (optional)

Turn it on once with `sudo csnode users migrate` (an existing space becomes the space of its user), then add people:
```bash
sudo csnode users add ali --key-file ali.pub      # or without --key-file to set a password
sudo csnode users ls
```
Each user gets their own container on first login (`ssh ali@myBrotherPc.cs`), with their files kept between logins. Idle spaces stop after 30 minutes (`idle_stop` in config) and at most 3 run at once (`max_running_spaces`). The host key changes when you turn this on, so send users the new `cs add` line from `csnode status`.
