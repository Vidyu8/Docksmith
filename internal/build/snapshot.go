package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// snapshot maps each path under a root (slash-separated, relative) to a short
// "signature" string. Two snapshots differ at a path exactly when the file
// was added or its type / permissions / content / link target changed.
// Timestamps are deliberately ignored.
type snapshot map[string]string

func modeBits(fi os.FileInfo) os.FileMode {
	return fi.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func takeSnapshot(root string) (snapshot, error) {
	snap := snapshot{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		fi, err := os.Lstat(p) // Lstat: describe symlinks themselves
		if err != nil {
			return err
		}
		switch {
		case fi.IsDir():
			snap[rel] = fmt.Sprintf("d:%o", modeBits(fi))
		case fi.Mode()&os.ModeSymlink != 0:
			t, err := os.Readlink(p)
			if err != nil {
				return err
			}
			snap[rel] = "l:" + t
		case fi.Mode().IsRegular():
			h, err := hashFile(p)
			if err != nil {
				return err
			}
			snap[rel] = fmt.Sprintf("f:%o:%s", modeBits(fi), h)
		}
		// devices, sockets, fifos: ignored (we can't put them in a layer)
		return nil
	})
	return snap, err
}

// changedPaths returns, sorted, every path that is new or different in
// `after`. Deletions are not reported (no whiteout support; see README).
func changedPaths(before, after snapshot) []string {
	var out []string
	for p, sig := range after {
		if old, ok := before[p]; !ok || old != sig {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
