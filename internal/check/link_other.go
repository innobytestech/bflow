//go:build !windows

package check

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// linkDir crea en link un enlace simbólico a la carpeta target.
func linkDir(target, link string) error { return os.Symlink(target, link) }

// runAlive dice si la corrida de dir sigue viva: si su proceso existe (ante
// la duda, sí).
func runAlive(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "pid"))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || perr != nil {
		return true
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH)
}
