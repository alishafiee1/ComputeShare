#!/usr/bin/env bash
# broker 005 R1: fill $ROOT/{apt,images,models,installers,csnode,pypi,npm} from upstream.
# Run on a host WITH international access, then rsync $ROOT to the Iran server. Idempotent + resumable
# (wget -N / rsync --partial / debmirror state). Nothing here runs on the broker.
set -euo pipefail
ROOT=${ROOT:-/srv/mirror}; ARCH=${ARCH:-amd64}; DISTS=${DISTS:-bookworm,bookworm-backports}
CSNODE_RELEASE=${CSNODE_RELEASE:?dir with csnode binaries, SHA256SUMS, SHA256SUMS.sig and install.sh (a signed release, see RELEASING.md)}
MODELS=${MODELS:-"hermes3:8b-q4_K_M whisper:small"}
mkdir -p "$ROOT"/{apt,images,models,installers,csnode}
step(){ echo "== $*"; }

step "apt (Debian main for spaces + incus)"
debmirror --progress --nosource --method=http --host=deb.debian.org --root=debian \
  --dist="$DISTS" --section=main,contrib,non-free-firmware --arch="$ARCH" \
  --rsync-extra=none --no-check-gpg --getcontents=false "$ROOT/apt/debian"

step "images (simplestreams subset: debian/12 container)"
command -v incus-simplestreams >/dev/null || { echo "need incus-simplestreams (incus package)"; exit 1; }
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
incus image export images:debian/12 "$T/debian12" --vm=false 2>/dev/null || incus image copy images:debian/12 local: --alias cs-debian12 && incus image export cs-debian12 "$T/debian12"
( cd "$ROOT/images" && for f in "$T"/debian12*; do incus-simplestreams add . "$f" 2>/dev/null || true; done )
# client 014: list what was mirrored; the owner checks and signs it OFFLINE as images.lock(.sig).
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
FP=$(incus image info images:debian/12 | awk '/^Fingerprint:/{print $2}')
printf '# alias arch fingerprint (generated %s)\ndebian/12 %s %s\n' "$(date -u +%F)" "$ARCH" "$FP" > "$ROOT/images/images.lock.unsigned"
echo "next: copy images/images.lock.unsigned to your laptop, rename to images.lock, run 'cs release sign images.lock', put images.lock and images.lock.sig into $ROOT/images/"

step "installers (WSL rootfs + Windows setup, if built)"
for f in ${INSTALLERS:-}; do rsync -a --partial "$f" "$ROOT/installers/"; done

step "csnode binaries + release signature (Ed25519 over SHA256SUMS, client card 012)"
for f in SHA256SUMS SHA256SUMS.sig install.sh; do [ -f "$CSNODE_RELEASE/$f" ] || { echo "missing $CSNODE_RELEASE/$f: build and sign a release first"; exit 1; }; done
rsync -a --partial "$CSNODE_RELEASE"/ "$ROOT/csnode/"
# the installer checks the signature itself; here only make sure the checksums match the files we copy
( cd "$ROOT/csnode" && sha256sum -c --ignore-missing SHA256SUMS ) || { echo "checksum mismatch in $ROOT/csnode"; exit 1; }

step "models"
for m in $MODELS; do
  d="$ROOT/models/${m%%:*}-${m#*:}"; mkdir -p "$d"
  case $m in
    whisper:*) wget -q -N -P "$d" "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-${m#*:}.bin" ;;
    *) [ -n "${MODEL_URL_hermes:-}" ] && wget -q -c -N -P "$d" "$MODEL_URL_hermes" ;;
  esac
  ( cd "$d" && ls | grep -v SHA256SUMS | xargs -r sha256sum > SHA256SUMS )
done

# one installer, two URLs: https://SERVER/install.sh (nginx root /srv) and https://SERVER/mirror/csnode/install.sh
cp "$ROOT/csnode/install.sh" "$ROOT/../install.sh"
date -u +%FT%TZ > "$ROOT/READY"
step "done: $(du -sh "$ROOT" | cut -f1)"
