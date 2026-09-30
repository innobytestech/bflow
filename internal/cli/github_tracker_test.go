package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/testutil"
	"innobytes.tech/bflow/internal/tracker"
	"innobytes.tech/bflow/internal/tracker/trackertest"
)

// ghFake es un tracker con las capacidades opcionales que usa el adaptador github.
type ghFake struct {
	*trackertest.Memory
	ensure   func(dryRun bool) ([]string, error)
	projects []tracker.Project
	projErr  error
	states   []tracker.StateInfo
	stateErr error
}

func (g *ghFake) Name() string { return "github" }
func (g *ghFake) EnsureStates(_ context.Context, dryRun bool) ([]string, error) {
	return g.ensure(dryRun)
}
func (g *ghFake) Projects(context.Context) ([]tracker.Project, error) { return g.projects, g.projErr }
func (g *ghFake) StateMap(context.Context) ([]tracker.StateInfo, error) {
	return g.states, g.stateErr
}

func ghEnv(t *testing.T, yaml string, tr tracker.Tracker) *Env {
	t.Helper()
	dir := testutil.TempDir(t)
	t.Setenv("BFLOW_CONFIG_HOME", testutil.TempDir(t))
	if err := os.WriteFile(filepath.Join(dir, "bflow.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Stdin: strings.NewReader(""), Dir: dir,
		Build: func(d string) (*engine.Engine, error) {
			cfg, err := config.Load(d)
			if err != nil {
				return nil, err
			}
			return &engine.Engine{Cfg: cfg, Store: store.Open(cfg.Root), Tracker: tr, User: "dev"}, nil
		}}
}

const ghYAML = "vcs: { host: github, repo: acme/app }\ntracker: { adapter: github, prefix: GH%s }\n"

func TestTrackerSetupManual(t *testing.T) {
	missing := []string{"Discovery (BLUE)", "Contrato (PURPLE)"}
	tr := &ghFake{Memory: trackertest.NewMemory(), ensure: func(bool) ([]string, error) {
		return nil, &tracker.ManualSetupError{Where: "el project acme/7, campo Status", Missing: missing, Reason: "la API no acepta ids de opción"}
	}}
	env := ghEnv(t, strings.Replace(ghYAML, "%s", ", project: acme/7", 1), tr)
	code := Run([]string{"tracker", "setup", "--json"}, env)
	out := env.Stdout.(*bytes.Buffer).String()
	if code != 0 {
		t.Fatalf("un setup manual no es un fallo, exit %d: %s", code, out)
	}
	var e struct {
		OK   bool   `json:"ok"`
		Code string `json:"code"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !e.OK || e.Code != "manual" {
		t.Errorf("ok con código manual: %+v", e)
	}
	plain := new(bytes.Buffer)
	env.Stdout = plain
	Run([]string{"tracker", "setup"}, env)
	for _, w := range append([]string{"crea a mano en el project acme/7, campo Status:"}, missing...) {
		if !strings.Contains(plain.String(), w) {
			t.Errorf("la salida no dice %q:\n%s", w, plain.String())
		}
	}

	// Otros errores siguen siendo fallo.
	tr.ensure = func(bool) ([]string, error) { return nil, errors.New("boom") }
	env.Stdout = &bytes.Buffer{}
	if code := Run([]string{"tracker", "setup"}, env); code == 0 {
		t.Error("un error cualquiera de EnsureStates sigue fallando")
	}
}

// doctorTrackerItem corre la revisión del tracker que usa bflow doctor.
func doctorTrackerItem(t *testing.T, env *Env) (status, detail string) {
	t.Helper()
	e, err := env.Build(env.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var items []docItem
	doctorTracker(context.Background(), e.Tracker, e.Cfg, func(area, st, format string, a ...any) {
		items = append(items, docItem{Area: area, Status: st, Detail: fmt.Sprintf(format, a...)})
	})
	if len(items) != 1 || items[0].Area != "tracker" {
		t.Fatalf("doctor da un resultado del tracker: %+v", items)
	}
	return items[0].Status, items[0].Detail
}

func allStates() []tracker.StateInfo {
	var out []tracker.StateInfo
	for _, p := range tracker.PhaseOrder {
		out = append(out, tracker.StateInfo{Name: "bflow:" + string(p), Group: "label", Phase: p, Writes: []string{string(p)}})
	}
	return out
}

func TestDoctorGithubTracker(t *testing.T) {
	t.Run("sin project no es fallo", func(t *testing.T) {
		tr := &ghFake{Memory: trackertest.NewMemory(), states: allStates()}
		st, d := doctorTrackerItem(t, ghEnv(t, strings.Replace(ghYAML, "%s", "", 1), tr))
		if st != "ok" {
			t.Errorf("sin project y con todas las etiquetas: %s %s", st, d)
		}
	})
	t.Run("etiquetas faltantes avisan", func(t *testing.T) {
		tr := &ghFake{Memory: trackertest.NewMemory(), states: allStates()[:5]}
		st, d := doctorTrackerItem(t, ghEnv(t, strings.Replace(ghYAML, "%s", "", 1), tr))
		if st != "warn" || !strings.Contains(d, string(flow.Done)) || !strings.Contains(d, "bflow tracker setup") {
			t.Errorf("faltan fases: %s %s", st, d)
		}
	})
	t.Run("token o repo inválidos fallan", func(t *testing.T) {
		tr := &ghFake{Memory: trackertest.NewMemory(), stateErr: errors.New("token inválido o vencido (bflow connect github)")}
		st, d := doctorTrackerItem(t, ghEnv(t, strings.Replace(ghYAML, "%s", "", 1), tr))
		if st != "fail" || !strings.Contains(d, "bflow connect github") {
			t.Errorf("%s %s", st, d)
		}
	})
	t.Run("project que el token no ve falla", func(t *testing.T) {
		tr := &ghFake{Memory: trackertest.NewMemory(), states: allStates(), projects: []tracker.Project{{ID: "acme/9", Name: "Otro"}}}
		st, d := doctorTrackerItem(t, ghEnv(t, strings.Replace(ghYAML, "%s", ", project: acme/7", 1), tr))
		if st != "fail" || !strings.Contains(d, "acme/7") {
			t.Errorf("%s %s", st, d)
		}
	})
	t.Run("project visible con Status completo", func(t *testing.T) {
		tr := &ghFake{Memory: trackertest.NewMemory(), states: allStates(), projects: []tracker.Project{{ID: "acme/7", Name: "Tablero"}}}
		st, d := doctorTrackerItem(t, ghEnv(t, strings.Replace(ghYAML, "%s", ", project: acme/7", 1), tr))
		if st != "ok" || !strings.Contains(d, "acme/7") {
			t.Errorf("%s %s", st, d)
		}
	})
}
