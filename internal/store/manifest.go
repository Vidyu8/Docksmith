package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Config struct {
	Env        []string `json:"Env"`
	Cmd        []string `json:"Cmd"`
	WorkingDir string   `json:"WorkingDir"`
}

type LayerRef struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	CreatedBy string `json:"createdBy"`
}

// Manifest uses a struct (not a map) so JSON field order is stable -> stable digest.
type Manifest struct {
	Name    string     `json:"name"`
	Tag     string     `json:"tag"`
	Digest  string     `json:"digest"`
	Created string     `json:"created"`
	Config  Config     `json:"config"`
	Layers  []LayerRef `json:"layers"`
}

// ParseRef splits "name[:tag]"; tag defaults to "latest".
func ParseRef(s string) (name, tag string) {
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, "latest"
}

var refRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// checkRef keeps image names/tags safe to use in file names (no "/" or "..").
func checkRef(name, tag string) error {
	if !refRe.MatchString(name) || !refRe.MatchString(tag) {
		return fmt.Errorf("invalid image reference %q: name and tag may contain letters, digits, '.', '_' and '-'", name+":"+tag)
	}
	return nil
}

func ManifestPath(name, tag string) string {
	return filepath.Join(ImagesDir(), name+"_"+tag+".json")
}

func normalize(m *Manifest) {
	if m.Config.Env == nil {
		m.Config.Env = []string{}
	}
	if m.Config.Cmd == nil {
		m.Config.Cmd = []string{}
	}
	if m.Layers == nil {
		m.Layers = []LayerRef{}
	}
}

// ComputeDigest: serialise with digest "" -> sha256 of those bytes.
func (m *Manifest) ComputeDigest() (string, error) {
	c := *m
	normalize(&c)
	c.Digest = ""
	b, err := json.MarshalIndent(&c, "", "  ")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Save computes the digest, then writes the final manifest atomically.
func Save(m *Manifest) error {
	if err := checkRef(m.Name, m.Tag); err != nil {
		return err
	}
	normalize(m)
	d, err := m.ComputeDigest()
	if err != nil {
		return err
	}
	m.Digest = d
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	final := ManifestPath(m.Name, m.Tag)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

func Load(name, tag string) (*Manifest, error) {
	if err := checkRef(name, tag); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(ManifestPath(name, tag))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("image %s:%s not found", name, tag)
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("corrupt manifest for %s:%s: %w", name, tag, err)
	}
	return &m, nil
}

func List() ([]*Manifest, error) {
	files, err := filepath.Glob(filepath.Join(ImagesDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	var out []*Manifest
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		out = append(out, &m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Tag < out[j].Tag
	})
	return out, nil
}

// Remove deletes the manifest and ALL its layer files (no refcounting, per spec).
func Remove(name, tag string) error {
	m, err := Load(name, tag)
	if err != nil {
		return err
	}
	for _, l := range m.Layers {
		if err := os.Remove(LayerPath(l.Digest)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Remove(ManifestPath(name, tag))
}
