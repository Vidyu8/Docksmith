# Docksmith

A simplified Docker-like **build and runtime system built from scratch in Go**: a single daemonless CLI with a Dockerfile-style build language, immutable content-addressed layers, a deterministic build cache, and container isolation implemented directly on Linux namespaces and `chroot`. It does not use Docker, runc, containerd or any other container tooling.

```
$ docksmith build -t myapp:latest .
Step 1/8 : FROM alpine:3.18
Step 2/8 : WORKDIR /app
Step 3/8 : ENV APP_NAME=Docksmith
Step 4/8 : ENV GREETING=Hello
Step 5/8 : RUN mkdir -p /app/data && echo "dependency layer ready" > /app/data/setup.txt [CACHE MISS] 0.05s
Step 6/8 : COPY app/ /app/ [CACHE MISS] 0.00s
Step 7/8 : RUN chmod +x /app/run.sh && echo "build complete" > /app/BUILD_INFO [CACHE MISS] 0.04s
Step 8/8 : CMD ["/app/run.sh"]
Successfully built sha256:xxxxxxxx myapp:latest (0.12s)
```
*(representative output; timings and digests will differ)*

## Features

- **Build language:** `FROM`, `COPY` (with `*` and `**` globs), `RUN`, `WORKDIR`, `ENV`, `CMD`. Unknown instructions fail immediately with a line number.
- **Immutable, content-addressed layers:** every `COPY` and `RUN` produces a delta tar named by the SHA-256 of its bytes.
- **Deterministic build cache:** keys derived from the previous layer, instruction text, `WORKDIR`, `ENV` and source-file hashes, with cascading invalidation, `[CACHE HIT]`/`[CACHE MISS]` reporting and `--no-cache`.
- **Reproducible builds:** sorted tar entries and zeroed metadata give byte-identical layers; an unchanged rebuild produces a byte-identical manifest.
- **Process isolation:** mount, PID, UTS and network namespaces plus `chroot`. The *same* primitive runs build-time `RUN` steps and `docksmith run`.
- **Fully offline:** base images are imported once during setup; build and run never touch the network.

## Requirements

- Linux (use a VM such as Ubuntu on VirtualBox; WSL has quirks) and **root** (namespaces and `chroot` need it)
- Go 1.21+
- `curl` and `python3` (setup script and demo only)

## Quick start

```bash
make build                          # builds ./docksmith
./scripts/import-base.sh            # one-time: downloads Alpine minirootfs (the only networked step)

# state lives in ~/.docksmith; keep it in one place when using sudo:
alias ds='sudo DOCKSMITH_HOME=$HOME/.docksmith ./docksmith'

ds import alpine:3.18 ~/.docksmith/base/alpine-minirootfs-*.tar.gz

ds build -t myapp:latest sample     # cold build: all CACHE MISS
ds build -t myapp:latest sample     # warm build: all CACHE HIT
ds images
ds run myapp:latest
ds run -e GREETING=Namaste myapp:latest
ds rmi myapp:latest
```

Run the full eight-step demo (cold/warm/edit rebuilds, images, run, env override, host-isolation check, rmi) with PASS/FAIL checks:

```bash
make demo
```

## CLI reference

| Command | Behaviour |
|---|---|
| `docksmith build -t <name:tag> [--no-cache] <context>` | Parse `<context>/Docksmithfile`, run every step in isolation, write the image manifest. Logs each step with cache status and duration. |
| `docksmith run [-e KEY=VALUE]... <name:tag> [cmd...]` | Assemble the filesystem, run in the foreground, print the exit code, clean up. `cmd` overrides the image `CMD`; `-e` overrides image `ENV`. Fails clearly if there is no command. |
| `docksmith images` | List images: Name, Tag, ID (first 12 chars of the manifest digest), Created. |
| `docksmith rmi <name:tag>` | Remove the manifest and all of its layer files. |
| `docksmith import <name:tag> <rootfs.tar[.gz]>` | Setup only: turn a root filesystem tarball into a single-layer base image. |

## Docksmithfile reference

| Instruction | Behaviour | Layer? |
|---|---|---|
| `FROM <image>[:<tag>]` | Use a locally stored image as the base. Fails if it is not in the store. | no |
| `COPY <src>... <dest>` | Copy from the build context. Supports `*` and `**`. Creates missing directories. | yes |
| `RUN <command>` | Run `/bin/sh -c <command>` **inside** the image filesystem (isolated, no network). Image `ENV` is injected. | yes |
| `WORKDIR <path>` | Set the working directory for later steps and for the container. Created silently if missing; never stored in a layer. | no |
| `ENV <key>=<value>` | Add an environment variable to the image config and to every `RUN`. | no |
| `CMD ["exe","arg"]` | Default command (JSON array form required). | no |

