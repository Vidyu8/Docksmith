#!/usr/bin/env bash
# RUN + layer-delta check. Run:  sudo DOCKSMITH_HOME=$HOME/.docksmith ./scripts/build-run-check.sh
set -u
H="${DOCKSMITH_HOME:?set DOCKSMITH_HOME}"
D=./docksmith
CTX=$(mktemp -d); CTX2=$(mktemp -d)
rm -f /leak-build.txt

cat > "$CTX/Docksmithfile" <<'EOF'
FROM alpine:3.18
ENV GREETING=world
WORKDIR /app
RUN echo "hello $GREETING" > /app/out.txt && mkdir -p /data/sub && echo x > /data/sub/f
RUN cat /app/out.txt
RUN echo hi > /dev/null && echo "from build" > /etc/motd-demo && echo leak > /leak-build.txt
CMD ["/bin/sh","-c","cat /app/out.txt; cat /data/sub/f; cat /etc/motd-demo"]
EOF

echo "=== build ==="
$D build -t demo2:latest "$CTX" || echo "BUILD FAILED (unexpected)"
echo; echo "=== run ==="
$D run demo2:latest

echo; echo "=== host isolation ==="
[ -e /leak-build.txt ] && echo "FAIL: /leak-build.txt on host" || echo "PASS: build-time write did not touch host"

echo; echo "=== layer contents (each RUN layer = only what changed) ==="
for h in $(python3 -c "import json;m=json.load(open('$H/images/demo2_latest.json'));print(' '.join(l['digest'].split(':')[1] for l in m['layers'][1:]))"); do
  echo "-- layer ${h:0:12}"; tar tf "$H/layers/$h.tar"
done
python3 - <<PY
import json,tarfile
m=json.load(open("$H/images/demo2_latest.json"))
names=[]
for l in m["layers"][1:]:
    names += tarfile.open("$H/layers/"+l["digest"].split(":")[1]+".tar").getnames()
print("PASS: /dev/null not captured in any layer" if not any(n.rstrip("/")=="dev/null" or n.startswith("dev/") for n in names) else "FAIL: dev/* leaked into a layer")
print("PASS: workdir /app not captured as a dir entry" if "app" not in [n.rstrip("/") for n in names] else "FAIL: app/ captured")
PY

echo; echo "=== failing RUN must fail the build ==="
printf 'FROM alpine:3.18\nRUN echo boom && exit 3\n' > "$CTX2/Docksmithfile"
$D build -t fail:1 "$CTX2"; echo "build exit status: $? (expect non-zero)"
$D images | grep -q '^fail' && echo "FAIL: failed build left an image" || echo "PASS: no image written for failed build"
