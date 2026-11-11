#!/bin/sh
# ComputeShare node installer for Linux.
#   curl -fsSL https://node.n3xus.ir/mirror/csnode/install.sh | sudo sh
#   curl -fsSL .../install.sh | sudo sh -s -- --from https://github.com/alishafiee1/computeshare-client/releases/download/v0.2.0 [csnode setup flags]
# Downloads csnode + cs + csnode-authkeys + SHA256SUMS + SHA256SUMS.sig, verifies the release signature
# (card 012: Ed25519, key below, checked with openssl) and then the checksums, installs to /usr/local/bin,
# then runs `csnode setup` (it asks for the server address, default https://node.n3xus.ir).
# The signature proves the files come from the project owner even if the download server is compromised.
# This script itself comes from the download server: compare it with the copy in the GitHub release if unsure.
set -eu
# Release public key, inserted by `make dist` from internal/release/release.pub. Empty = unsigned build: refuse.
RELEASE_PUB=""
SIGFAIL="امضای نسخه معتبر نیست؛ نصب متوقف شد. فایل را فقط از منبع رسمی دریافت کنید."
FROM="${CS_FROM:-https://node.n3xus.ir/mirror/csnode}"
if [ "${1:-}" = "--from" ]; then FROM="$2"; shift 2; fi
PREFIX="${CS_PREFIX:-/usr/local/bin}"   # CS_PREFIX: tests only
[ "$(id -u)" = 0 ] || [ -n "${CS_PREFIX:-}" ] || { echo "run as root: curl ... | sudo sh"; exit 1; }
case "$(uname -m)" in x86_64|amd64) A=amd64;; aarch64|arm64) A=arm64;; *) echo "unsupported CPU $(uname -m)"; exit 1;; esac
command -v sha256sum >/dev/null || { echo "sha256sum is required (coreutils)"; exit 1; }
command -v openssl >/dev/null || { echo "openssl is required to check the release signature (apt-get install openssl)"; exit 1; }
[ -n "$RELEASE_PUB" ] || { echo "$SIGFAIL"; echo "(this installer was built without a release key)"; exit 1; }
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
get(){ curl -fsSL --retry 3 -o "$T/$1" "$FROM/$1" || { [ "$1" = SHA256SUMS.sig ] && echo "$SIGFAIL"; echo "download failed: $FROM/$1"; exit 1; }; }
get SHA256SUMS
get SHA256SUMS.sig
printf '%s\n' "$RELEASE_PUB" > "$T/release.pub"
openssl pkeyutl -verify -pubin -inkey "$T/release.pub" -rawin -in "$T/SHA256SUMS" -sigfile "$T/SHA256SUMS.sig" >/dev/null 2>&1 || { echo "$SIGFAIL"; exit 1; }
for b in csnode cs csnode-authkeys cs-bench; do get "$b-linux-$A"; done
( cd "$T" && grep -E "[ *](csnode|cs|csnode-authkeys|cs-bench)-linux-$A\$" SHA256SUMS | sha256sum -c - ) || { echo "$SIGFAIL"; echo "checksum FAILED; nothing installed"; exit 1; }
for b in csnode cs csnode-authkeys; do install -m 0755 "$T/$b-linux-$A" "$PREFIX/$b"; done
# card 018: the helper `cs bench --run` uploads into a space keeps its arch in the name
install -m 0755 "$T/cs-bench-linux-$A" "$PREFIX/cs-bench-linux-$A"
echo "installed: $("$PREFIX/csnode" version 2>/dev/null || echo csnode)"
[ "${CS_NO_SETUP:-0}" = 1 ] && { echo "CS_NO_SETUP=1: skipping csnode setup"; exit 0; }
exec "$PREFIX/csnode" setup "$@" </dev/tty