Out of scope: networking, registries, resource limits, multi-stage builds, bind mounts, detached containers, daemons, `EXPOSE`/`VOLUME`/`ADD`/`ARG`/`ENTRYPOINT`/`SHELL`.

### COPY semantics

- A **directory** source copies its *contents* into the destination (`COPY app/ /app/`).
- A **single file** goes to `dest/<name>` if the destination is directory-like (trailing `/`, an existing directory, or several sources), otherwise to `dest` itself.
- A **wildcard** source keeps each match's path relative to the pattern's non-wildcard prefix (`src/**/*.py` preserves subdirectories). `**` matches zero or more path segments.
- A relative destination resolves against the current `WORKDIR`. Sources may not escape the build context, and a source that matches nothing fails the build.

## Using Docksmith with your own project

Anyone with a Linux machine and root can build and run their own project. After the [Quick start](#quick-start) setup:

```bash
mkdir myproj && cd myproj
printf '#!/bin/sh\necho "hello $NAME"\n' > hello.sh
cat > Docksmithfile <<'EOF'
FROM alpine:3.18
WORKDIR /srv
ENV NAME=world
COPY hello.sh .
RUN chmod +x hello.sh
CMD ["/srv/hello.sh"]
EOF
cd ..

ds build -t mine:1 myproj
ds run mine:1                  # hello world
ds run -e NAME=you mine:1      # hello you
```

### Constraints

- **Linux and root only.**
- **No network during build or run.** `RUN apk add ...` or `pip install ...` will fail. Every dependency must be in the build context and brought in with `COPY`.
- **The base image is minimal Alpine** (busybox `sh` and core utilities): no Python, Node or compilers unless you import a different base image (see below).
- **Only the six instructions** are supported; anything else fails with a line number.

### What you can package

The Docksmithfile is not limited to tiny scripts: whole directory trees, many files, globs (`COPY **/*.txt /data/`) and chains of `RUN` steps (each cached as its own layer) all work. The limits come from the base image and the offline rule.

| Goal | Works? | How |
|---|---|---|
| Multi-file shell projects, text and data processing with busybox tools | Yes | `COPY` the tree, run it from `CMD` |
| Compiled programs | Yes | Build a **statically linked** binary on the host (e.g. `CGO_ENABLED=0 go build -o myapp .`), `COPY` it in |
| Python or Node apps | With a different base | Import a Python/Node root filesystem tarball as the base (see below) |
| `pip install` / `apk add` during the build | No (no network) | Download packages on the host first (e.g. `pip install --target libs`), `COPY` them in |
| Servers you open in a browser | No | There is no network access from inside the container; CLI programs and batch jobs work |

### Other base images

`docksmith import <name:tag> <rootfs.tar[.gz]>` accepts any root filesystem tarball. For example, export a Python image's filesystem once (`docker export` from a container works) and import it. This is a one-time setup step, not part of build or run, so builds and runs stay fully offline.

## How it works

### Architecture

```
Docksmithfile + context ──► build engine ──► layers/<sha256>.tar  (immutable deltas)
                                  │          images/<name>_<tag>.json  (manifest)
                                  │          cache/index.json  (key -> layer digest)
                                  ▼
 docksmith run ◄── extract layers in order into a temp dir ◄── manifest
        │
        └─► runtime.Run ─► re-exec self in new namespaces ─► chroot ─► exec command
```

Everything is a plain CLI: there is no daemon, and all state lives in `~/.docksmith/`.

```
~/.docksmith/
  images/   one JSON manifest per image
  layers/   content-addressed tar files named by digest
  cache/    index mapping cache keys to layer digests
```

### Layers and determinism

A layer is a tar containing only the files a step added or modified (a delta). Layers are named by the SHA-256 of their raw bytes, written to a temp file while hashing, then atomically renamed into place and made read-only. Identical content therefore maps to one file.

Reproducibility comes from removing every source of noise from the tar: entries sorted by path, mtime fixed to the epoch, atime/ctime and uid/gid/names zeroed, one fixed header format, and no compression (gzip embeds timestamps).

### Manifest and its digest

The manifest lists name, tag, creation time, config (`Env`, `Cmd`, `WorkingDir`) and the ordered layers (`digest`, `size`, `createdBy`). The digest is computed over the manifest serialised with `"digest": ""` and then written into the final file, so it can be verified by blanking the field and re-hashing. `created` is set at first build and preserved when every step is a cache hit, so an unchanged rebuild is byte-identical.

### Build cache

Before each `COPY`/`RUN` a key is computed as the SHA-256 of length-prefixed fields:

1. digest of the previous layer-producing step (the base image's *manifest* digest for the first one, so a changed `FROM` invalidates everything)
2. the full instruction text as written
3. the current `WORKDIR`
4. all `ENV` pairs so far, sorted by key
5. `COPY` only: per-file SHA-256 of the sources in sorted path order (path and permission bits are mixed in too, so a `chmod` also invalidates)

A hit needs both the index entry **and** the layer file on disk. After the first miss every later step is a miss (cascade). `--no-cache` skips all lookups and writes but still stores layers. `WORKDIR`, `ENV` and `CMD` do not produce layers but feed later keys.

### Isolation (one primitive, two users)

`runtime.RunIO` re-executes the docksmith binary itself (`/proc/self/exe __child ...`) with `CLONE_NEWNS | CLONE_NEWPID | CLONE_NEWUTS | CLONE_NEWNET`. The child, now inside the new namespaces, makes `/` mount-private, sets the hostname, `chroot`s into the assembled image directory, mounts `/proc` and a minimal tmpfs `/dev`, changes to the working directory and `exec`s the command with exactly the image environment (the host environment is not inherited). The parent waits, reports the exit code and removes the temp directory.

The re-exec step exists because the Go runtime is multi-threaded: setup code must run *inside* the new namespaces before the target program starts, and Go cannot safely run code between `fork` and `exec`.

Layer extraction runs in the parent on the host, before any `chroot`, so it guards against hostile archives: `..` components are neutralised and writing through a symlinked parent directory is refused.

### RUN: snapshot diff

A `RUN` layer must contain only what the command changed, so the build snapshots the working filesystem (type, permission bits, content hash or link target per path; timestamps ignored), runs the command through the isolation primitive, snapshots again, and tars the added or modified paths. Non-zero exit codes fail the build with the line number. An unchanged filesystem yields a valid empty layer, which keeps the cache chain one-layer-per-step.

## Testing

```bash
make test        # unit + integration tests, no root required
make vet
./scripts/isolation-check.sh        # (root) namespaces + chroot: host unaffected
./scripts/build-run-check.sh        # (root) RUN deltas, /dev handling, failing RUN
make demo                           # (root) the full 8-step demo with PASS/FAIL
```

`go test` covers deterministic tar output (including mtime independence), extraction round-trips, manifest digest verification, the glob matcher and COPY expansion, the parser and its error messages, snapshot diffing, cache-key ingredients, and end-to-end cache behaviour: cold, warm, edited source, cascade, `ENV` change, `--no-cache`, a missing layer file and manifest stability across warm rebuilds.

## Project layout

```
cmd/docksmith/        CLI entry point (build, run, images, rmi, import)
internal/build/       Docksmithfile parser, build engine, COPY/glob, RUN snapshot diff, cache
internal/layer/       deterministic tar writer, safe extraction
internal/runtime/     the isolation primitive (namespaces + chroot)
internal/container/   assemble an image and run it
internal/store/       ~/.docksmith paths, manifests, digests, env helpers
sample/               demo app that uses all six instructions
scripts/              base-image import, isolation/RUN checks, demo
```

## Limitations

- **`chroot` + namespaces is not a security sandbox.** The container's root is the host's root (no user namespace, no dropped capabilities, no seccomp, no `pivot_root`, no cgroups), so a determined root process can escape. It satisfies the isolation requirement (writes stay out of the host filesystem) but must not be used to run untrusted code.
- **No whiteouts:** a step cannot delete files from earlier layers; only added/modified files are recorded.
- Device nodes, fifos and sockets are not stored in layers.
- `COPY` paths containing spaces and `Docksmithfile` line continuations are not supported.
- Layers are not reference-counted: `rmi` removes the layer files of the image, including ones shared with other images (documented, expected behaviour).
- `RUN` commands that are not deterministic (timestamps, randomness) produce different layers when re-executed; the cache avoids this on unchanged rebuilds.
- Linux only; requires root.
