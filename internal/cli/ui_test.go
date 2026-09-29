package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/output"
)

func TestUIHandler(t *testing.T) {
	since := time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC)
	v := engine.View{ID: "API-7", Lane: flow.Light, Phase: flow.Spec, Gate: "spec", GateSince: since, Since: &since,
		Phases: flow.DefaultLanes()[flow.Light], Upcoming: flow.Step{Phase: flow.Implementing, Agents: []string{"implementer"}},
		Next: output.Next{Action: output.ActionAsk}}
	h := uiHandler([]string{"127.0.0.1:7719"}, func() (panelState, error) {
		return buildPanel("dev", watchData{Repo: "api", Active: &v}, since.Add(time.Minute)), nil
	})
	get := func(host, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := get("evil.example:7719", "/api/state"); w.Code != http.StatusForbidden {
		t.Errorf("Host ajeno (DNS rebinding): %d", w.Code)
	}
	if w := get("127.0.0.1:7719", "/"); w.Code != 200 || !strings.Contains(w.Body.String(), "Avisarme en este navegador") || w.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("página: %d %v", w.Code, w.Header())
	}
	w := get("127.0.0.1:7719", "/api/state")
	var ps panelState
	if err := json.Unmarshal(w.Body.Bytes(), &ps); err != nil {
		t.Fatal(err)
	}
	tk := ps.Task
	if tk == nil || !tk.Waiting || tk.Now == nil || !tk.Now.Human || tk.Now.Text != "aprobar la spec" || tk.Next.Text != "implementer implementa las tareas de la spec" {
		t.Fatalf("estado: %+v", tk)
	}
	if tk.Phases[0]["state"] != "current" || tk.Phases[1]["state"] != "pending" || len(tk.Phases) != 6 {
		t.Errorf("fases: %v", tk.Phases)
	}
	if w := get("127.0.0.1:7719", "/otra"); w.Code != http.StatusNotFound {
		t.Errorf("ruta desconocida: %d", w.Code)
	}
}

func TestBrowserCmd(t *testing.T) {
	if c, err := browserCmd("windows", env(), "http://127.0.0.1:7719/"); err != nil || c[0] != "rundll32" {
		t.Errorf("windows: %v %v", c, err)
	}
	if c, err := browserCmd("linux", env("DISPLAY", ":0"), "u"); err != nil || c[0] != "xdg-open" {
		t.Errorf("linux: %v %v", c, err)
	}
	if _, err := browserCmd("darwin", env("CI", "1"), "u"); !errors.Is(err, errNoDesktop) {
		t.Errorf("CI: %v", err)
	}
}
