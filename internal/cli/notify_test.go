package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/testutil"
)

func TestNotifyCmd(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("no") }
	has := func(string) (string, error) { return "/usr/bin/notify-send", nil }
	if c, err := notifyCmd("windows", env(), none); err != nil || c[0] != "powershell.exe" || !strings.Contains(c[4], "$env:BFLOW_BODY") {
		t.Errorf("windows: %v %v", c, err)
	}
	if c, err := notifyCmd("darwin", env(), none); err != nil || c[0] != "osascript" {
		t.Errorf("darwin: %v %v", c, err)
	}
	if c, err := notifyCmd("linux", env("DISPLAY", ":0"), has); err != nil || !strings.Contains(c[2], "notify-send") {
		t.Errorf("linux: %v %v", c, err)
	}
	for name, c := range map[string]struct {
		goos string
		env  []string
		look func(string) (string, error)
	}{
		"CI":              {"windows", []string{"CI", "true"}, none},
		"SSH":             {"darwin", []string{"SSH_CONNECTION", "x"}, none},
		"sin DISPLAY":     {"linux", nil, has},
		"sin notify-send": {"linux", []string{"DISPLAY", ":0"}, none},
	} {
		if _, err := notifyCmd(c.goos, env(c.env...), c.look); !errors.Is(err, errNoDesktop) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNotifyWaitingOncePerWait(t *testing.T) {
	dir := testutil.TempDir(t)
	var sent []string
	send := func(title, body string) error { sent = append(sent, title+" | "+body); return nil }
	since := time.Date(2026, 9, 29, 13, 26, 0, 0, time.UTC)
	v := engine.View{ID: "API-7", Phase: flow.Contract, Gate: "contract", GateSince: since}

	if got := notifyWaiting(dir, v, send); got != "Espera tu decisión: aprobar el contrato (pruebas y firmas)" {
		t.Errorf("primer aviso: %q", got)
	}
	if notifyWaiting(dir, v, send) != "" {
		t.Error("la misma espera no se avisa dos veces")
	}
	v.Gate, v.GateSince = "decision", since.Add(3*time.Minute)
	if got := notifyWaiting(dir, v, send); !strings.Contains(got, "tomar una decisión") {
		t.Errorf("gate nueva: %q", got)
	}
	v.Gate, v.Blocked = "", "falta acceso a la BD"
	if got := notifyWaiting(dir, v, send); got != "Espera tu decisión: desbloquear la tarea: falta acceso a la BD" {
		t.Errorf("bloqueada: %q", got)
	}
	v.Blocked = ""
	if notifyWaiting(dir, v, send) != "" {
		t.Error("sin espera no se avisa")
	}
	if len(sent) != 3 || !strings.HasPrefix(sent[0], "bflow · API-7 | ") {
		t.Errorf("enviados: %v", sent)
	}
	// Si el envío falla (sin escritorio), no se marca: se intenta en el próximo turno.
	v.Gate, v.GateSince = "walkthrough", since.Add(time.Hour)
	notifyWaiting(dir, v, func(string, string) error { return errNoDesktop })
	if notifyWaiting(dir, v, send) == "" {
		t.Error("tras un envío fallido se vuelve a intentar")
	}
}
