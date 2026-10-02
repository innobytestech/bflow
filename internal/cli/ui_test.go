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
	var asked string
	h := uiHandler([]string{"127.0.0.1:7719"}, func(key string) (panelState, error) {
		asked = key
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
	if w := get("127.0.0.1:7719", "/"); w.Code != 200 || !strings.Contains(w.Body.String(), "Avisarme cuando me toque") || w.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("página: %d %v", w.Code, w.Header())
	}
	if w := get("127.0.0.1:7719", "/fonts/Jersey10.woff2"); w.Code != 200 || w.Body.Len() < 1000 {
		t.Errorf("fuente: %d", w.Code)
	}
	w := get("127.0.0.1:7719", "/api/state?repo=ab12cd34")
	if asked != "ab12cd34" {
		t.Errorf("repo pedido: %q", asked)
	}
	var ps panelState
	if err := json.Unmarshal(w.Body.Bytes(), &ps); err != nil {
		t.Fatal(err)
	}
	tk := ps.Task
	if tk == nil || !tk.Waiting || tk.Now == nil || !tk.Now.Human || tk.Now.Text != "aprobar la spec" || tk.Next.Text != "implementer implementa las tareas de la spec" {
		t.Fatalf("estado: %+v", tk)
	}
	if p := tk.Phases; p[0].State != "current" || !p[0].Stop || p[1].State != "pending" || p[1].Stop || len(p) != 6 {
		t.Errorf("fases: %+v", tk.Phases)
	}
	if w := get("127.0.0.1:7719", "/otra"); w.Code != http.StatusNotFound {
		t.Errorf("ruta desconocida: %d", w.Code)
	}
}

// La página dibuja el mismo banner que la terminal.
func TestUIPageBanner(t *testing.T) {
	for _, l := range bannerWord {
		if !strings.Contains(string(uiPage), `"`+l+`"`) {
			t.Errorf("la página no tiene la línea del banner %q", l)
		}
	}
}

// R9, R12, R16, R20: la página cabe en una pantalla, con ventana de detalle y chips de repos.
func TestUIPageOneScreen(t *testing.T) {
	page := string(uiPage)
	for _, want := range []string{"100dvh", `<dialog id="calls"`, `id="repos"`} {
		if !strings.Contains(page, want) {
			t.Errorf("la página debe tener %q", want)
		}
	}
	for _, bad := range []string{"<details", `id="foot"`, `id="net"`} {
		if strings.Contains(page, bad) {
			t.Errorf("la página no debe tener %q", bad)
		}
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

func TestRenderPanelClosedOutside(t *testing.T) {
	got := renderPanel(engine.PanelReport{ClosedOutside: []engine.ClosedOutside{
		{ID: "GH-13", From: flow.Implementing, Reason: "tracker"},
		{ID: "GH-14", From: flow.Implementing, Reason: "pr", PR: 30},
	}})
	for _, want := range []string{"GH-13 cerrada: terminada fuera de esta copia (tracker)", "GH-14 cerrada: terminada fuera de esta copia (PR #30 mergeado)"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en:\n%s", want, got)
		}
	}
}
