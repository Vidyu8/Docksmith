package build

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func m(p, s string) bool { return matchSegs(strings.Split(p, "/"), strings.Split(s, "/")) }

func TestMatchSegs(t *testing.T) {
	cases := []struct {
		p, s string
		want bool
	}{
		{"*.txt", "a.txt", true},
		{"*.txt", "d/a.txt", false},
		{"**/*.txt", "a.txt", true},
		{"**/*.txt", "d/e/a.txt", true},
		{"src/**", "src/a/b", true},
		{"src/**", "other/a", false},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/c", false},
	}
	for _, c := range cases {
		if got := m(c.p, c.s); got != c.want {
			t.Errorf("match(%q,%q)=%v want %v", c.p, c.s, got, c.want)
		}
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	full := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpand(t *testing.T) {
	ctx := t.TempDir()
	writeFile(t, ctx, "a.txt", "a")
	writeFile(t, ctx, "src/b.txt", "b")
	writeFile(t, ctx, "src/sub/c.py", "c")

	got := func(pat string) string {
		items, err := expandSource(ctx, pat)
		if err != nil {
			t.Fatalf("%s: %v", pat, err)
		}
		var o []string
		for _, it := range items {
			o = append(o, it.Ctx+"|"+it.Rel)
		}
		sort.Strings(o)
		return strings.Join(o, ",")
	}
	if g := got("**/*.txt"); g != "a.txt|a.txt,src/b.txt|src/b.txt" {
		t.Errorf("**/*.txt -> %s", g)
	}
	if g := got("src"); g != "src/b.txt|b.txt,src/sub/c.py|sub/c.py,src/sub|sub" {
		t.Errorf("src -> %s", g)
	}
	if g := got("src/*.txt"); g != "src/b.txt|b.txt" {
		t.Errorf("src/*.txt -> %s", g)
	}
	if _, err := expandSource(ctx, "nope/*.x"); err == nil {
		t.Error("expected no-match error")
	}
	if _, err := expandSource(ctx, "../x"); err == nil {
		t.Error("expected escape error")
	}
}

func TestHashItems(t *testing.T) {
	ctx := t.TempDir()
	writeFile(t, ctx, "a.txt", "one")
	items, _ := expandSource(ctx, "a.txt")
	h1, _ := hashItems(items)
	writeFile(t, ctx, "a.txt", "two")
	h2, _ := hashItems(items)
	os.Chmod(filepath.Join(ctx, "a.txt"), 0o755)
	h3, _ := hashItems(items)
	if h1 == h2 || h2 == h3 {
		t.Fatalf("hash must change with content and mode: %s %s %s", h1, h2, h3)
	}
}
