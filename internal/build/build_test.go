package build

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vidyu8/docksmith/internal/layer"
	"github.com/Vidyu8/docksmith/internal/store"
)

// makeBase stores a tiny fake base image "base:1" (no root / namespaces needed).
func makeBase(t *testing.T) {
	t.Helper()
	t.Setenv("DOCKSMITH_HOME", t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "bin"), 0o755)
	os.WriteFile(filepath.Join(src, "bin", "sh"), []byte("x"), 0o755)
	es, err := layer.EntriesFromDir(src)
	if err != nil {
		t.Fatal(err)
	}
	d, size, err := layer.BuildTar(es)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Save(&store.Manifest{
		Name: "base", Tag: "1", Created: "2026-01-01T00:00:00Z",
		Layers: []store.LayerRef{{Digest: d, Size: size, CreatedBy: "test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func runBuild(t *testing.T, ctx string, noCache bool) (string, *store.Manifest) {
	t.Helper()
	var out bytes.Buffer
	m, err := Build(Options{Context: ctx, Name: "app", Tag: "t", NoCache: noCache, Out: &out})
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out.String())
	}
	return out.String(), m
}

func lineWith(out, sub string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

func TestCacheBehaviour(t *testing.T) {
	makeBase(t)
	ctx := t.TempDir()
	writeFile(t, ctx, "a.txt", "A")
	writeFile(t, ctx, "b.txt", "B")
	dockerfile := func(env string) {
		writeFile(t, ctx, "Docksmithfile",
			"FROM base:1\nWORKDIR /app\nENV K="+env+"\nCOPY a.txt /app/\nCOPY b.txt /app/\nCMD [\"x\"]\n")
	}
	dockerfile("1")

	// 1. cold build: everything misses
	out, m1 := runBuild(t, ctx, false)
	if strings.Count(out, "[CACHE MISS]") != 2 || strings.Contains(out, "[CACHE HIT]") {
		t.Fatalf("cold build should be all misses:\n%s", out)
	}

	// 2. warm build: everything hits, manifest identical (created preserved)
	time.Sleep(1100 * time.Millisecond) // prove `created` is preserved, not regenerated
	out, m2 := runBuild(t, ctx, false)
	if strings.Count(out, "[CACHE HIT]") != 2 || strings.Contains(out, "[CACHE MISS]") {
		t.Fatalf("warm build should be all hits:\n%s", out)
	}
	if m1.Digest != m2.Digest || m1.Created != m2.Created {
		t.Fatalf("warm rebuild must keep manifest identical: %s/%s vs %s/%s",
			m1.Digest, m1.Created, m2.Digest, m2.Created)
	}

	// 3. edit the second source: first step still hits, second misses
	writeFile(t, ctx, "b.txt", "B2")
	out, _ = runBuild(t, ctx, false)
	if !strings.Contains(lineWith(out, "COPY a.txt"), "[CACHE HIT]") ||
		!strings.Contains(lineWith(out, "COPY b.txt"), "[CACHE MISS]") {
		t.Fatalf("only the edited step (and below) should miss:\n%s", out)
	}

	// 4. editing the FIRST source cascades: both miss
	writeFile(t, ctx, "a.txt", "A2")
	out, _ = runBuild(t, ctx, false)
	if strings.Count(out, "[CACHE MISS]") != 2 {
		t.Fatalf("a miss must cascade to later steps:\n%s", out)
	}

	// 5. ENV change invalidates the steps after it
	dockerfile("2")
	out, _ = runBuild(t, ctx, false)
	if strings.Count(out, "[CACHE MISS]") != 2 {
		t.Fatalf("changed ENV must invalidate later steps:\n%s", out)
	}

	// 6. --no-cache: all misses even though everything is cached
	out, m3 := runBuild(t, ctx, false)
	if strings.Count(out, "[CACHE HIT]") != 2 {
		t.Fatalf("expected warm build before --no-cache check:\n%s", out)
	}
	out, _ = runBuild(t, ctx, true)
	if strings.Count(out, "[CACHE MISS]") != 2 || strings.Contains(out, "[CACHE HIT]") {
		t.Fatalf("--no-cache must miss everywhere:\n%s", out)
	}

	// 7. a missing layer file is a miss, and cascades
	if err := os.Remove(store.LayerPath(m3.Layers[1].Digest)); err != nil {
		t.Fatal(err)
	}
	out, _ = runBuild(t, ctx, false)
	if strings.Count(out, "[CACHE MISS]") != 2 {
		t.Fatalf("missing layer file must miss and cascade:\n%s", out)
	}
	if _, err := os.Stat(store.LayerPath(m3.Layers[1].Digest)); err != nil {
		t.Fatal("rebuilt layer must be written back with the same digest (determinism)")
	}
}

func TestBuildErrors(t *testing.T) {
	makeBase(t)
	ctx := t.TempDir()
	var out bytes.Buffer

	writeFile(t, ctx, "Docksmithfile", "FROM nope:1\n")
	if _, err := Build(Options{Context: ctx, Name: "x", Tag: "t", Out: &out}); err == nil ||
		!strings.Contains(err.Error(), "line 1") {
		t.Fatalf("missing base must fail with line number, got %v", err)
	}

	writeFile(t, ctx, "Docksmithfile", "FROM base:1\nCOPY missing.txt /x\n")
	if _, err := Build(Options{Context: ctx, Name: "x", Tag: "t", Out: &out}); err == nil ||
		!strings.Contains(err.Error(), "line 2") {
		t.Fatalf("unmatched COPY source must fail with line number, got %v", err)
	}
}
