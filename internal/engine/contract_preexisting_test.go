package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/check"
	"innobytes.tech/bflow/internal/flow"
)

const (
	preYAML  = "stack: go\nvcs:\n  base_branch: dev\ncheck:\n  steps:\n    - { name: theme, run: \"npm run check:theme\" }\n    - { name: test, run: \"go test ./...\" }\n"
	baseFile = "src/app/modal/modal.css" // rojo que ya estaba en la base
	mineFile = "src/app/cart/cart.ts"    // archivo de producción que la rama tocó
	goneFile = "src/app/cart/borrado.ts" // archivo que la rama borró o renombró
)

// preEnv deja la tarea en contract con un check vigente: theme rojo con la
// salida resumida en blame y test rojo por una T1.
func preEnv(t *testing.T, blame *check.Blame) (*env, string) {
	t.Helper()
	res := &check.Result{SHA: "abc", Result: "FAIL", Steps: []check.StepResult{
		{Name: "theme", Cmd: "npm run check:theme", Status: "fail", Tail: "theme check: 1 violation(s):\n  " + baseFile + ":3 - radio", Blame: blame},
		{Name: "test", Status: "fail", Fails: []string{"--- FAIL: TestSuma (0.00s)"}},
	}}
	v, id := contractEnv(t, preYAML, &fakeContract{res: res, fresh: true})
	g := v.e.Git.(*fakeGit)
	g.diff, g.removed = []string{t1File, mineFile}, []string{goneFile}
	put(t, v, baseFile, []byte("a { border-radius: 4px }"))
	put(t, v, mineFile, []byte("export const x = 1;"))
	return v, id
}

func TestContractIgnoresPreexistingRed(t *testing.T) {
	// «src/app» es el alcance del comando y docs/guia.md un texto de ayuda: no
	// culpan a lo que la rama tocó dentro de ellos.
	v, id := preEnv(t, &check.Blame{Findings: []string{baseFile}, Mentions: []string{"scripts/check-theme.js", "src/app", "docs/guia.md"}})
	put(t, v, "scripts/check-theme.js", []byte("x"))
	put(t, v, "docs/guia.md", []byte("x"))
	g := v.e.Git.(*fakeGit)
	g.diff = append(g.diff, "docs/otra.md", "CHANGELOG.md")
	o, err := contractReady(v, id)
	if err != nil {
		t.Fatalf("theme solo falla en archivos que la rama no tocó: %v", err)
	}
	if o.To != flow.Implementing {
		t.Fatalf("debe avanzar: %+v", o)
	}
	if !hasWarning(o, `"theme"`) || !hasWarning(o, baseFile) {
		t.Errorf("el aviso debe nombrar el paso y los archivos: %v", o.Warnings)
	}
	log, err := v.e.Store.Log(id)
	if err != nil {
		t.Fatal(err)
	}
	last := log[len(log)-1]
	ign, _ := last.Data["preexisting_red"].(map[string]any)
	if last.Verdict != string(flow.ContractReady) || ign == nil || ign["theme"] == nil {
		t.Errorf("el log debe registrar el paso ignorado: %+v", last)
	}
}

