#!/usr/bin/env bash
# End-to-end demo of the 8 required scenarios. Run from anywhere:
#   sudo DOCKSMITH_HOME=$HOME/.docksmith ./scripts/demo.sh
set -u
: "${DOCKSMITH_HOME:?run as: sudo DOCKSMITH_HOME=\$HOME/.docksmith ./scripts/demo.sh}"
H="$DOCKSMITH_HOME"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/docksmith"
[ -x "$BIN" ] || { echo "binary missing: run 'make build' first"; exit 1; }
TAR="$(ls "$H"/base/alpine-minirootfs-*.tar.gz 2>/dev/null | head -n1)"
[ -n "$TAR" ] || { echo "base tarball missing: run ./scripts/import-base.sh first"; exit 1; }

ds()  { "$BIN" "$@"; }
hdr() { printf '\n\033[1;36m=== %s ===\033[0m\n' "$*"; }
cmd() { printf '\033[2m$ docksmith %s\033[0m\n' "$*"; ds "$@"; }
image_id() { ds images | awk '$1=="myapp" && $2=="latest" {print $3}'; }

# work on a copy so editing a source file never touches your repo
CTX="$(mktemp -d)"; trap 'rm -rf "$CTX"' EXIT
cp -r "$ROOT/sample/." "$CTX/"

# clean slate (rmi also deletes the shared base layer, so always re-import after)
ds rmi myapp:latest >/dev/null 2>&1
rm -f "$H/cache/index.json"
ds import alpine:3.18 "$TAR" >/dev/null || exit 1

hdr "1. Cold build: every layer step must be [CACHE MISS]"
cmd build -t myapp:latest "$CTX"
ID1="$(image_id)"

hdr "2. Warm build: every layer step must be [CACHE HIT]"
cmd build -t myapp:latest "$CTX"
ID2="$(image_id)"
[ -n "$ID1" ] && [ "$ID1" = "$ID2" ] \
  && echo "PASS: identical image ID on warm rebuild ($ID1)" \
  || echo "FAIL: image ID changed ($ID1 -> $ID2)"

hdr "3. Edit a source file: that step and all below MISS, steps above HIT"
echo "edited $(date +%s)" >> "$CTX/app/banner.txt"
cmd build -t myapp:latest "$CTX"

hdr "4. docksmith images"
cmd images

hdr "5. docksmith run myapp:latest"
cmd run myapp:latest

hdr "6. Environment override: -e GREETING=Namaste"
cmd run -e GREETING=Namaste myapp:latest

hdr "7. Isolation: write a file inside the container, then look for it on the host"
cmd run myapp:latest /bin/sh -c 'echo secret > /container-secret.txt && echo "inside the container: $(ls /container-secret.txt)"'
if find / -xdev -name container-secret.txt 2>/dev/null | grep -q .; then
  echo "FAIL: container-secret.txt found on the host"
else
  echo "PASS: container-secret.txt does not exist anywhere on the host"
fi
if ls -d /tmp/docksmith-run-* >/dev/null 2>&1; then
  echo "FAIL: temp run directory left behind"
else
  echo "PASS: temp run directories were cleaned up"
fi

hdr "8. docksmith rmi myapp:latest"
LAYERS="$(python3 -c "import json;m=json.load(open('$H/images/myapp_latest.json'));print(' '.join(l['digest'].split(':')[1] for l in m['layers']))")"
cmd rmi myapp:latest
ok=1
[ -e "$H/images/myapp_latest.json" ] && ok=0
for h in $LAYERS; do [ -e "$H/layers/$h.tar" ] && ok=0; done
[ $ok = 1 ] && echo "PASS: manifest and all of its layer files were removed" \
            || echo "FAIL: leftovers remain in $H"

echo
echo "(rmi also removed the shared base layer, so re-importing alpine to keep it usable)"
ds import alpine:3.18 "$TAR" >/dev/null
