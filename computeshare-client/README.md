# computeshare-client

[فارسی](README.fa.md)

The client side of **ComputeShare** (source and releases: <https://github.com/alishafiee1/ComputeShare>):
- **`csnode`** — the node agent. Creates an isolated Linux space (Incus on Linux; WSL2 + Incus on Windows) with a sudo user `user` (root SSH closed), connects **outbound** to the server (no static IP needed), applies the owner's limits (empty = unlimited), passes the GPU through if asked, and starts automatically. The space can reach the internet but not the owner's LAN or host.
- **`cs`** — the user CLI. `cs setup` once, then `ssh <node>.cs`; node host keys are pinned, impostors are rejected.
- **Local owner panel** at `http://127.0.0.1:7777` (`csnode panel`): connection status, share on/off, limits, live sessions, kick.
- **`proto/`** — the wire protocol shared with [computeshare-server](https://github.com/alishafiee1/computeshare-server).

Install a node: [INSTALL.md](INSTALL.md) ([فارسی — راهنمای ساده](INSTALL.fa.md)). Default server: `https://node.n3xus.ir`.

Connect as a user:
```bash
cs setup                   # init + setup-ssh + known-hosts; server node.n3xus.ir:2222
ssh myBrotherPc.cs         # or: ssh -J join@node.n3xus.ir:2222 user@myBrotherPc
# the manual steps are still there: cs init, cs setup-ssh, cs known-hosts, cs trust; and cs add <name> <fingerprint> for an owner's invite
```

Build: `make vet test dist` → `dist/` with linux amd64/arm64 and windows amd64 binaries and `SHA256SUMS` (reproducible). Full-system e2e (root + Incus): `make e2e`. Releases: [RELEASING.md](RELEASING.md).

License: **Apache-2.0** ([LICENSE](LICENSE)) · Name/logo: [TRADEMARK.md](TRADEMARK.md) · [SECURITY.md](SECURITY.md) · [CONTRIBUTING.md](CONTRIBUTING.md) · [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
