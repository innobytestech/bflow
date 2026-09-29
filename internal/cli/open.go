package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// desktop dice por qué no hay escritorio donde abrir ventanas (nil si lo
// hay): CI, una sesión SSH o Linux sin DISPLAY.
func desktop(goos string, getenv func(string) string) error {
	switch {
	case getenv("CI") != "":
		return fmt.Errorf("%w: CI", errNoDesktop)
	case goos != "windows" && getenv("SSH_CONNECTION") != "":
		return fmt.Errorf("%w: sesión SSH", errNoDesktop)
	case goos != "windows" && goos != "darwin" && getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "":
		return fmt.Errorf("%w: no hay DISPLAY ni WAYLAND_DISPLAY", errNoDesktop)
	}
	return nil
}

// terminalCmd arma el comando que abre una ventana o pestaña con `bflow
// watch` (y --web si web) en dir. No abre nada en CI ni sin escritorio (SSH,
// Linux sin DISPLAY). lookPath dice qué programas hay; wtProfile es el perfil
// de Windows Terminal con que se abre la pestaña ("" = el que elija WT).
func terminalCmd(goos string, getenv func(string) string, lookPath func(string) (string, error), bflow, dir, wtProfile string, web bool) ([]string, error) {
	if getenv("CI") != "" {
		return nil, fmt.Errorf("%w: CI", errNoDesktop)
	}
	has := func(p string) bool { _, err := lookPath(p); return err == nil }
	run := []string{bflow, "watch"}
	if web {
		run = append(run, "--web")
	}
	switch goos {
	case "windows":
		if has("wt.exe") { // Windows Terminal: pestaña nueva en la ventana más reciente
			// Con el perfil explícito: un comando suelto se abre con la
			// configuración base de WT y no con la fuente y colores del usuario.
			args := []string{"wt.exe", "-w", "0", "new-tab"}
			if wtProfile != "" {
				args = append(args, "-p", wtProfile)
			}
			return append(append(args, "--title", "bflow", "-d", dir), run...), nil
		}
		// Sin Windows Terminal, una consola nueva. Start-Process y no `cmd /c start`,
		// que confunde el primer argumento entre comillas con el título.
		ps := fmt.Sprintf("Start-Process -FilePath %s -ArgumentList %s -WorkingDirectory %s", psQuote(bflow), strings.Join(run[1:], ","), psQuote(dir))
		return []string{"powershell.exe", "-NoProfile", "-Command", ps}, nil
	case "darwin":
		if getenv("SSH_CONNECTION") != "" {
			return nil, fmt.Errorf("%w: sesión SSH", errNoDesktop)
		}
		script := fmt.Sprintf(`tell application "Terminal" to do script "cd %s && %s %s"`, shellQuote(dir), shellQuote(bflow), strings.Join(run[1:], " "))
		return []string{"osascript", "-e", script, "-e", `tell application "Terminal" to activate`}, nil
	}
	if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
		return nil, fmt.Errorf("%w: no hay DISPLAY ni WAYLAND_DISPLAY", errNoDesktop)
	}
	// La terminal hereda el directorio del proceso (cmd.Dir); las que lo
	// ignoran lo reciben como argumento.
	if t := getenv("TERMINAL"); t != "" && has(t) {
		return append([]string{t, "-e"}, run...), nil
	}
	for _, t := range []struct {
		name string
		args []string
	}{
		{"x-terminal-emulator", []string{"-e"}},
		{"gnome-terminal", []string{"--working-directory=" + dir, "--"}},
		{"konsole", []string{"--workdir", dir, "-e"}},
		{"xfce4-terminal", []string{"--working-directory", dir, "-x"}},
		{"kitty", []string{"--directory", dir}},
		{"alacritty", []string{"--working-directory", dir, "-e"}},
		{"wezterm", []string{"start", "--cwd", dir}},
		{"xterm", []string{"-e"}},
	} {
		if has(t.name) {
			return append(append([]string{t.name}, t.args...), run...), nil
		}
	}
	return nil, fmt.Errorf("%w: no se encontró una terminal (define TERMINAL)", errNoDesktop)
}

// wtProfile es el perfil de Windows Terminal para la pestaña del panel: el
// de la pestaña desde donde se corre bflow (WT_PROFILE_ID) o, si bflow corre
// fuera de WT (la sesión de Claude en VS Code), el perfil por defecto del
// usuario, leído de su settings.json.
func wtProfile(getenv func(string) string, readFile func(string) ([]byte, error)) string {
	if id := getenv("WT_PROFILE_ID"); id != "" {
		return id
	}
	base := getenv("LOCALAPPDATA")
	if base == "" {
		return ""
	}
	for _, p := range []string{
		`Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json`,
		`Packages\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\LocalState\settings.json`,
		`Microsoft\Windows Terminal\settings.json`,
	} {
		if b, err := readFile(filepath.Join(base, p)); err == nil {
			if m := defaultProfileRe.FindSubmatch(b); m != nil {
				return string(m[1])
			}
		}
	}
	return ""
}

// defaultProfileRe saca defaultProfile de settings.json, que admite
// comentarios y por eso no siempre es JSON válido.
var defaultProfileRe = regexp.MustCompile(`"defaultProfile"\s*:\s*"([^"]+)"`)

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

// openWatch abre el panel en otra ventana si no hay uno abierto; con web,
// además sirve la página y abre el navegador. Devuelve
// "opened" o "already".
func openWatch(root, storeDir string, web bool) (string, error) {
	if watchOpen(storeDir, time.Now()) {
		return "already", nil
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	args, err := terminalCmd(goos, os.Getenv, exec.LookPath, self, root, wtProfile(os.Getenv, os.ReadFile), web)
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
