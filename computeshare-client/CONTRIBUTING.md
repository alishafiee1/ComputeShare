# Contributing

Thanks for helping! / ممنون که کمک می‌کنید.

1. **Discuss first** for anything bigger than a bug fix: open an issue (templates in `.github/ISSUE_TEMPLATE`).
2. **Describe behaviour changes:** say in the pull request what the behaviour is before and after, and add or update a test that proves it. Small fixes need one clear sentence.
3. **Build and test:** Go (version in `go.mod`).
   - `make vet test` must pass; `make e2e` (root + Incus) for node changes; the server repo CI also builds against `proto/`.
4. **Security rules (never break):** the server runs no user code and never sees inner-SSH content; a node forwards only to its space's port 22; spaces cannot reach the host/LAN; no secrets in code, tests, logs or issues.
5. **Style:** `gofmt`, small focused commits, clear messages (`feat:`, `fix:`, `docs:`, `test:`).
6. **License:** by contributing you agree your work is licensed under this repository's license (Apache-2.0). The name and logo are covered by `TRADEMARK.md`.

Security issues: see `SECURITY.md` (private reporting, never a public issue).
