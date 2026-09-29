package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/testutil"
)

func TestTerminalCmd(t *testing.T) {
	look := func(have ...string) func(string) (string, error) {
		return func(p string) (string, error) {
			if slices.Contains(have, p) {
				return "/usr/bin/" + p, nil
			}
			return "", errors.New("no")
		}
	}
	const bf, dir = `C:\Users\Ana Pérez\go\bin\bflow.exe`, `C:\repo`
	cases := []struct {
		name, goos string
		env        []string
		have       []string
		want       string // prefijo del comando unido con espacios; "" = sin escritorio
	}{
		{"windows terminal", "windows", nil, []string{"wt.exe"}, "wt.exe -w 0 new-tab --title bflow -d " + dir + " " + bf + " watch"},
		{"consola sin wt", "windows", nil, nil, "powershell.exe -NoProfile -Command Start-Process -FilePath '" + bf + "' -ArgumentList watch -WorkingDirectory '" + dir + "'"},
		{"CI nunca", "windows", []string{"CI", "true"}, []string{"wt.exe"}, ""},
		{"macOS", "darwin", nil, nil, "osascript -e tell application \"Terminal\" to do script \"cd '" + dir + "' && '" + bf + "' watch\""},
		{"macOS por SSH", "darwin", []string{"SSH_CONNECTION", "1 2 3 4"}, nil, ""},
		{"linux sin DISPLAY", "linux", nil, []string{"gnome-terminal"}, ""},
		{"linux gnome", "linux", []string{"DISPLAY", ":0"}, []string{"gnome-terminal", "xterm"}, "gnome-terminal --working-directory=" + dir + " -- " + bf + " watch"},
		{"linux $TERMINAL gana", "linux", []string{"WAYLAND_DISPLAY", "w", "TERMINAL", "foot"}, []string{"foot", "gnome-terminal"}, "foot -e " + bf + " watch"},
		{"linux xterm al final", "linux", []string{"DISPLAY", ":0"}, []string{"xterm"}, "xterm -e " + bf + " watch"},
		{"linux sin terminal", "linux", []string{"DISPLAY", ":0"}, nil, ""},
	}
	for _, c := range cases {
		got, err := terminalCmd(c.goos, env(c.env...), look(c.have...), bf, dir, "", false)
		if c.want == "" {
			if !errors.Is(err, errNoDesktop) {
				t.Errorf("%s: esperaba sin escritorio, got %v %v", c.name, got, err)
			}
			continue
		}
		if err != nil || !strings.HasPrefix(strings.Join(got, " "), c.want) {
			t.Errorf("%s:\n got %q (%v)\nwant %q", c.name, strings.Join(got, " "), err, c.want)
		}
	}
}

func TestWatchHeartbeat(t *testing.T) {
	dir := testutil.TempDir(t)
	now := time.Now()
	if watchOpen(dir, now) {
		t.Fatal("sin latido no hay panel")
	}
	beat(dir, 2*time.Second)
	if !watchOpen(dir, now) {
		t.Error("latido reciente: panel abierto")
	}
	if watchOpen(dir, now.Add(10*time.Second)) {
		t.Error("latido de hace 10 s con intervalo de 2 s: el panel se cerró")
	}
	beat(dir, time.Minute)
	if !watchOpen(dir, now.Add(90*time.Second)) {
		t.Error("con intervalo largo el latido tarda más en envejecer")
	}
	os.Remove(filepath.Join(dir, "cache", "watch.alive"))
	if watchOpen(dir, now) {
		t.Error("al salir con Ctrl+C se borra")
	}
}

func TestTerminalCmdProfileAndWeb(t *testing.T) {
	look := func(string) (string, error) { return "wt.exe", nil }
	got, _ := terminalCmd("windows", env(), look, "bflow.exe", `C:\repo`, "{61c54bbd}", true)
	want := []string{"wt.exe", "-w", "0", "new-tab", "-p", "{61c54bbd}", "--title", "bflow", "-d", `C:\repo`, "bflow.exe", "watch", "--web"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%q", got)
	}
}

func TestWTProfile(t *testing.T) {
	settings := []byte("// comentario\n{\n    \"$schema\": \"x\",\n    \"defaultProfile\": \"{61c54bbd-c2c6-5271-96e7-009a87ff44bf}\",\n}")
	read := func(p string) ([]byte, error) {
		if strings.Contains(p, "Microsoft.WindowsTerminal_8wekyb3d8bbwe") {
			return settings, nil
		}
		return nil, os.ErrNotExist
	}
	if got := wtProfile(env("LOCALAPPDATA", `C:\u\AppData\Local`), read); got != "{61c54bbd-c2c6-5271-96e7-009a87ff44bf}" {
		t.Errorf("perfil por defecto: %q", got)
	}
	if got := wtProfile(env("WT_PROFILE_ID", "{abc}", "LOCALAPPDATA", `C:\x`), read); got != "{abc}" {
		t.Errorf("la pestaña actual manda: %q", got)
	}
	if got := wtProfile(env(), read); got != "" {
		t.Errorf("sin WT: %q", got)
	}
}
