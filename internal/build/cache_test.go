package build

import (
	"testing"

	"github.com/Vidyu8/docksmith/internal/store"
)

func TestCacheKeyIngredients(t *testing.T) {
	env := []string{"A=1", "B=2"}
	base := cacheKey("p", "RUN x", "/app", env, "")

	if got := cacheKey("p", "RUN x", "/app", []string{"B=2", "A=1"}, ""); got != base {
		t.Error("ENV order must not change the key")
	}
	if got := cacheKey("p", "RUN x", "/app", env, ""); got != base {
		t.Error("same inputs must give the same key")
	}
	variants := map[string]string{
		"prev layer":  cacheKey("q", "RUN x", "/app", env, ""),
		"instruction": cacheKey("p", "RUN y", "/app", env, ""),
		"workdir":     cacheKey("p", "RUN x", "/other", env, ""),
		"env value":   cacheKey("p", "RUN x", "/app", []string{"A=1", "B=3"}, ""),
		"env added":   cacheKey("p", "RUN x", "/app", append([]string{"C=3"}, env...), ""),
		"copy hash":   cacheKey("p", "RUN x", "/app", env, "h"),
	}
	for name, k := range variants {
		if k == base {
			t.Errorf("changing %s must change the key", name)
		}
	}
	// field boundaries must not blur
	if cacheKey("ab", "c", "", nil, "") == cacheKey("a", "bc", "", nil, "") {
		t.Error("fields must be length-prefixed")
	}
}

func TestCacheIndexRoundTrip(t *testing.T) {
	t.Setenv("DOCKSMITH_HOME", t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	idx, err := loadCache()
	if err != nil || len(idx) != 0 {
		t.Fatalf("fresh index: %v %v", idx, err)
	}
	idx["k1"] = "sha256:aa"
	if err := idx.save(); err != nil {
		t.Fatal(err)
	}
	back, err := loadCache()
	if err != nil || back["k1"] != "sha256:aa" {
		t.Fatalf("round trip failed: %v %v", back, err)
	}
}
