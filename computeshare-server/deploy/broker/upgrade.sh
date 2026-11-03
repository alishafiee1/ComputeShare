#!/usr/bin/env bash
# Upgrade csbroker on this server from a signed GitHub release, with a backup and an automatic rollback.
#
#   sudo bash upgrade.sh v0.2.0            # verify, back up, install, restart, check
#   bash upgrade.sh v0.2.0 --check         # only download and verify the signature and checksum (no root, changes nothing)
#
# Needs: bash, curl, openssl 3 (Ed25519), sha256sum, systemd. The public key below is the project's release key
# (computeshare-client/internal/release/release.pub); a release not signed by it is refused.
set -euo pipefail

VER="${1:?usage: sudo bash upgrade.sh vX.Y.Z [--check]}"
MODE="${2:-}"
REPO="alishafiee1/ComputeShare"
BIN=/usr/local/bin/csbroker
CONF=/etc/csbroker.yaml
STATE=/var/lib/csbroker

PUBKEY='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAm+MPqgM4Ks4+x3EiPHSiavZH1UtHgpmDyjA+/T5Z0Vg=
-----END PUBLIC KEY-----'

die() { echo "upgrade: $*" >&2; exit 1; }
step() { echo "== $*"; }

case "$(uname -m)" in x86_64) ARCH=amd64 ;; aarch64) ARCH=arm64 ;; *) die "unsupported CPU $(uname -m)" ;; esac
for c in curl openssl sha256sum; do command -v "$c" >/dev/null || die "$c is not installed"; done

W="$(mktemp -d)"; trap 'rm -rf "$W"' EXIT
BASE="https://github.com/$REPO/releases/download/$VER"
NAME="csbroker-linux-$ARCH"

step "1/6 download $VER"
for f in "$NAME" SHA256SUMS SHA256SUMS.sig; do curl -fsSL -o "$W/$f" "$BASE/$f" || die "could not download $f"; done

step "2/6 verify the signature and the checksum"
printf '%s\n' "$PUBKEY" > "$W/release.pub"
openssl pkeyutl -verify -pubin -inkey "$W/release.pub" -rawin -in "$W/SHA256SUMS" -sigfile "$W/SHA256SUMS.sig" >/dev/null \
  || die "SIGNATURE DOES NOT MATCH: refusing to install"
(cd "$W" && grep " \*\?$NAME\$" SHA256SUMS | sha256sum -c - >/dev/null) || die "CHECKSUM DOES NOT MATCH: refusing to install"
NEWSHA="$(sha256sum "$W/$NAME" | cut -d' ' -f1)"
echo "signature OK, checksum OK ($NEWSHA)"
[ "$MODE" = "--check" ] && { echo "check only: nothing was installed"; exit 0; }

[ "$(id -u)" = 0 ] || die "run as root (sudo) to install"
chmod +x "$W/$NAME"; "$W/$NAME" version >/dev/null || die "the new binary does not run on this machine"

step "3/6 back up (the service is stopped for a consistent copy)"
OLDSHA="$(sha256sum "$BIN" | cut -d' ' -f1)"
B="/var/backups/csbroker-$(date +%Y%m%d-%H%M%S)"; mkdir -p "$B"
systemctl stop csbroker
cp -a "$BIN" "$B/csbroker.bin"; cp -a "$CONF" "$B/"; cp -a "$STATE" "$B/state"
echo "backup in $B (old binary sha $OLDSHA)"

step "4/6 install and start"
install -m 0755 "$W/$NAME" "$BIN"
systemctl start csbroker

step "5/6 check"
ok=0
for _ in 1 2 3 4 5 6 7 8 9 10; do sleep 2; systemctl is-active --quiet csbroker && { ok=1; break; }; done
if [ "$ok" != 1 ]; then
  echo "the service did not come up: rolling back" >&2
  install -m 0755 "$B/csbroker.bin" "$BIN"; systemctl restart csbroker || true
  die "rolled back to the old binary; see: journalctl -u csbroker -n 50"
fi
"$BIN" version

step "6/6 done"
echo "installed $VER (sha $NEWSHA). To roll back:"
echo "  sudo systemctl stop csbroker && sudo install -m 0755 $B/csbroker.bin $BIN && sudo systemctl start csbroker"
echo "  (state copy, only if needed: $B/state)"
