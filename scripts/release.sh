#!/usr/bin/env bash
# ComputeShare release script (card 018 R5). Run on the OWNER's computer from anywhere inside the repo:
#
#   scripts/release.sh 0.2.0                     # test, build, sign (asks for the key passphrase), tag, draft release
#   scripts/release.sh 0.2.0 --no-sign --no-upload --allow-dirty --reproducible-check   # dry run: build only
#
# Steps, in order, stopping at the first failure:
#   1. checks: version looks like X.Y.Z, branch is main, tree is clean, release key exists in the repo
#   2. tests: go vet + go test for the client and the server
#   3. build: dist/ with csnode, cs, csnode-authkeys, cs-bench, csbroker, install.sh (public key inserted) and SHA256SUMS
#   4. sign: `cs release sign dist/SHA256SUMS` -> SHA256SUMS.sig, then `cs release verify` (the passphrase is typed by
#      you in this terminal; the script never sees it, stores it or prints it)
#   5. tag: annotated tag vX.Y.Z, pushed together with the draft release (`gh release create --draft`)
# With --rootfs FILE (csnode-linux-rootfs.tar.gz, built on Linux by computeshare-client/deploy/mirror/wsl-rootfs/build-from-dist.sh)
# the rootfs is signed with the rest, and after signing the Windows installer csnode-setup-X.Y.Z.exe is built with Inno Setup.
# The owner publishes the draft by hand after looking at it. The private key never leaves this computer.
set -euo pipefail

die() { echo "release: $*" >&2; exit 1; }
step() { echo "== $*"; }

[ $# -ge 1 ] || die "usage: scripts/release.sh X.Y.Z [--no-sign] [--no-upload] [--allow-dirty] [--skip-tests] [--reproducible-check] [--rootfs FILE]"
VER="$1"; shift
[[ "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "version must look like 0.2.0, got '$VER'"
SIGN=1; UPLOAD=1; ALLOW_DIRTY=0; TESTS=1; REPRO=0; ROOTFS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --no-sign) SIGN=0 ;;
    --no-upload) UPLOAD=0 ;;
    --allow-dirty) ALLOW_DIRTY=1 ;;
    --skip-tests) TESTS=0 ;;
    --reproducible-check) REPRO=1 ;;
    --rootfs) shift; [ $# -gt 0 ] || die "--rootfs needs a file"; ROOTFS="$1" ;;
    *) die "unknown option $1" ;;
  esac
  shift
done
[ -z "$ROOTFS" ] || [ -f "$ROOTFS" ] || die "rootfs file not found: $ROOTFS"
TAG="v$VER"

ROOT="$(git rev-parse --show-toplevel)"; cd "$ROOT"
CLIENT="$ROOT/computeshare-client"; SERVER="$ROOT/computeshare-server"
[ -d "$CLIENT" ] && [ -d "$SERVER" ] || die "run inside the ComputeShare repo (computeshare-client/ and computeshare-server/ are missing)"
command -v go >/dev/null || die "go is not installed"
command -v sha256sum >/dev/null || die "sha256sum is not installed"

step "1/5 checks"
[ "$(git branch --show-current)" = main ] || die "not on main"
if [ "$ALLOW_DIRTY" = 0 ] && [ -n "$(git status --porcelain --untracked-files=no)" ]; then die "working tree has uncommitted changes (commit them, or use --allow-dirty for a dry run)"; fi
if [ "$SIGN" = 1 ] || [ "$UPLOAD" = 1 ]; then
  if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then die "tag $TAG already exists"; fi
fi
grep -q 'BEGIN PUBLIC KEY' "$CLIENT/internal/release/release.pub" || die "computeshare-client/internal/release/release.pub holds no key (run: cs release keygen, copy release.pub there, commit)"
COMMIT="$(git rev-parse --short=7 HEAD)"
BUILDVER="$VER+$COMMIT"
echo "version $BUILDVER"

if [ "$TESTS" = 1 ]; then
  step "2/5 tests"
  (
    cd "$CLIENT"
    if [ "$(go env GOOS)" = linux ]; then
      go vet ./... && go test -count=1 ./...
    else
      # the node packages only build on Linux: type-check them for Linux, run the portable tests here
      GOOS=linux go vet ./... && go test -count=1 ./cmd/cs/... ./internal/client/... ./internal/release/... ./proto/...
    fi
  )
  ( cd "$SERVER" && go vet ./... && go test -count=1 ./internal/... ./e2e )
else
  step "2/5 tests skipped (--skip-tests)"
fi

