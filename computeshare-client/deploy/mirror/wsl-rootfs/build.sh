#!/usr/bin/env bash
# Builds csnode-linux-rootfs.tar.gz (Debian 12 + systemd + incus + csnode) for `wsl --import`.
# Run on a Linux host with access to the mirror (MIRROR=http://<server>/mirror). Needs debootstrap.
set -euo pipefail
MIRROR=${MIRROR:-http://deb.debian.org}; OUT=${OUT:-csnode-linux-rootfs.tar.gz}; BIN=${BIN:-../../../bin}
R=$(mktemp -d); trap 'sudo rm -rf "$R"' EXIT
sudo debootstrap --variant=minbase --include=systemd,systemd-sysv,dbus,ca-certificates,iproute2,nftables,openssh-client bookworm "$R" "$MIRROR/debian"
echo "deb $MIRROR/debian bookworm-backports main" | sudo tee "$R/etc/apt/sources.list.d/backports.list" >/dev/null
sudo chroot "$R" sh -c 'apt-get update && apt-get install -y -t bookworm-backports incus && apt-get clean'
printf '[boot]\nsystemd=true\n[user]\ndefault=root\n[interop]\nappendWindowsPath=false\n' | sudo tee "$R/etc/wsl.conf" >/dev/null
sudo install -m755 "$BIN/csnode" "$BIN/csnode-authkeys" "$R/usr/local/bin/"
sudo chown root:root "$R"; sudo chmod 755 "$R"   # mktemp -d gives 0700 + the caller as owner; "/" must be 0755 root or LXC cannot start containers
sudo tar -C "$R" -czf "$OUT" .
sha256sum "$OUT"
