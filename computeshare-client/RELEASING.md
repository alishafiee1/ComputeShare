# Releasing ComputeShare

Covers both repos: **computeshare-client** (`csnode`, `cs`, `csnode-authkeys`, `install.sh`) and **computeshare-server** (`csbroker`). Builds are reproducible (`CGO_ENABLED=0 -trimpath -buildvcs=false -ldflags "-s -w -buildid="`), so anyone can rebuild a tag and get byte-identical files and the same `SHA256SUMS`.

> **Status:** building, checksums and **signing (card 012)** work, and `scripts/release.sh` runs the whole release on the owner's computer. `install.sh` and the Windows `csnode install` refuse anything that is not signed by the key in `internal/release/release.pub` (the key was created on 2026-10-31). Source and releases live in one repo: <https://github.com/alishafiee1/ComputeShare> (client and server are folders of it; one tag `vX.Y.Z` covers both).

## Rules

1. The **private signing key lives only on the owner's laptop** (plus an offline USB backup). Never on the bot box, the server, CI, or in GitHub secrets.
2. CI builds and publishes **draft** releases with `SHA256SUMS`; only the owner publishes them, after signing.
3. Server and client are released with the **same tag** `vX.Y.Z` (the server build checks out the client at that tag for `proto`).

## 1. Prepare (any machine)

```bash
# both repos side by side, clean trees, on main
(cd computeshare-client && make vet test)
(cd computeshare-server && make vet test e2e)
sudo IMAGE=images:debian/12 computeshare-client/e2e/system_e2e.sh   # lab machine with Incus: must end "0 failed"
```
Bump nothing else (the version comes from the tag).

## 2. Run the release script (owner's computer)

From inside the repo (Git Bash on Windows, or any Linux shell), on `main` with a clean tree:
```bash
scripts/release.sh 0.2.0
```
It checks the tree and the release key, runs vet and tests for client and server, builds `dist/` (all binaries, `install.sh` with the public key inserted, `SHA256SUMS`), asks for your key passphrase to sign (`SHA256SUMS.sig`), verifies the signature, creates the annotated tag `v0.2.0`, pushes it and creates a **draft** GitHub release with every file. You publish the draft after looking at it. It needs `go`, `sha256sum`, `openssl` (for the checks) and `gh` (logged in) on that computer.

Dry run (build only, no signing, tag or upload; also proves the build is reproducible by building twice and comparing):
```bash
scripts/release.sh 0.2.0 --no-sign --no-upload --allow-dirty --reproducible-check
```
Builds are reproducible: `-trimpath -buildvcs=false -ldflags "-s -w -buildid="`, and the version printed by `csnode version` is `X.Y.Z+<commit>`, so a running binary maps to a commit.

## 3. Sign on the owner's laptop (card 012)

One time, before the first signed release:
```bash
cs release keygen            # ~/.config/cs/release.key (Ed25519 seed sealed with your passphrase, scrypt + secretbox) and release.pub
# copy release.key to an offline USB stick; never anywhere else
cp ~/.config/cs/release.pub computeshare-client/internal/release/release.pub   # commit it BEFORE tagging
```
The public key is built into `cs` and `csnode` and inserted into `dist/install.sh` by `scripts/release.sh` (and by `make dist`). If you ever lose or rotate the key, every older installer and binary will reject the new releases; plan rotations as a normal release with the new `release.pub`.

Every release signs by hand only if you do not use the script:
```bash
cs release sign dist/SHA256SUMS   # asks for the passphrase, writes dist/SHA256SUMS.sig (raw Ed25519 signature)
cs release verify dist/SHA256SUMS # self-check with the key built into your cs
```
`scripts/release.sh` does this for you and uploads `SHA256SUMS.sig` to the draft release; you then publish it. (The build also makes `cs-bench-linux-*`, the helper for `cs bench --run`; it is listed in `SHA256SUMS` like the other binaries.) The server binary `csbroker-linux-*` is in the same release and the same `SHA256SUMS`.
Windows: build `csnode-setup.exe` (Inno Setup, `installer/windows/csnode.iss`) **after** signing, with `SHA256SUMS` and `SHA256SUMS.sig` next to the rootfs; `csnode install` checks the rootfs against them.
Mirror image list (card 014): sign `images.lock` the same way (`cs release sign images.lock`) and put both files in `/srv/mirror/images/`.

Check the installer end to end on any Linux box: `e2e/installer_sig.sh` (signed install works; tampered binary, tampered SHA256SUMS, missing signature and a key-less installer all abort).

## 4. Mirror (optional)

Fill the mirror from the signed release with `computeshare-server/deploy/mirror/sync.sh` (see its README) so `curl -fsSL https://node.n3xus.ir/install.sh | sudo sh` works from Iran. Both `https://node.n3xus.ir/install.sh` and `https://node.n3xus.ir/mirror/csnode/install.sh` must answer 200 after a sync.

## 5. Deploy the server

Follow "Upgrade" in `computeshare-server/docs/DEPLOY.md` (checksum check, backup of the state dir, restart). Only with the owner's go-ahead.
