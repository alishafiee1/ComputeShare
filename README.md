# ComputeShare

**Share your computer's spare power with people you trust, safely, over plain SSH.**

[فارسی](#فارسی)

ComputeShare lets the owner of a computer (a **node**) give others access to an isolated Linux space on it. A guest connects with an ordinary `ssh` command and works inside that space, with no access to the owner's files or home network. A small relay **server** in the middle connects guests and nodes. It never runs code and never sees passwords or content: everything is end-to-end encrypted.

```
guest  --ssh-->  relay server  <--outbound tunnel--  node (isolated Linux space)
```

## Why

- The owner stays in control: switch sharing on and off, set limits, block addresses, cut everything with one command.
- No router setup: the node connects **out** to the server, so it works behind a home modem without a static IP.
- The server is a relay only: no user code runs on it and it cannot read the traffic.
- Everything is open source, and releases are signed so you can check what you install.

## What is in this repository

| Folder | What it is | License |
|---|---|---|
| [`computeshare-client`](computeshare-client) | `csnode` (the node agent) and `cs` (the guest tool) | Apache-2.0 |
| [`computeshare-server`](computeshare-server) | `csbroker` (the relay server and admin panel) | AGPL-3.0 |
| [`examples`](examples) | A sample bot that uses a node (voice to text) | Apache-2.0 |
| [`docs`](docs) | Guides, for example using a node from AI tools and bots | |
| [`PROPOSAL_v3.md`](PROPOSAL_v3.md) | The design proposal (in Persian) | |

The two parts have different licenses on purpose; see [LICENSE.md](LICENSE.md).

## Quick start

**Use a node** (you are the guest):

```bash
cs setup                 # once
ssh <node-name>.cs       # you are inside the node's isolated Linux space
```

**Share your computer** (you are the owner): see [computeshare-client/INSTALL.md](computeshare-client/INSTALL.md) (Linux, and Windows through WSL2).

**Run a relay server:** see [computeshare-server/docs/DEPLOY.md](computeshare-server/docs/DEPLOY.md).

## Status

Early and actively developed. The core (isolated space, outbound tunnel, end-to-end SSH, owner controls, admin panel, signed releases) works. Some parts are still being finished or tested on real hardware: the Windows installer, GPU in the space, the in-country mirror, and per-user spaces. See [`docs/project-map.html`](docs/project-map.html) for a colourful overview you can open in a browser.

## Security

Please report vulnerabilities **privately**, not as public issues: see [computeshare-client/SECURITY.md](computeshare-client/SECURITY.md) and [computeshare-server/SECURITY.md](computeshare-server/SECURITY.md).

## Contributing

See [computeshare-client/CONTRIBUTING.md](computeshare-client/CONTRIBUTING.md) and [computeshare-server/CONTRIBUTING.md](computeshare-server/CONTRIBUTING.md). Please read the [code of conduct](computeshare-client/CODE_OF_CONDUCT.md).

---

## فارسی

**ComputeShare یعنی «سیستم‌هایمان را به هم قرض بدهیم».** صاحب یک کامپیوتر (نود) یک لینوکس جدا و امن روی آن می‌سازد و دیگران با یک دستور `ssh` ساده از راه دور وارد همان فضا می‌شوند؛ بدون دسترسی به فایل‌ها یا شبکهٔ خانهٔ او. یک سرور واسط وسط راه فقط ارتباط را برقرار می‌کند و هیچ‌چیز از محتوا نمی‌بیند، چون همه‌چیز سرتاسر رمزنگاری شده است.

- صاحب سیستم همیشه کنترل دارد: روشن و خاموش کردن اشتراک، سقف منابع و قطع فوری.
- بدون تنظیم مودم کار می‌کند.
- کاملاً متن‌باز است و نسخه‌ها امضا می‌شوند.

راهنمای نصب ساده: [computeshare-client/INSTALL.fa.md](computeshare-client/INSTALL.fa.md) · نقشهٔ تصویری پروژه: [docs/project-map.html](docs/project-map.html)
