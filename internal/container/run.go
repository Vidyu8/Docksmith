// Package container assembles an image's filesystem and runs it.
package container

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Vidyu8/docksmith/internal/layer"
	"github.com/Vidyu8/docksmith/internal/runtime"
	"github.com/Vidyu8/docksmith/internal/store"
)

// Run extracts the image into a temp dir, executes the command isolated,
// removes the temp dir, and returns the exit code.
// overrides ("K=V") beat image ENV; cmdOverride beats image CMD.
func Run(m *store.Manifest, overrides, cmdOverride []string) (int, error) {
	cmd := cmdOverride
	if len(cmd) == 0 {
		cmd = m.Config.Cmd
	}
	if len(cmd) == 0 {
		return 0, fmt.Errorf("no CMD defined in image %s:%s and no command given", m.Name, m.Tag)
	}

	root, err := os.MkdirTemp("", "docksmith-run-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(root)

	var ds []string
	for _, l := range m.Layers {
		ds = append(ds, l.Digest)
	}
	if err := layer.ExtractLayers(ds, root); err != nil {
		return 0, err
	}

	wd := m.Config.WorkingDir
	if wd == "" {
		wd = "/"
	}
	if err := os.MkdirAll(filepath.Join(root, wd), 0o755); err != nil {
		return 0, err
	}

	env := append([]string(nil), m.Config.Env...)
	for _, kv := range overrides {
		env = store.SetEnv(env, kv)
	}
	return runtime.Run(root, wd, env, cmd)
}
