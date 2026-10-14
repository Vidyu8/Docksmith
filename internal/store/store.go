package store

import (
	"os"
	"path/filepath"
	"strings"
)

// Root returns the state dir. Override with DOCKSMITH_HOME (handy under sudo).
func Root() string {
	if v := os.Getenv("DOCKSMITH_HOME"); v != "" {
		return v
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".docksmith")
}

func ImagesDir() string { return filepath.Join(Root(), "images") }
func LayersDir() string { return filepath.Join(Root(), "layers") }
func CacheDir() string  { return filepath.Join(Root(), "cache") }

// LayerPath maps "sha256:<hex>" to layers/<hex>.tar.
func LayerPath(digest string) string {
	return filepath.Join(LayersDir(), strings.TrimPrefix(digest, "sha256:")+".tar")
}

// Init creates ~/.docksmith/{images,layers,cache}.
func Init() error {
	for _, d := range []string{ImagesDir(), LayersDir(), CacheDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
