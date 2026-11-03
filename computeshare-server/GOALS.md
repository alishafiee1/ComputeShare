# ComputeShare goals (v2)

A box is checked **only** when the goal is implemented **and** tested (automated test or recorded manual test). Last update: 2026-10-17.

Test suites referenced below:
- **unit** — `make test` in each repo.
- **S00x** — server in-process e2e (`computeshare-server/e2e`, `make e2e`): S001 registration rules, S002 jump E2E with no plaintext on the tapped path, S003 jump refusals incl. impostor host key, S004 admin approval, S005 reconnect + list, S006 ban, S007 trusted proxies / anti-spoofing, S008 panel login.
- **SYS** — full-system e2e (`computeshare-client/e2e/system_e2e.sh`): real csbroker + csnode + Incus space; last run 22 passed, 0 failed, 4 skipped.

- [x] **1. Client service connects **outbound** to the server (no static IP)** — _done_. SYS "node online over outbound tunnel"; S005 reconnect
- [x] **2. Server only routes, executes no code, **single port 2222**** — _done_. S002/S003, SYS "ssh -J … (single port)"; no per-node listeners exist in code
- [ ] **3. Isolated space via Incus (Linux) / WSL2 (Windows)** — _partial_. Linux/Incus done + SYS tested; WSL2 path built but **not tested on Windows**
- [x] **4. End-to-end encryption via ProxyJump** — _done_. S002 tap proxy sees no password/command; SYS jump login
- [x] **5. Node identity, impostor rejection** — _done_. S003 impostor host key refused; SYS "impostor/fake host key refused"
- [x] **6. Token join + optional admin approval** — _done_. S001, S004; SYS register pending → approve → online
- [x] **7. Access with user credentials; `user` has sudo; root SSH closed** — _done_. SYS "root SSH refused", "user is in sudo"
- [ ] **8. Isolation from LAN/host, internet allowed** — _partial_. LAN/host blocked: SYS (3 targets) + ACL check. **Live internet egress not testable in this sandbox** (ACL final allow rule verified only)
- [ ] **9. Resource limits (CPU/RAM/bandwidth/quota; empty = unlimited)** — _partial_. unit (control, quota, validation). **Real cgroup/bandwidth enforcement not testable here** (box lacks cgroup delegation; e2e ran with `CSNODE_LAB_NO_CGROUP=1`)
- [x] **10. Node list with specs and current/average load** — _done_. unit (list), S005, panel nodes page
- [x] **11. `cs` tool (`init`, `setup-ssh`, `known-hosts`, `trust`, `ssh <name>.cs`)** — _done_. unit (sshconfig, knownhosts); SYS "cs setup-ssh + ssh e2eNode.cs"
- [ ] **12. Easy install + autostart on Linux and Windows** — _partial_. Linux `install.sh` download + checksum tested locally; `csnode setup` tested in SYS. **Autostart (systemd / Windows task) not testable here** (no systemd on box, no Windows)
- [ ] **13. GPU access in the space** — _partial_. code + unit (gpu); **no GPU hardware to test**
- [x] **14. Server deploy guide + automated tests** — _done_. `docs/DEPLOY.md`, `docs/DEPLOY.fa.md`; unit + S001–S008 in CI
- [x] **15. Server admin web panel** — _done_. unit (panel), S008, SYS admin login + node list
- [x] **16. Client local web panel** — _done_. unit (localpanel, control, stream kick); SYS 4 panel checks + kick + share off/on
- [ ] **17. Release signing** — _**stub**_. `cs release` prints "planned"; card 012 (`computeshare-client`)
- [ ] **18. Internal mirror** — _partial_. `deploy/mirror` scripts + 40G volume on the server; sync not run; model prefill = card 013
- [x] **19. Simple client install guide** — _done_. `computeshare-client/INSTALL.fa.md`, `INSTALL.md`; installer tested (download + checksum)
- [x] **20. e2e tests** — _done_. S001–S008 (pass), SYS (22 pass / 0 fail / 4 skip: internet egress, autostart, GPU, Windows)
- [x] **21. Simple release + signing guide** — _done_. `RELEASING.md` / `RELEASING.fa.md` in both repos; signing steps clearly marked as card 012 (not implemented)
- [x] **22. GitHub open-source structure** — _done_. README (fa+en), LICENSE, SECURITY, CONTRIBUTING, TRADEMARK, CODE_OF_CONDUCT, issue/PR templates, CI + release workflows in both repos
