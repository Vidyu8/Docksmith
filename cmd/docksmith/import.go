package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Vidyu8/docksmith/internal/layer"
	"github.com/Vidyu8/docksmith/internal/store"
)

// cmdImport turns a rootfs tarball into a single-layer base image.
func cmdImport(ref, file string) error {
	name, tag := store.ParseRef(ref)
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	var r io.Reader = br
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}

	tmp, err := os.MkdirTemp("", "docksmith-import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := layer.Extract(r, tmp); err != nil {
		return err
	}
	entries, err := layer.EntriesFromDir(tmp)
	if err != nil {
		return err
	}
	digest, size, err := layer.BuildTar(entries)
	if err != nil {
		return err
	}
	// Re-importing the same tarball must not change the image ID (it is the
	// base digest in every cache key), so keep the original `created`.
	created := time.Now().UTC().Format(time.RFC3339)
	if old, err := store.Load(name, tag); err == nil && len(old.Layers) == 1 && old.Layers[0].Digest == digest {
		created = old.Created
	}
	m := &store.Manifest{
		Name:    name,
		Tag:     tag,
		Created: created,
		Config:  store.Config{},
		Layers: []store.LayerRef{{
			Digest:    digest,
			Size:      size,
			CreatedBy: "import " + filepath.Base(file),
		}},
	}
	if err := store.Save(m); err != nil {
		return err
	}
	fmt.Printf("imported %s:%s  image=%s  layer=%s (%d bytes)\n", name, tag, m.Digest[:19], digest[:19], size)
	return nil
}