func TestContractPreexistingRedStillBlocks(t *testing.T) {
	findings := func(mentions ...string) *check.Blame {
		return &check.Blame{Findings: []string{baseFile}, Mentions: mentions}
	}
	cases := []struct {
		name      string
		blame     *check.Blame
		touch     []string // más archivos tocados por la rama
		disk      []string // archivos que existen en el repo
		cmd       string
		dirty     []string
		untracked []string
		err       error
		noBase    bool
		noGit     bool
		why       string
	}{
		{name: "hallazgo en un archivo tocado", blame: &check.Blame{Findings: []string{baseFile, mineFile}}, why: mineFile},
		{name: "mayúsculas distintas", blame: &check.Blame{Findings: []string{"SRC/app/cart/Cart.ts"}}, why: mineFile},
		{name: "mención de un archivo borrado", blame: findings(goneFile), why: goneFile},
		{name: "import sin extensión de un archivo borrado", blame: findings("../cart/borrado"), why: goneFile},
		{name: "ruta absoluta de otro clon", blame: findings("C:/otro/clon/" + mineFile), why: mineFile},
		{name: "archivo sin commitear", blame: findings(), dirty: []string{baseFile}, why: baseFile},
		{name: "directorio tocado", blame: &check.Blame{Findings: []string{"libs/shared/x.ts"}}, touch: []string{"libs/shared"}, disk: []string{"libs/shared/x.ts"}, why: "libs/shared"},
		{name: "manifiesto de dependencias", blame: findings(), touch: []string{"package-lock.json"}, why: "package-lock.json"},
		{name: "archivo oculto de configuración", blame: findings(), touch: []string{"src/.stylelintrc.json"}, why: ".stylelintrc.json"},
		{name: "tsconfig", blame: findings(), touch: []string{"src/tsconfig.app.json"}, why: "tsconfig.app.json"},
		{name: "configuración en la raíz", blame: findings(), touch: []string{"eslint.config.js"}, why: "eslint.config.js"},
		{name: "línea base junto al script", blame: findings("scripts/check-theme.js"), disk: []string{"scripts/check-theme.js"}, touch: []string{"scripts/theme-baseline.json"}, why: "theme-baseline.json"},
		{name: "archivo junto al comando", blame: findings(), cmd: "node tools/lint.js", disk: []string{"tools/lint.js"}, touch: []string{"tools/rules.json"}, why: "rules.json"},
		{name: "vecino del hallazgo", blame: findings(), touch: []string{"src/app/modal/modal.html"}, why: "modal.html"},
		{name: "import relativo de una sola parte", blame: findings("./viejo"), touch: []string{"src/app/modal/viejo.ts"}, why: "viejo.ts"},
		{name: "herramienta en un directorio del comando", blame: findings(), cmd: "go run ./cmd/quality-check", disk: []string{"cmd/quality-check/main.go"}, touch: []string{"cmd/quality-check/rules/rules.go"}, why: "rules.go"},
		{name: "configuración de la raíz sin «config» en el nombre", blame: findings(), touch: []string{"Makefile"}, why: "Makefile"},
		{name: "configuración conocida en una subcarpeta", blame: findings(), touch: []string{"packages/web/eslint.config.mjs"}, why: "eslint.config.mjs"},
		{name: "ruta citada por git", blame: findings(), touch: []string{`"scripts/l\303\255nea-base.json"`}, why: "fuera de ASCII"},
		{name: "hallazgo que git no rastrea", blame: findings(), untracked: []string{baseFile}, why: "no rastrea"},
		{name: "sin rutas", blame: nil, why: "hallazgos atribuibles"},
		{name: "solo menciones", blame: &check.Blame{Mentions: []string{baseFile}}, why: "hallazgos atribuibles"},
		{name: "compilación caída", blame: &check.Blame{Findings: []string{baseFile}, Opaque: "el paso compila o revisa tipos"}, why: "compila"},
		{name: "demasiadas rutas", blame: &check.Blame{Findings: []string{baseFile}, More: true}, why: "más rutas"},
		{name: "hallazgo fuera del repo", blame: &check.Blame{Findings: []string{baseFile, "src/app/no-existe.ts"}}, why: "no-existe.ts"},
		{name: "hallazgo en dependencias", blame: &check.Blame{Findings: []string{"node_modules/lib/index.js"}}, disk: []string{"node_modules/lib/index.js"}, why: "node_modules"},
		{name: "sin diff", blame: findings(), err: errors.New("bad revision"), why: "bad revision"},
		{name: "sin rama base", noBase: true, blame: findings(), why: "rama base"},
		{name: "sin git", noGit: true, blame: findings(), why: "no tiene git"},
	}
	for _, c := range cases {
		v, id := preEnv(t, c.blame)
		for _, f := range c.disk {
			put(t, v, f, []byte("x"))
		}
		res, _, _, _ := v.e.Contract.Last(context.Background(), id)
		if c.cmd != "" {
			res.Steps[0].Cmd = c.cmd
		}
		g := v.e.Git.(*fakeGit)
		g.diff = append(g.diff, c.touch...)
		g.dirty, g.diffErr, g.untracked = c.dirty, c.err, c.untracked
		if c.noBase {
			v.e.Cfg.VCS.BaseBranch = ""
		}
		if c.noGit {
			v.e.Git = nil
		}
		_, err := contractReady(v, id)
		if err == nil {
			t.Errorf("%s: debe rechazar", c.name)
			continue
		}
		rj := rejectionOf(t, err)
		if rj.Code != "contract_broken" || !strings.Contains(rj.Reason, `"theme"`) || !strings.Contains(rj.Reason, c.why) {
			t.Errorf("%s: %s %q", c.name, rj.Code, rj.Reason)
		}
		if phaseOf(t, v, id) != flow.Contract {
			t.Errorf("%s: la fase no debe cambiar", c.name)
		}
	}
}

