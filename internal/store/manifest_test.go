package store

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestManifestDigest(t *testing.T) {
	t.Setenv("DOCKSMITH_HOME", t.TempDir())
	Init()
	m := &Manifest{Name: "x", Tag: "latest", Created: "2026-01-01T00:00:00Z",
		Layers: []LayerRef{{Digest: "sha256:aa", Size: 1, CreatedBy: "t"}}}
	if err := Save(m); err != nil {
		t.Fatal(err)
	}
	// recompute from the file on disk: blank the digest, re-hash, must match
	b, _ := os.ReadFile(ManifestPath("x", "latest"))
	var back Manifest
	json.Unmarshal(b, &back)
	want, _ := back.ComputeDigest()
	if back.Digest != want || !strings.HasPrefix(want, "sha256:") {
		t.Fatalf("digest mismatch %s vs %s", back.Digest, want)
	}
}

func TestInvalidRef(t *testing.T) {
	t.Setenv("DOCKSMITH_HOME", t.TempDir())
	Init()
	for _, bad := range []string{"../x", "a/b", "", ".hidden", "a b"} {
		if err := Save(&Manifest{Name: bad, Tag: "latest"}); err == nil {
			t.Errorf("Save accepted bad name %q", bad)
		}
		if _, err := Load(bad, "latest"); err == nil {
			t.Errorf("Load accepted bad name %q", bad)
		}
	}
}
