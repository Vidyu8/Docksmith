#!/usr/bin/env bash
# Isolation check (namespaces + chroot). Run as root: sudo ./scripts/day1-check.sh
set -u
R=/tmp/alpine-rootfs
rm -f /leak.txt
./docksmith dev-run $R /bin/sh -c 'echo secret > /leak.txt; echo "hostname=$(hostname)"; echo "pid=$$"; ls /; ls /home; pwd; echo "HOME=$HOME"'
echo "--- host checks ---"
[ -e /leak.txt ] && echo "FAIL: /leak.txt on host" || echo "PASS: /leak.txt not on host"
[ -e $R/leak.txt ] && echo "PASS: file landed only inside rootfs" || echo "FAIL: file missing from rootfs"
./docksmith dev-run $R /bin/sh -c 'exit 7'
echo "--- network (expect only lo) ---"
./docksmith dev-run $R /bin/sh -c 'tail -n +3 /proc/net/dev | cut -d: -f1'
