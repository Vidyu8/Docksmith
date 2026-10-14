package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Vidyu8/docksmith/internal/store"
)

// cacheIndex maps cache key -> layer digest ("sha256:<hex>").
// It lives in ~/.docksmith/cache/index.json.
type cacheIndex map[string]string

func cachePath() string { return filepath.Join(store.CacheDir(), "index.json") }

// loadCache reads the index. A missing or corrupt index just means "empty":
// it is only a cache, so the worst case is rebuilding.
func loadCache() (cacheIndex, error) {
	idx := cacheIndex{}
	b, err := os.ReadFile(cachePath())
	if os.IsNotExist(err) {
		return idx, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &idx); err != nil || idx == nil {
		return cacheIndex{}, nil
	}
	return idx, nil
}

// save writes the index atomically (temp file + rename). json.Marshal sorts
// map keys, so the file is deterministic too.
func (c cacheIndex) save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := cachePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, cachePath())
}

// cacheKey is the deterministic fingerprint of everything that can change a
// layer-producing step's result:
//
//	prev     digest of the previous layer-producing step
//	         (the base image's MANIFEST digest for the first one)
//	inst     the full instruction text as written
//	workdir  current WORKDIR ("" if unset)
//	env      all ENV pairs so far, sorted by key
//	copy     COPY only: hash of the source files ("" for RUN)
//
// Every field is length-prefixed, so values can never run into each other
// ("ab"+"c" and "a"+"bc" produce different keys).
func cacheKey(prev, inst, workdir string, env []string, copyHash string) string {
	h := sha256.New()
	field := func(name, v string) { fmt.Fprintf(h, "%s:%d:%s\n", name, len(v), v) }
	field("prev", prev)
	field("inst", inst)
	field("workdir", workdir)
	field("env", store.SortedEnv(env))
	field("copy", copyHash)
	return hex.EncodeToString(h.Sum(nil))
}
