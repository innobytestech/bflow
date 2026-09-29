package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/store"
)

// El panel se abre en otra ventana para que la persona siga la tarea sin
// mirar la conversación. Mientras corre, bflow watch deja un latido en
// .bflow/cache/watch.alive; así --open no abre un segundo panel.

var goos = runtime.GOOS

var errNoDesktop = errors.New("sin escritorio")

// terminalCmd arma el comando que abre una ventana o pestaña con `bflow
// watch` en dir. No abre nada en CI ni sin escritorio (SSH, Linux sin
// DISPLAY). lookPath dice qué programas hay.
func terminalCmd(goos string, getenv func(string) string, lookPath func(string) (string, error), bflow, dir string) ([]string, error) {
	if getenv("CI") != "" {
		return nil, fmt.Errorf("%w: CI", errNoDesktop)
	}
	has := func(p string) bool { _, err := lookPath(p); return err == nil }
	switch goos {
	case "windows":
		if has("wt.exe") { // Windows Terminal: pestaña nueva en la ventana más reciente
			return []string{"wt.exe", "-w", "0", "new-tab", "--title", "bflow", "-d", dir, bflow, "watch"}, nil
		}
		// Sin Windows Terminal, una consola nueva. Start-Process y no `cmd /c start`,
		// que confunde el primer argumento entre comillas con el título.
		ps := fmt.Sprintf("Start-Process -FilePath %s -ArgumentList watch -WorkingDirectory %s", psQuote(bflow), psQuote(dir))
		return []string{"powershell.exe", "-NoProfile", "-Command", ps}, nil
	case "darwin":
		if getenv("SSH_CONNECTION") != "" {
			return nil, fmt.Errorf("%w: sesión SSH", errNoDesktop)
		}
		script := fmt.Sprintf(`tell application "Terminal" to do script "cd %s && %s watch"`, shellQuote(dir), shellQuote(bflow))
		return []string{"osascript", "-e", script, "-e", `tell application "Terminal" to activate`}, nil
	}
	if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
		return nil, fmt.Errorf("%w: no hay DISPLAY ni WAYLAND_DISPLAY", errNoDesktop)
	}
	// La terminal hereda el directorio del proceso (cmd.Dir); las que lo
	// ignoran lo reciben como argumento.
	if t := getenv("TERMINAL"); t != "" && has(t) {
		return []string{t, "-e", bflow, "watch"}, nil
	}
	for _, t := range []struct {
		name string
		args []string
	}{
		{"x-terminal-emulator", []string{"-e", bflow, "watch"}},
		{"gnome-terminal", []string{"--working-directory=" + dir, "--", bflow, "watch"}},
		{"konsole", []string{"--workdir", dir, "-e", bflow, "watch"}},
		{"xfce4-terminal", []string{"--working-directory", dir, "-x", bflow, "watch"}},
		{"kitty", []string{"--directory", dir, bflow, "watch"}},
		{"alacritty", []string{"--working-directory", dir, "-e", bflow, "watch"}},
		{"wezterm", []string{"start", "--cwd", dir, bflow, "watch"}},
		{"xterm", []string{"-e", bflow, "watch"}},
	} {
		if has(t.name) {
			return append([]string{t.name}, t.args...), nil
		}
	}
	return nil, fmt.Errorf("%w: no se encontró una terminal (define TERMINAL)", errNoDesktop)
}

func psQuote(s string) string { return `'` + strings.ReplaceAll(s, `'`, `''`) + `'` }

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}

func alivePath(storeDir string) string { return filepath.Join(storeDir, "cache", "watch.alive") }

// beat marca que el panel sigue abierto; guarda el intervalo para saber
// cuándo el latido ya es viejo.
func beat(storeDir string, interval time.Duration) {
	p := alivePath(storeDir)
	_ = store.EnsureDirFor(p)
	_ = store.WriteAtomic(p, []byte(strconv.FormatInt(int64(interval/time.Millisecond), 10)))
}

// watchOpen dice si hay un panel vivo: su latido es más reciente que dos
// intervalos y un margen.
func watchOpen(storeDir string, now time.Time) bool {
	p := alivePath(storeDir)
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	b, _ := os.ReadFile(p)
	ms, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return now.Sub(st.ModTime()) < 2*time.Duration(ms)*time.Millisecond+5*time.Second
}

// openWatch abre el panel en otra ventana si no hay uno abierto. Devuelve
// "opened" o "already".
func openWatch(root, storeDir string) (string, error) {
	if watchOpen(storeDir, time.Now()) {
		return "already", nil
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	args, err := terminalCmd(goos, os.Getenv, exec.LookPath, self, root)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = root
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%s: %w", args[0], err)
	}
	// Se marca de una vez: si start se repite antes de que el panel dibuje su
	// primer cuadro, no abre otro.
	beat(storeDir, 5*time.Second)
	return "opened", cmd.Process.Release()
}
