// Package build is the build engine: parse -> execute steps -> write manifest.
package build

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Vidyu8/docksmith/internal/layer"
	"github.com/Vidyu8/docksmith/internal/runtime"
	"github.com/Vidyu8/docksmith/internal/store"
)

type Options struct {
	Context string // directory containing the Docksmithfile and COPY sources
	Name    string
	Tag     string
	NoCache bool // skip all cache lookups AND writes (layers are still stored)
	Out     io.Writer
}

type state struct {
	base      *store.Manifest
	layers    []store.LayerRef
	env       []string
	workdir   string
	cmd       []string
	prev      string // digest of the previous layer-producing step (base manifest digest at first)
	rootfs    string // temp working filesystem, created lazily
	extracted int    // how many of s.layers are already extracted into rootfs

	useCache bool
	cache    cacheIndex
	cascade  bool // a miss happened: every later step must also miss
	allHits  bool // true while every layer-producing step has been a cache hit
}

func (s *state) cleanup() {
	if s.rootfs != "" {
		os.RemoveAll(s.rootfs)
	}
}

// prepare makes the working rootfs reflect all layers so far, and silently
// creates WORKDIR (never part of any layer delta). It is lazy so that a fully
// cached build never extracts anything.
func (s *state) prepare() error {
	if s.rootfs == "" {
		tmp, err := os.MkdirTemp("", "docksmith-build-")
		if err != nil {
			return err
		}
		s.rootfs = tmp
	}
	if s.extracted < len(s.layers) {
		var ds []string
		for _, l := range s.layers[s.extracted:] {
			ds = append(ds, l.Digest)
		}
		if err := layer.ExtractLayers(ds, s.rootfs); err != nil {
			return err
		}
		s.extracted = len(s.layers)
	}
	if s.workdir != "" {
		if err := os.MkdirAll(filepath.Join(s.rootfs, s.workdir), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- COPY

func (s *state) resolveDest(raw string) (dest string, trailingSlash bool) {
	trailingSlash = strings.HasSuffix(raw, "/")
	d := raw
	if !path.IsAbs(d) {
		base := s.workdir
		if base == "" {
			base = "/"
		}
		d = path.Join(base, d)
	}
	return path.Clean(d), trailingSlash
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// planCopy maps copy items to image paths and creates any missing ancestor
// directories in the working rootfs, returning them as tar entries too so
// the layer is a true delta (dirs that already exist are NOT included).
func (s *state) planCopy(items []copyItem, nsrc int, rawDest string) ([]layer.Entry, error) {
	dest, trailing := s.resolveDest(rawDest)
	destIsDir := trailing || nsrc > 1 || isDir(filepath.Join(s.rootfs, dest))

	var entries []layer.Entry
	want := map[string]bool{}
	for _, it := range items {
		var name string
		switch {
		case it.Rel != "":
			name = path.Join(dest, it.Rel)
			want[dest] = true
		case destIsDir:
			name = path.Join(dest, path.Base(it.Ctx))
			want[dest] = true
		default:
			name = dest
		}
		if name == "/" {
			return nil, fmt.Errorf("invalid destination %q", rawDest)
		}
		for d := path.Dir(name); d != "/" && d != "."; d = path.Dir(d) {
			want[d] = true
		}
		entries = append(entries, layer.Entry{Name: name, Src: it.Src})
	}

	dirs := make([]string, 0, len(want))
	for d := range want {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs) // parents sort before children
	var made []layer.Entry
	for _, d := range dirs {
		p := filepath.Join(s.rootfs, d)
		if _, err := os.Lstat(p); err == nil {
			continue
		}
		if err := os.Mkdir(p, 0o755); err != nil {
			return nil, err
		}
		made = append(made, layer.Entry{Name: d, Src: p})
	}
	// context entries come last so they win on duplicate names
	return append(made, entries...), nil
}

func (s *state) execCopy(ins Instruction, items []copyItem, nsrc int, rawDest string) (store.LayerRef, error) {
	if err := s.prepare(); err != nil {
		return store.LayerRef{}, err
	}
	entries, err := s.planCopy(items, nsrc, rawDest)
	if err != nil {
		return store.LayerRef{}, err
	}
	digest, size, err := layer.BuildTar(entries)
	if err != nil {
		return store.LayerRef{}, err
	}
	// apply the new layer to the working rootfs so later steps see it
	if err := layer.ExtractLayers([]string{digest}, s.rootfs); err != nil {
		return store.LayerRef{}, err
	}
	return store.LayerRef{Digest: digest, Size: size, CreatedBy: ins.Raw}, nil
}

// ----------------------------------------------------------------- RUN

// execRun runs `RUN <cmd>` inside the working rootfs using the SAME isolation
// primitive as `docksmith run`, then turns "what changed" into a delta layer:
// snapshot before -> run -> snapshot after -> tar the difference.
// Returns the layer and the command's captured output.
func (s *state) execRun(ins Instruction) (store.LayerRef, string, error) {
	if err := s.prepare(); err != nil {
		return store.LayerRef{}, "", err
	}
	// Mount points the container setup needs. Create them BEFORE the first
	// snapshot so they never show up as part of the layer.
	for _, d := range []string{"proc", "dev"} {
		if err := os.MkdirAll(filepath.Join(s.rootfs, d), 0o755); err != nil {
			return store.LayerRef{}, "", err
		}
	}

	before, err := takeSnapshot(s.rootfs)
	if err != nil {
		return store.LayerRef{}, "", err
	}

	var out bytes.Buffer // stdout and stderr share one buffer -> ordered output
	code, err := runtime.RunIO(
		s.rootfs, s.workdir,
		append([]string(nil), s.env...), // image ENV is injected into every RUN
		[]string{"/bin/sh", "-c", ins.Args},
		nil, &out, &out,
	)
	if err != nil {
		return store.LayerRef{}, out.String(), err
	}
	if code != 0 {
		return store.LayerRef{}, out.String(), fmt.Errorf("command exited with code %d", code)
	}

	after, err := takeSnapshot(s.rootfs)
	if err != nil {
		return store.LayerRef{}, out.String(), err
	}
	var entries []layer.Entry
	for _, p := range changedPaths(before, after) {
		entries = append(entries, layer.Entry{Name: p, Src: filepath.Join(s.rootfs, p)})
	}
	// An empty delta is fine: it becomes a valid (empty) layer, so the layer
	// chain used by the cache stays one-layer-per-COPY/RUN.
	digest, size, err := layer.BuildTar(entries)
	if err != nil {
		return store.LayerRef{}, out.String(), err
	}
	return store.LayerRef{Digest: digest, Size: size, CreatedBy: ins.Raw}, out.String(), nil
}

func printOutput(w io.Writer, out string) {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return
	}
	for _, ln := range strings.Split(out, "\n") {
		fmt.Fprintf(w, "    %s\n", ln)
	}
}

// --------------------------------------------------------------- cache

// lookup returns the cached layer for key, but only if caching is on, no
// earlier step has missed (cascade), and the layer file is still on disk.
func (s *state) lookup(key string) (store.LayerRef, bool) {
	if !s.useCache || s.cascade {
		return store.LayerRef{}, false
	}
	d, ok := s.cache[key]
	if !ok {
		return store.LayerRef{}, false
	}
	fi, err := os.Stat(store.LayerPath(d))
	if err != nil {
		return store.LayerRef{}, false
	}
	return store.LayerRef{Digest: d, Size: fi.Size()}, true
}

// layerStep is the shared flow for COPY and RUN:
//
//	compute key -> hit? reuse layer : execute, store layer, record key
func (s *state) layerStep(o Options, ins Instruction, label, copyHash string,
	exec func() (store.LayerRef, string, error)) error {
	t0 := time.Now()
	key := cacheKey(s.prev, ins.Raw, s.workdir, s.env, copyHash)

	if ref, ok := s.lookup(key); ok {
		ref.CreatedBy = ins.Raw
		s.layers = append(s.layers, ref)
		s.prev = ref.Digest
		fmt.Fprintf(o.Out, "%s [CACHE HIT] %.2fs\n", label, time.Since(t0).Seconds())
		return nil
	}

	s.cascade = true // from here on, every layer step is a miss
	s.allHits = false
	ref, out, err := exec()
	if err != nil {
		fmt.Fprintf(o.Out, "%s [FAILED]\n", label)
		printOutput(o.Out, out)
		return fmt.Errorf("line %d: %s: %v", ins.Line, ins.Op, err)
	}
	s.layers = append(s.layers, ref)
	s.extracted = len(s.layers) // rootfs already contains this layer's effects
	s.prev = ref.Digest
	if s.useCache {
		s.cache[key] = ref.Digest
		if err := s.cache.save(); err != nil {
			return err
		}
	}
	fmt.Fprintf(o.Out, "%s [CACHE MISS] %.2fs\n", label, time.Since(t0).Seconds())
	printOutput(o.Out, out)
	return nil
}

// --------------------------------------------------------------- Build

// Build runs a Docksmithfile and writes the image manifest.
func Build(o Options) (*store.Manifest, error) {
	start := time.Now()
	f, err := os.Open(filepath.Join(o.Context, "Docksmithfile"))
	if err != nil {
		return nil, fmt.Errorf("no Docksmithfile in %s", o.Context)
	}
	insts, err := Parse(f)
	f.Close()
	if err != nil {
		return nil, err
	}

	s := &state{allHits: true, useCache: !o.NoCache, cache: cacheIndex{}}
	if s.useCache {
		if s.cache, err = loadCache(); err != nil {
			return nil, err
		}
	}
	defer s.cleanup()
	total := len(insts)

	for i, ins := range insts {
		label := fmt.Sprintf("Step %d/%d : %s", i+1, total, ins.Raw)
		switch ins.Op {
		case "FROM":
			fmt.Fprintln(o.Out, label)
			name, tag := store.ParseRef(ins.Args)
			base, err := store.Load(name, tag)
			if err != nil {
				return nil, fmt.Errorf("line %d: base image %q not found in local store", ins.Line, ins.Args)
			}
			s.base = base
			s.layers = append([]store.LayerRef(nil), base.Layers...)
			s.env = append([]string(nil), base.Config.Env...)
			s.workdir = base.Config.WorkingDir
			s.cmd = base.Config.Cmd
			s.prev = base.Digest // FROM changes invalidate everything downstream

		case "WORKDIR":
			fmt.Fprintln(o.Out, label)
			p := ins.Args
			if !path.IsAbs(p) {
				b := s.workdir
				if b == "" {
					b = "/"
				}
				p = path.Join(b, p)
			}
			s.workdir = path.Clean(p)

		case "ENV":
			fmt.Fprintln(o.Out, label)
			s.env = store.SetEnv(s.env, ParseEnv(ins.Args))

		case "CMD":
			fmt.Fprintln(o.Out, label)
			s.cmd, _ = ParseCmd(ins.Args) // validated by Parse

		case "COPY":
			fields := strings.Fields(ins.Args)
			srcs, rawDest := fields[:len(fields)-1], fields[len(fields)-1]
			var items []copyItem
			for _, sp := range srcs {
				its, err := expandSource(o.Context, sp)
				if err != nil {
					return nil, fmt.Errorf("line %d: COPY: %v", ins.Line, err)
				}
				items = append(items, its...)
			}
			sort.Slice(items, func(a, b int) bool { return items[a].Ctx < items[b].Ctx })
			srcHash, err := hashItems(items)
			if err != nil {
				return nil, fmt.Errorf("line %d: COPY: %v", ins.Line, err)
			}
			err = s.layerStep(o, ins, label, srcHash, func() (store.LayerRef, string, error) {
				ref, err := s.execCopy(ins, items, len(srcs), rawDest)
				return ref, "", err
			})
			if err != nil {
				return nil, err
			}

		case "RUN":
			err := s.layerStep(o, ins, label, "", func() (store.LayerRef, string, error) {
				return s.execRun(ins)
			})
			if err != nil {
				return nil, err
			}
		}
	}

	// `created` is set at first build and preserved when every layer step was
	// a cache hit, so an unchanged rebuild yields a byte-identical manifest.
	created := time.Now().UTC().Format(time.RFC3339)
	if s.allHits {
		if old, err := store.Load(o.Name, o.Tag); err == nil && old.Created != "" {
			created = old.Created
		}
	}
	m := &store.Manifest{
		Name:    o.Name,
		Tag:     o.Tag,
		Created: created,
		Config:  store.Config{Env: s.env, Cmd: s.cmd, WorkingDir: s.workdir},
		Layers:  s.layers,
	}
	if err := store.Save(m); err != nil {
		return nil, err
	}
	fmt.Fprintf(o.Out, "Successfully built %s %s:%s (%.2fs)\n",
		m.Digest[:15], o.Name, o.Tag, time.Since(start).Seconds())
	return m, nil
}
