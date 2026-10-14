package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// copyItem is one file/dir/symlink selected by a COPY source pattern.
type copyItem struct {
	Src   string // absolute host path
	Ctx   string // slash path relative to the build context (identity + hashing)
	Rel   string // slash path relative to the pattern's static prefix; "" = directly-named file
	IsDir bool
}

func isWild(seg string) bool { return strings.ContainsAny(seg, "*?[") }

func relSlash(base, p string) string {
	r, _ := filepath.Rel(base, p)
	return filepath.ToSlash(r)
}

func cleanPattern(orig string) (string, error) {
	p := strings.TrimPrefix(path.Clean(filepath.ToSlash(orig)), "/")
	if p == "" {
		p = "."
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("source %q escapes the build context", orig)
	}
	return p, nil
}

func noMatch(pattern string) error {
	return fmt.Errorf("no source files matched %q", pattern)
}

// expandSource resolves one COPY source pattern against the context.
//
// Semantics (documented in README):
//   - non-wildcard dir  -> its CONTENTS are copied into dest (Docker-style)
//   - non-wildcard file -> dest/<base> if dest is dir-like, else dest itself
//   - wildcard pattern  -> every match (dirs recursively) lands under dest,
//     keeping its path relative to the pattern's static (non-wildcard) prefix
func expandSource(ctxDir, pattern string) ([]copyItem, error) {
	pat, err := cleanPattern(pattern)
	if err != nil {
		return nil, err
	}
	var segs []string
	if pat != "." {
		segs = strings.Split(pat, "/")
	}
	var static []string
	wild := false
	for _, s := range segs {
		if isWild(s) {
			wild = true
			break
		}
		static = append(static, s)
	}
	staticRel := strings.Join(static, "/")
	base := filepath.Join(ctxDir, filepath.FromSlash(staticRel))

	if !wild {
		fi, err := os.Lstat(base)
		if err != nil {
			return nil, noMatch(pattern)
		}
		if fi.IsDir() {
			return collect(ctxDir, base, base, false)
		}
		return []copyItem{{Src: base, Ctx: staticRel}}, nil
	}

	if fi, err := os.Lstat(base); err != nil || !fi.IsDir() {
		return nil, noMatch(pattern)
	}
	var items []copyItem
	err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == base {
			return nil
		}
		ctxRel := relSlash(ctxDir, p)
		if !matchSegs(segs, strings.Split(ctxRel, "/")) {
			return nil
		}
		if d.IsDir() {
			sub, err := collect(ctxDir, base, p, true)
			if err != nil {
				return err
			}
			items = append(items, sub...)
			return fs.SkipDir
		}
		t := d.Type()
		if !(t.IsRegular() || t&fs.ModeSymlink != 0) {
			return nil
		}
		items = append(items, copyItem{Src: p, Ctx: ctxRel, Rel: relSlash(base, p)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, noMatch(pattern)
	}
	return items, nil
}

// collect walks start (lexical order) and returns dirs/files/symlinks.
func collect(ctxDir, relBase, start string, includeStart bool) ([]copyItem, error) {
	var items []copyItem
	err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == start && !includeStart {
			return nil
		}
		t := d.Type()
		if !(t.IsDir() || t.IsRegular() || t&fs.ModeSymlink != 0) {
			return nil
		}
		items = append(items, copyItem{
			Src: p, Ctx: relSlash(ctxDir, p), Rel: relSlash(relBase, p), IsDir: t.IsDir(),
		})
		return nil
	})
	return items, err
}

// hashItems: per-file SHA-256 of raw bytes, combined in sorted path order.
// We also mix in path and permission bits, so a chmod or rename invalidates
// the cache too (a superset of "hash of source files"). Used in the Day-5 key.
func hashItems(items []copyItem) (string, error) {
	sorted := append([]copyItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Ctx < sorted[j].Ctx })
	parts := make([]string, 0, len(sorted))
	for _, it := range sorted {
		fi, err := os.Lstat(it.Src)
		if err != nil {
			return "", err
		}
		var content string
		switch {
		case fi.IsDir():
			content = "dir"
		case fi.Mode()&os.ModeSymlink != 0:
			t, err := os.Readlink(it.Src)
			if err != nil {
				return "", err
			}
			content = "link:" + t
		default:
			f, err := os.Open(it.Src)
			if err != nil {
				return "", err
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			f.Close()
			if err != nil {
				return "", err
			}
			content = hex.EncodeToString(h.Sum(nil))
		}
		parts = append(parts, fmt.Sprintf("%s:%o:%s", it.Ctx, fi.Mode().Perm(), content))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:]), nil
}