// Si después se rechaza por otra causa, el rechazo dice qué rojo se ignoró.
func TestContractIgnoredRedShownInRejection(t *testing.T) {
	v, id := preEnv(t, &check.Blame{Findings: []string{baseFile}})
	res, _, _, _ := v.e.Contract.Last(context.Background(), id)
	res.Steps[1] = check.StepResult{Name: "test", Status: "pass"}
	_, err := contractReady(v, id)
	rj := rejectionOf(t, err)
	if rj.Code != "contract_green" || !strings.Contains(rj.Reason, `"theme"`) || !strings.Contains(rj.Reason, baseFile) {
		t.Errorf("%s %q", rj.Code, rj.Reason)
	}

	// Dos pasos previos: el primero se ignora y el segundo rechaza; el rechazo nombra los dos.
	v, id = preEnv(t, &check.Blame{Findings: []string{baseFile}})
	res, _, _, _ = v.e.Contract.Last(context.Background(), id)
	ui := check.StepResult{Name: "ui", Status: "fail", Tail: mineFile + ":1 control", Blame: &check.Blame{Findings: []string{mineFile}}}
	res.Steps = append(res.Steps[:1], append([]check.StepResult{ui}, res.Steps[1:]...)...)
	_, err = contractReady(v, id)
	rj = rejectionOf(t, err)
	if rj.Code != "contract_broken" || !strings.Contains(rj.Reason, `"ui"`) || !strings.Contains(rj.Reason, `se ignoró el paso "theme"`) {
		t.Errorf("%s %q", rj.Code, rj.Reason)
	}
}

// De punta a punta: la salida real de un paso pasa por bflow check, se guarda
// en JSON como check.json y el contrato la lee.
func TestContractPreexistingFromRealCheck(t *testing.T) {
	v, id := preEnv(t, nil)
	out := map[string]string{
		"npm run check:theme": "\n> app@1.0.0 check:theme\n> node scripts/check-theme.js\n\ntheme check: 1 violation(s):\n  " + baseFile + ":3 - radio fuera de escala\n",
		"go test ./...":       "--- FAIL: TestSuma (0.00s)\nFAIL\n",
	}
	r := &check.Runner{Root: v.e.Cfg.Root, Base: "origin/dev", Cfg: v.e.Cfg.Check, Git: v.e.Git, Now: time.Now,
		Exec: func(_ context.Context, _ string, argv []string, shell string) (string, int, error) {
			return out[strings.Join(argv, " ")+shell], 1, nil
		}}
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, _ := json.Marshal(res)
	var back check.Result
	if err := json.Unmarshal(js, &back); err != nil {
		t.Fatal(err)
	}
	v.e.Contract = &fakeContract{res: &back, fresh: true}
	if o, err := contractReady(v, id); err != nil || !hasWarning(o, baseFile) {
		t.Fatalf("debe aceptarse avisando del rojo de la base: %v %v", err, o.Warnings)
	}

	// Mismo paso, pero el hallazgo cae en un archivo que la rama tocó.
	v, id = preEnv(t, nil)
	out["npm run check:theme"] = "theme check: 1 violation(s):\n  " + mineFile + ":1 - radio\n"
	r.Root, r.Git = v.e.Cfg.Root, v.e.Git
	res, _ = r.Run(context.Background())
	v.e.Contract = &fakeContract{res: &res, fresh: true}
	_, err = contractReady(v, id)
	if rj := rejectionOf(t, err); rj.Code != "contract_broken" || !strings.Contains(rj.Reason, mineFile) {
		t.Errorf("%s %q", rj.Code, rj.Reason)
	}
}