build_into() { # build_into <outdir>
  local out="$1"; rm -rf "$out"; mkdir -p "$out"
  local ldflags="-s -w -buildid= -X main.version=$BUILDVER"
  local t o a x c
  for t in linux/amd64 linux/arm64 windows/amd64; do
    o="${t%/*}"; a="${t#*/}"; x=""; [ "$o" = windows ] && x=".exe"
    for c in csnode cs csnode-authkeys cs-bench; do
      if [ "$o" = windows ] && { [ "$c" = csnode-authkeys ] || [ "$c" = cs-bench ]; }; then continue; fi
      ( cd "$CLIENT" && CGO_ENABLED=0 GOOS="$o" GOARCH="$a" go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$out/$c-$o-$a$x" "./cmd/$c" )
    done
  done
  for a in amd64 arm64; do
    ( cd "$SERVER" && CGO_ENABLED=0 GOOS=linux GOARCH="$a" go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$out/csbroker-linux-$a" ./cmd/csbroker )
  done
  # the Windows rootfs is signed together with everything else (csnode install checks it against SHA256SUMS)
  if [ -n "$ROOTFS" ]; then cp "$ROOTFS" "$out/csnode-linux-rootfs.tar.gz"; fi
  # the installer with the release public key inserted (empty key = unsigned build, the installer refuses it)
  K="$(cat "$CLIENT/internal/release/release.pub")" awk '/^RELEASE_PUB=""$/{print "RELEASE_PUB=\"" ENVIRON["K"] "\""; next} {print}' "$CLIENT/installer/linux/install.sh" > "$out/install.sh"
  grep -q 'BEGIN PUBLIC KEY' "$out/install.sh" || die "the release key was not inserted into install.sh"
  ( cd "$out" && sha256sum $(ls | grep -v -e '^SHA256SUMS' | sort) > SHA256SUMS )
}

find_iscc() {
  local c
  for c in "$(command -v iscc 2>/dev/null || true)" "${LOCALAPPDATA:-}/Programs/Inno Setup 6/ISCC.exe" "/c/Program Files (x86)/Inno Setup 6/ISCC.exe" "/c/Program Files/Inno Setup 6/ISCC.exe"; do
    if [ -n "$c" ] && [ -x "$c" ]; then echo "$c"; return 0; fi
  done
  return 1
}

# build_installer signed|unsigned: Inno Setup needs the exes, rootfs and the SIGNED SHA256SUMS(+.sig) next to each other
build_installer() {
  local mode="$1" iscc stage
  iscc="$(find_iscc)" || die "Inno Setup (ISCC.exe) not found; install it (winget install JRSoftware.InnoSetup) or leave out --rootfs"
  stage="$(mktemp -d)"; mkdir -p "$stage/installer" "$stage/dist"
  cp -r "$CLIENT/installer/windows" "$stage/installer/windows"
  cp "$DIST/csnode-windows-amd64.exe" "$DIST/cs-windows-amd64.exe" "$stage/dist/"
  cp "$DIST/csnode-linux-rootfs.tar.gz" "$DIST/SHA256SUMS" "$stage/installer/windows/"
  if [ "$mode" = signed ]; then cp "$DIST/SHA256SUMS.sig" "$stage/installer/windows/"; else echo "UNSIGNED TEST BUILD" > "$stage/installer/windows/SHA256SUMS.sig"; fi
  ( cd "$stage/installer/windows" && VERSION="$VER" "$iscc" csnode.iss > "$stage/iscc.log" 2>&1 ) || { tail -20 "$stage/iscc.log"; die "Inno Setup failed"; }
  cp "$stage/installer/windows/Output/csnode-setup-$VER.exe" "$DIST/"
  ( cd "$DIST" && sha256sum "csnode-setup-$VER.exe" > "csnode-setup-$VER.exe.sha256" )
  echo "installer: dist/csnode-setup-$VER.exe ($mode). It is not in the signed SHA256SUMS: users get it from the release page; compare its .sha256 file."
  rm -rf "$stage"
}

step "3/5 build"
DIST="$ROOT/dist"
build_into "$DIST"
echo "built $(ls "$DIST" | grep -vc '^SHA256SUMS') files in dist/"
if [ "$REPRO" = 1 ]; then
  step "3b reproducibility: build again elsewhere and compare"
  TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
  build_into "$TMP/dist2"
  if diff -q "$DIST/SHA256SUMS" "$TMP/dist2/SHA256SUMS" >/dev/null; then echo "reproducible: two builds give the same SHA256SUMS"; else diff "$DIST/SHA256SUMS" "$TMP/dist2/SHA256SUMS" || true; die "builds differ"; fi
fi

if [ "$SIGN" = 0 ]; then
  [ -z "$ROOTFS" ] || build_installer unsigned
  echo "--no-sign: stopping after the build. Nothing was tagged or uploaded."; exit 0; fi

step "4/5 sign (you type the key passphrase now)"
CSBIN="$(mktemp -d)/cs"
( cd "$CLIENT" && CGO_ENABLED=0 go build -trimpath -o "$CSBIN" ./cmd/cs )
"$CSBIN" release sign "$DIST/SHA256SUMS"
"$CSBIN" release verify "$DIST/SHA256SUMS"
[ -f "$DIST/SHA256SUMS.sig" ] || die "no SHA256SUMS.sig after signing"
if [ -n "$ROOTFS" ]; then step "4b Windows installer"; build_installer signed; fi

step "5/5 tag and draft release"
git tag -a "$TAG" -m "$TAG"
if [ "$UPLOAD" = 0 ]; then echo "--no-upload: tag $TAG created locally; push it and create the release yourself."; exit 0; fi
command -v gh >/dev/null || die "gh (GitHub CLI) is not installed; the tag $TAG exists locally, push it and upload dist/* by hand"
git push origin "$TAG"
gh release create "$TAG" --draft --title "$TAG" --notes "ComputeShare $VER (commit $COMMIT). Verify with: cs release verify SHA256SUMS" "$DIST"/*
echo "draft release $TAG created. Look at it, then publish it. Next: mirror sync (computeshare-server/deploy/mirror/README.md) and deploy (card 019)."
