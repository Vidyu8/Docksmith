#!/usr/bin/env bash
# Run ONCE (needs network). Downloads Alpine minirootfs and unpacks a scratch
# copy at /tmp/alpine-rootfs for the Day-1 isolation spike.
# Day 2 replaces the unpack with `docksmith import` (tar -> layer + manifest).
set -euo pipefail
VER=3.18.4
case "$(uname -m)" in
  x86_64)  ARCH=x86_64 ;;
  aarch64) ARCH=aarch64 ;;
  *) echo "unsupported arch"; exit 1 ;;
esac
DEST="${DOCKSMITH_HOME:-$HOME/.docksmith}/base"
mkdir -p "$DEST"
TAR="$DEST/alpine-minirootfs-$VER-$ARCH.tar.gz"
[ -f "$TAR" ] || curl -fL -o "$TAR" \
  "https://dl-cdn.alpinelinux.org/alpine/v3.18/releases/$ARCH/alpine-minirootfs-$VER-$ARCH.tar.gz"
sudo rm -rf /tmp/alpine-rootfs && sudo mkdir -p /tmp/alpine-rootfs
sudo tar -xzf "$TAR" -C /tmp/alpine-rootfs
echo "base tarball: $TAR"
echo "scratch rootfs: /tmp/alpine-rootfs"
