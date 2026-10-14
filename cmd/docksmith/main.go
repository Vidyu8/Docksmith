package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/Vidyu8/docksmith/internal/build"
	"github.com/Vidyu8/docksmith/internal/container"
	"github.com/Vidyu8/docksmith/internal/layer"
	"github.com/Vidyu8/docksmith/internal/runtime"
	"github.com/Vidyu8/docksmith/internal/store"
)

func main() {
	// Re-exec entry point: must be checked before anything else.
	if len(os.Args) > 1 && os.Args[1] == runtime.ChildArg {
		runtime.Child()
		return
	}
	if len(os.Args) < 2 {
		usage()
	}
	if err := store.Init(); err != nil {
		die(err)
	}
	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "build":
		err = cmdBuild(args)
	case "run":
		code, rerr := cmdRun(args)
		if rerr != nil {
			die(rerr)
		}
		os.Exit(code)
	case "images":
		err = cmdImages()
	case "rmi":
		if len(args) != 1 {
			usage()
		}
		err = cmdRmi(args[0])
	case "import": // import <name:tag> <rootfs.tar.gz>
		if len(args) != 2 {
			usage()
		}
		err = cmdImport(args[0], args[1])
	case "dev-extract": // dev-extract <name:tag> <dir>
		if len(args) != 2 {
			usage()
		}
		err = cmdDevExtract(args[0], args[1])
	case "dev-run": // dev-run <rootfs> <cmd...>
		if len(args) < 2 {
			usage()
		}
		code, rerr := runtime.Run(args[0], "/", []string{"HOME=/root"}, args[1:])
		if rerr != nil {
			die(rerr)
		}
		fmt.Printf("exit code: %d\n", code)
		os.Exit(code)
	default:
		usage()
	}
	if err != nil {
		die(err)
	}
}

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	tag := fs.String("t", "", "name:tag for the image (required)")
	noCache := fs.Bool("no-cache", false, "skip all cache lookups and writes")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return err
	}
	if *tag == "" || fs.NArg() != 1 {
		usage()
	}
	name, t := store.ParseRef(*tag)
	_, err := build.Build(build.Options{
		Context: fs.Arg(0), Name: name, Tag: t, NoCache: *noCache, Out: os.Stdout,
	})
	return err
}

type envList []string

func (e *envList) String() string { return strings.Join(*e, ",") }
func (e *envList) Set(v string) error {
	if !strings.Contains(v, "=") {
		return fmt.Errorf("-e expects KEY=VALUE, got %q", v)
	}
	*e = append(*e, v)
	return nil
}

func cmdRun(args []string) (int, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var envs envList
	fs.Var(&envs, "e", "KEY=VALUE env override (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 1, err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		usage()
	}
	name, tag := store.ParseRef(rest[0])
	m, err := store.Load(name, tag)
	if err != nil {
		return 1, err
	}
	code, err := container.Run(m, envs, rest[1:])
	if err != nil {
		return 1, err
	}
	fmt.Printf("Container exited with code %d\n", code)
	return code, nil
}

func cmdImages() error {
	ms, err := store.List()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTAG\tID\tCREATED")
	for _, m := range ms {
		id := strings.TrimPrefix(m.Digest, "sha256:")
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", m.Name, m.Tag, id, m.Created)
	}
	return tw.Flush()
}

func cmdRmi(ref string) error {
	name, tag := store.ParseRef(ref)
	if err := store.Remove(name, tag); err != nil {
		return err
	}
	fmt.Printf("removed %s:%s\n", name, tag)
	return nil
}

func cmdDevExtract(ref, dir string) error {
	name, tag := store.ParseRef(ref)
	m, err := store.Load(name, tag)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var ds []string
	for _, l := range m.Layers {
		ds = append(ds, l.Digest)
	}
	return layer.ExtractLayers(ds, dir)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  docksmith build -t <name:tag> [--no-cache] <context>
  docksmith run [-e KEY=VALUE]... <name:tag> [cmd...]
  docksmith images
  docksmith rmi <name:tag>
  docksmith import <name:tag> <rootfs.tar.gz>   (setup: load a base image)`)
	os.Exit(2)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// reorderFlags moves flags in front of positional arguments so both
//
//	docksmith build -t app:1 --no-cache .   and   docksmith build . -t app:1
//
// work (Go's flag package stops parsing at the first positional argument).
func reorderFlags(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-t" || a == "--t":
			flags = append(flags, a)
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
		default:
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}
