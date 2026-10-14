// Package runtime is the single isolation primitive. Both `RUN` during build
// and `docksmith run` MUST go through Run()/RunIO().
package runtime

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// ChildArg is argv[1] when the binary re-execs itself inside new namespaces.
const ChildArg = "__child"

const defaultPath = "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// Run is RunIO wired to the terminal (used by `docksmith run`).
func Run(rootfs, workdir string, env []string, cmd []string) (int, error) {
	return RunIO(rootfs, workdir, env, cmd, os.Stdin, os.Stdout, os.Stderr)
}

// RunIO executes cmd with rootfs as its root, in new mount/PID/UTS/net
// namespaces. env is the COMPLETE environment (host env is not inherited).
// stdin/stdout/stderr can be redirected (the build captures RUN output).
// Returns the process exit code.
func RunIO(rootfs, workdir string, env []string, cmd []string,
	stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if len(cmd) == 0 {
		return 1, errors.New("no command given")
	}
	if workdir == "" {
		workdir = "/"
	}
	hasPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
		}
	}
	if !hasPath {
		env = append(append([]string{}, env...), defaultPath)
	}

	args := append([]string{ChildArg, rootfs, workdir}, cmd...)
	c := exec.Command("/proc/self/exe", args...)
	c.Env = env
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
	c.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID |
			syscall.CLONE_NEWUTS | syscall.CLONE_NEWNET,
		Pdeathsig: syscall.SIGKILL,
	}

	err := c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 1, fmt.Errorf("start container: %w", err)
	}
	return 0, nil
}

// Child runs inside the new namespaces as PID 1. Never returns on success
// (it execs the target command).
func Child() {
	if len(os.Args) < 5 {
		fatal("child: bad args")
	}
	rootfs, workdir, cmd := os.Args[2], os.Args[3], os.Args[4:]

	// Stop our mounts from propagating back to the host mount namespace.
	must("make / private", syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""))
	must("sethostname", syscall.Sethostname([]byte("docksmith")))

	must("chroot", syscall.Chroot(rootfs))
	must("chdir /", syscall.Chdir("/"))

	// /proc so ps etc. work (shows only this PID namespace).
	_ = os.MkdirAll("/proc", 0o555)
	_ = syscall.Mount("proc", "/proc", "proc", 0, "")

	setupDev()

	must("chdir workdir", syscall.Chdir(workdir))

	bin, err := exec.LookPath(cmd[0]) // uses container PATH from env
	if err != nil {
		fatal("command not found: " + cmd[0])
	}
	must("exec", syscall.Exec(bin, cmd, os.Environ()))
}

// setupDev mounts a private tmpfs on /dev and creates the handful of device
// nodes almost every shell script needs (e.g. `2>/dev/null`). The tmpfs lives
// only in this mount namespace, so nothing leaks into the image or the host.
// Best effort: failures are ignored.
func setupDev() {
	_ = os.MkdirAll("/dev", 0o755)
	if err := syscall.Mount("tmpfs", "/dev", "tmpfs", syscall.MS_NOSUID, "mode=755"); err != nil {
		return
	}
	old := syscall.Umask(0) // so mknod'd modes are exactly 0666
	defer syscall.Umask(old)

	nodes := []struct {
		name         string
		major, minor int
	}{
		{"null", 1, 3}, {"zero", 1, 5}, {"random", 1, 8}, {"urandom", 1, 9},
	}
	for _, n := range nodes {
		_ = syscall.Mknod("/dev/"+n.name, syscall.S_IFCHR|0o666, n.major<<8|n.minor)
	}
	_ = os.Symlink("/proc/self/fd", "/dev/fd")
	_ = os.Symlink("/proc/self/fd/0", "/dev/stdin")
	_ = os.Symlink("/proc/self/fd/1", "/dev/stdout")
	_ = os.Symlink("/proc/self/fd/2", "/dev/stderr")
}

func must(what string, err error) {
	if err != nil {
		fatal(what + ": " + err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "docksmith:", msg)
	os.Exit(127)
}
