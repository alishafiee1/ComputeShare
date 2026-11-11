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

Run `csnode-setup-<version>.exe` (choose "all users"/administrator, or "only me" if WSL is already installed — see below). It enables WSL2 (one reboot may be needed; setup continues by itself), imports the `csnode-linux` distro, and registers a startup task. Then open a terminal and run `csnode setup` — the same questions as on Linux. All `csnode` commands work the same in PowerShell. (The Windows installer is built from `installer/windows/csnode.iss`; it is not yet tested on real Windows hardware.)

### Windows: admin or not?

| Step | Needs administrator? |
|---|---|
| Turning on WSL2 (`wsl --install`, only when WSL is **not** installed yet) | **Yes** |
| Boot-time startup task (runs as SYSTEM, node starts without anyone logging in) | **Yes** |
| Adding `csnode` to the machine-wide PATH, installing into `Program Files` | **Yes** |
| WSL2 **already installed**: importing the `csnode-linux` distro, `csnode setup`, running the node | No |

So: if WSL is already on this computer, choose **"Install for me only"** in the first dialog (or pass `/CURRENTUSER`). Files go to `%LOCALAPPDATA%\Programs\ComputeShare`, `csnode` is added to your own PATH, and the node starts **when you log on** (a Run entry in your user profile) instead of at boot. If WSL is missing, a per-user run stops on the system-check page and tells you to run again as administrator. (Not verified on a real second machine yet — treat the per-user mode as new.)

### Windows: silent / unattended install

Use a PowerShell, run in the folder with the installer:

```powershell
.\csnode-setup-<version>.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART /LOG="$env:TEMP\csnode-setup.log" `
  /ACCEPT /SERVER=https://node.n3xus.ir /TOKENFILE=C:\secrets\token.txt /NODENAME=my-node-01 `
  /WSLDIR=D:\ComputeShare /SPACEUSER=guest /SPACEPASSFILE=C:\secrets\pass.txt
echo $LASTEXITCODE
```

Add `/CURRENTUSER` (no admin, WSL must already exist) or `/ALLUSERS` (admin; an elevated shell avoids the UAC prompt).

| Parameter | Meaning |
|---|---|
| `/ACCEPT` | **required**: you accepted the terms in `consent.txt` |
| `/SERVER=` | server address, `https://…` (default `https://node.n3xus.ir`) |
| `/TOKEN=` or `/TOKENFILE=` | join token, or a file whose first line is the token (a file keeps it out of the process list) |
| `/NODENAME=` | node name, 3–32 chars, letters/digits/dash, starts with a letter |
| `/WSLDIR=` | folder for the Linux system file, local drive, ≥15 GB free (default: the drive with most free space) |
| `/SPACEUSER=` | space user (default `user`) |
| `/SPACEPASS=` or `/SPACEPASSFILE=` | space password, 12+ ASCII chars (optional if `/SPACEKEY` is given) |
| `/SPACEKEY=` | SSH public key, one line starting with `ssh-` (optional if a password is given) |
| `/CPU=` `/MEMORY=` `/DISK=` | limits, e.g. `4`, `8GiB`, `100GiB`. Not given = the same suggested safe values the questions offer |

In the same way, the normal (non-silent) wizard is pre-filled by these parameters.

**Exit codes** (0 = success). On any failure a reason is written to `%LOCALAPPDATA%\ComputeShare\logs\install-result.log`; the detailed logs are `install.log` and `setup.log` in the same folder (use Inno's `/LOG=` for the installer's own log).

| Code | Meaning |
|---|---|
| 0 | installed and connected |
| 20 | a parameter is missing or invalid (the log says which) |
| 21 | `/ACCEPT` missing |
| 22 | Windows too old or virtualization off in BIOS |
| 23 | WSL is not installed and this run is not administrator |
| 30 | installing WSL / the Linux system failed (`install.log`) |
| 31 | node setup failed (`setup.log`) |
| 32 | node name already taken on the server |
| 33 | server not reachable |
| 3010 | WSL was just installed: restart Windows, then run the same command again |

When the wizard (non-silent) fails, the last page is red, says **"Installation FAILED"**, and shows the reason and the log path.

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

### DNS inside the space

csnode gives spaces DNS through the bridge's dnsmasq. If the host's own resolver cannot be used by dnsmasq (WSL2's `10.255.255.254`, a `127.x` stub), csnode sets `raw.dnsmasq` on `csbr0` with real upstreams: first the ones from `/run/systemd/resolve/resolv.conf`, otherwise `1.1.1.1`, `8.8.8.8`, `9.9.9.9`. To choose your own, run setup with the variable set:

```bash
sudo CSNODE_DNS="9.9.9.9,1.1.1.1" csnode setup
```
It is safe to rerun, and a `raw.dnsmasq` you wrote yourself is never overwritten. The space's firewall is unchanged: dnsmasq asks upstream from the host, so no LAN access is opened.
