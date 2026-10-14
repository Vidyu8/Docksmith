package build

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSnapshotDiff(t *testing.T) {
	root := t.TempDir()
	put := func(rel, content string) {
		full := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("a.txt", "1")
	put("keep/k.txt", "same")
	put("gone.txt", "bye")

	before, err := takeSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}

	put("a.txt", "2")           // modified
	put("b.txt", "new")         // added
	put("e/f.txt", "nested")    // new dir + new file
	os.Remove(filepath.Join(root, "gone.txt")) // deleted: must be ignored

	after, err := takeSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	got := changedPaths(before, after)
	want := []string{"a.txt", "b.txt", "e", "e/f.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSnapshotIgnoresMtime(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "x")
	os.WriteFile(p, []byte("same"), 0o644)
	s1, _ := takeSnapshot(root)
	os.WriteFile(p, []byte("same"), 0o644) // rewrite, identical content
	s2, _ := takeSnapshot(root)
	if len(changedPaths(s1, s2)) != 0 {
		t.Fatal("identical rewrite must not count as a change")
	}
}
