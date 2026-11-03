# computeshare-server

[فارسی](README.fa.md)

The server side of **ComputeShare**: share the CPU/RAM/GPU of home computers through SSH, without static IPs and without trusting the server.

`csbroker` is one static Go binary that:
- accepts nodes that connect **outbound** (WSS tunnel), so node owners need no static IP or port forwarding;
- routes SSH from users to nodes through **one port (2222)** with ProxyJump — end-to-end encrypted, the server never sees passwords or content and never runs user code;
- registers nodes with join tokens (optional admin approval), pins node host keys so impostors are rejected;
- lists nodes with specs and current/average load;
- has an **admin web panel** (`/panel/`) with a strong-password login, login rate limit and audit log;
- rate-limits and bans abusers; honours `X-Forwarded-For` only from `trusted_proxies`.

```
user ──ssh -J join@node.n3xus.ir:2222──▶ csbroker ◀──WSS tunnel (outbound)── csnode ──▶ isolated space :22
      └──────────── inner SSH, end-to-end encrypted (server cannot read it) ─────────────┘
```

The node agent and user CLI live in [computeshare-client](https://github.com/alishafiee1/computeshare-client) (which also hosts the shared `proto` package).

## Quick start

- Deploy: [docs/DEPLOY.md](docs/DEPLOY.md) ([فارسی](docs/DEPLOY.fa.md)).
- Build from source (needs Go and the client repo next to this one):
  ```bash
  git clone https://github.com/alishafiee1/computeshare-client
  git clone https://github.com/alishafiee1/computeshare-server
  cd computeshare-server && make check && make dist     # dist/csbroker-linux-{amd64,arm64} + SHA256SUMS
  ```
- Goals and their status: [GOALS.md](GOALS.md). Releases: [RELEASING.md](RELEASING.md).

## Project docs

- License: **AGPL-3.0** ([LICENSE](LICENSE)). Name and logo: [TRADEMARK.md](TRADEMARK.md).
- Security: [SECURITY.md](SECURITY.md). Contributing: [CONTRIBUTING.md](CONTRIBUTING.md). Conduct: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
