// Package layer creates deterministic tar layers and extracts them.
package layer

import (
	"archive/tar"
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
	"time"

	"github.com/Vidyu8/docksmith/internal/store"
)

// Entry maps a host file (Src) to its path inside the image (Name, no leading "/").
type Entry struct {
	Name string
	Src  string
}

// EntriesFromDir lists every dir/file/symlink under root (excluding root itself).
// Other types (devices, fifos, sockets) are skipped.
func EntriesFromDir(root string) ([]Entry, error) {
	var out []Entry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		t := d.Type()
		if !(t.IsDir() || t.IsRegular() || t&fs.ModeSymlink != 0) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, Entry{Name: filepath.ToSlash(rel), Src: p})
		return nil
	})
	return out, err
}

// BuildTar writes a deterministic tar into the layer store and returns
// ("sha256:<hex>", size on disk). Determinism rules:
//   - entries sorted by name (last duplicate wins)
//   - mtime = epoch, atime/ctime zero, uid/gid 0, no user/group names
//   - fixed PAX format, no extra records, no compression
func BuildTar(entries []Entry) (string, int64, error) {
	byName := map[string]Entry{}
	for _, e := range entries {
		n := strings.TrimPrefix(path.Clean(filepath.ToSlash(e.Name)), "/")
		if n == "" || n == "." {
			continue
		}
		e.Name = n
		byName[n] = e
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)

	tmp, err := os.CreateTemp(store.LayersDir(), "tmp-*")
	if err != nil {
		return "", 0, err
	}
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	h := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(tmp, h))
	for _, n := range names {
		if err := writeEntry(tw, byName[n]); err != nil {
			return "", 0, err
		}
	}
	if err := tw.Close(); err != nil {
		return "", 0, err
	}
	if err := tmp.Chmod(0o444); err != nil {
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}

	digest := "sha256:" + hex.EncodeToString(h.Sum(nil))
	final := store.LayerPath(digest)
	if _, err := os.Stat(final); err == nil {
		os.Remove(tmp.Name()) // identical content already stored
	} else if err := os.Rename(tmp.Name(), final); err != nil {
		return "", 0, err
	}
	ok = true
	fi, err := os.Stat(final)
	if err != nil {
		return "", 0, err
	}
	return digest, fi.Size(), nil
}

func writeEntry(tw *tar.Writer, e Entry) error {
	fi, err := os.Lstat(e.Src)
	if err != nil {
		return err
	}
	link := ""
	if fi.Mode()&os.ModeSymlink != 0 {
		if link, err = os.Readlink(e.Src); err != nil {
			return err
		}
	}
	hdr, err := tar.FileInfoHeader(fi, link)
	if err != nil {
		return err
	}
	hdr.Name = e.Name
	if fi.IsDir() {
		hdr.Name += "/"
	}
	hdr.Uid, hdr.Gid = 0, 0
	hdr.Uname, hdr.Gname = "", ""
	hdr.ModTime = time.Unix(0, 0)
	hdr.AccessTime, hdr.ChangeTime = time.Time{}, time.Time{}
	hdr.Format = tar.FormatPAX
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if fi.Mode().IsRegular() {
		f, err := os.Open(e.Src)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
	}
	return nil
}

// safeJoin resolves name under dest and refuses to traverse symlinks in
// parent components (prevents a layer escaping dest via "link -> /etc").
func safeJoin(dest, name string) (string, error) {
	rel := strings.TrimPrefix(path.Clean("/"+name), "/")
	if rel == "" {
		return "", nil
	}
	parts := strings.Split(rel, "/")
	cur := dest
	for _, p := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, p)
		if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to write through symlink: %s", name)
		}
	}
	return filepath.Join(dest, rel), nil
}

// Extract unpacks a tar stream into dest. Later layers overwrite earlier files.
func Extract(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		if target == "" {
			continue
		}
		mode := hdr.FileInfo().Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if fi, err := os.Lstat(target); err == nil && !fi.IsDir() {
				os.RemoveAll(target)
			}
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			if err := os.Chmod(target, mode); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			os.RemoveAll(target)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if err := os.Chmod(target, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			os.RemoveAll(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		case tar.TypeLink:
			src, err := safeJoin(dest, hdr.Linkname)
			if err != nil {
				return err
			}
			os.RemoveAll(target)
			if err := os.Link(src, target); err != nil {
				return err
			}
		default:
			// devices/fifos unsupported; skipped.
		}
	}
}

// ExtractLayers extracts the given layer digests, in order, into dest.
func ExtractLayers(digests []string, dest string) error {
	for _, d := range digests {
		f, err := os.Open(store.LayerPath(d))
		if err != nil {
			return fmt.Errorf("layer %s missing: %w", d, err)
		}
		err = Extract(f, dest)
		f.Close()
		if err != nil {
			return fmt.Errorf("extract %s: %w", d, err)
		}
	}
	return nil
}
