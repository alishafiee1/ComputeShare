# Security policy — computeshare-client

## Reporting a vulnerability

Please **do not** open a public issue. Use GitHub's private vulnerability reporting ("Security" → "Report a vulnerability") on this repository, or contact the maintainers (alishafiee1) privately. Include the affected version, steps to reproduce and impact. We aim to acknowledge within 7 days and to fix critical issues before any public disclosure.

## Supported versions

Only the latest release (and `main`) receives security fixes.

## Security guarantees (a break of any of these is a vulnerability)

- The server (`csbroker`) never executes user code and never sees inner-SSH credentials or session content (end-to-end via ProxyJump on port 2222).
- A node forwards traffic only to its isolated space's port 22.
- A space cannot reach the owner's host or LAN; root SSH into a space is closed.
- Impostor nodes are rejected (host keys are signed by the device key and pinned by `cs`).
- Join tokens, panel passwords and session ids are stored only as hashes; secrets are never logged.
- The admin panel (server) and the local panel (client, loopback only) require authentication and reject cross-site requests.

## Releases

Every release ships `SHA256SUMS`; installers verify the checksums. **Signing of `SHA256SUMS` is planned (card 012) and not implemented yet**: until then a checksum proves integrity, not the publisher. Download only from the official GitHub releases or `https://node.n3xus.ir/mirror/`. See `RELEASING.md`.

---

**فارسی:** مشکل امنیتی را عمومی منتشر نکنید؛ از «Report a vulnerability» در GitHub یا به‌طور خصوصی به نگهدارندگان (alishafiee1) گزارش دهید. فعلاً نسخه‌ها فقط SHA256SUMS دارند و امضا (کارت 012) هنوز پیاده نشده است؛ فقط از منبع رسمی دانلود کنید.
