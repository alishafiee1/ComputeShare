#!/usr/bin/env bash
# Builds csnode-linux-rootfs.tar.gz (the Linux that the Windows installer imports into WSL2) using the csnode binaries of a
# release build in dist/, so the rootfs and the release carry the SAME csnode.
# Run on a separate Linux machine, NOT on the node's own distro: it needs sudo, debootstrap and a network.
#   scripts/release.sh X.Y.Z --no-sign --no-upload --allow-dirty     # on the owner's computer: builds dist/
#   build-from-dist.sh /path/to/dist /path/to/csnode-linux-rootfs.tar.gz   # on Linux
#   scripts/release.sh X.Y.Z --rootfs /path/to/csnode-linux-rootfs.tar.gz  # the real release
# The two release.sh runs must be on the same commit and version, so the csnode inside the rootfs matches the signed one.
set -euo pipefail
DIST="${1:?usage: build-from-dist.sh DIST_DIR [OUT_FILE]}"; OUT="${2:-$PWD/csnode-linux-rootfs.tar.gz}"
for f in csnode-linux-amd64 csnode-authkeys-linux-amd64; do [ -f "$DIST/$f" ] || { echo "missing $DIST/$f (run release.sh --no-sign first)" >&2; exit 1; }; done
command -v debootstrap >/dev/null || { echo "debootstrap is not installed (sudo apt install debootstrap)" >&2; exit 1; }
BIN="$(mktemp -d)"; trap 'rm -rf "$BIN"' EXIT
cp "$DIST/csnode-linux-amd64" "$BIN/csnode"; cp "$DIST/csnode-authkeys-linux-amd64" "$BIN/csnode-authkeys"
cd "$(dirname "$0")" && BIN="$BIN" OUT="$OUT" ./build.sh
