package layer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vidyu8/docksmith/internal/store"
)

func setup(t *testing.T) string {
	t.Setenv("DOCKSMITH_HOME", t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "app", "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "app", "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(src, "app", "sub", "b.sh"), []byte("#!/bin/sh\n"), 0o755)
	os.Symlink("a.txt", filepath.Join(src, "app", "link"))
	return src
}

func build(t *testing.T, src string) string {
	es, err := EntriesFromDir(src)
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := BuildTar(es)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDeterministic(t *testing.T) {
	src := setup(t)
	d1 := build(t, src)
	// change mtimes: digest must not change
	future := time.Now().Add(48 * time.Hour)
	os.Chtimes(filepath.Join(src, "app", "a.txt"), future, future)
	d2 := build(t, src)
	if d1 != d2 {
		t.Fatalf("digest changed with mtime: %s vs %s", d1, d2)
	}
	// change content: digest must change
	os.WriteFile(filepath.Join(src, "app", "a.txt"), []byte("HELLO"), 0o644)
	if d3 := build(t, src); d3 == d1 {
		t.Fatal("digest did not change with content")
	}
}

func TestRoundTrip(t *testing.T) {
	src := setup(t)
	d := build(t, src)
	dst := t.TempDir()
	if err := ExtractLayers([]string{d}, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dst, "app", "a.txt"))
	if string(b) != "hello" {
		t.Fatalf("got %q", b)
	}
	if l, _ := os.Readlink(filepath.Join(dst, "app", "link")); l != "a.txt" {
		t.Fatalf("symlink target %q", l)
	}
	fi, _ := os.Stat(filepath.Join(dst, "app", "sub", "b.sh"))
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode())
	}
}
