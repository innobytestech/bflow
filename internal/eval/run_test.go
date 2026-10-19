package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func cacheRel(parts ...string) string {
	return strings.Join(append([]string{".bflow/cache/evals", runName}, parts...), "/")
}

func TestNextSpawnsFirstCase(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "spec: specs/x/spec.md\n")
	h.validCase("beta", "")
	h.caseYAML("gama", "draft: true\ndefects: []\n")

	st := h.next(Opts{})
	sp := st.Spawn
	if sp == nil || sp.Case != "alfa" || st.Result != nil {
		t.Fatalf("primer caso pendiente: %+v", st)
	}
	if !slices.Equal(st.Skipped, []string{"gama"}) {
		t.Errorf("skipped: %v", st.Skipped)
	}
	wt := filepath.Join(h.root, ".bflow", "cache", "evals", runName, "alfa", "wt")
	out := filepath.Join(h.root, ".bflow", "cache", "evals", runName, "alfa", "out")
	if sp.Args["id"] != "alfa" || sp.Args["base"] != shaBase || filepath.Clean(sp.Args["worktree"]) != wt ||
		filepath.Clean(sp.Args["out"]) != out || sp.Args["craft"] == "" || sp.Args["spec"] != "specs/x/spec.md" {
		t.Errorf("args: %v", sp.Args)
	}
	if sp.Report != "bflow eval reviewer report alfa --verdict APPROVED|REJECTED" {
		t.Errorf("report: %q", sp.Report)
	}
	if !slices.Equal(h.g.adds, []string{wt + "@" + shaHead}) {
		t.Errorf("worktree desacoplado en head: %v", h.g.adds)
	}
	if ents, err := os.ReadDir(out); err != nil || len(ents) != 0 {
		t.Errorf("out existe y está vacío: %v %v", ents, err)
	}
	if !h.exists(cacheRel("run.json")) || h.exists(cacheRel("beta")) {
		t.Error("run.json existe y beta no se prepara todavía")
	}

	// Un caso sin spec no lleva ese arg.
	h2 := newHarness(t)
	h2.validCase("solo", "")
	if _, ok := h2.next(Opts{}).Spawn.Args["spec"]; ok {
		t.Error("sin spec declarada no hay arg spec")
	}
}

func TestNextCaseInvalid(t *testing.T) {
	h := newHarness(t)
	h.validCase("bueno", "")
	h.caseYAML("malo", "base: base1\nhead: head1\ndefects: []\n")
	h.caseYAML("peor", "base: nope\nhead: head1\ndefects:\n  - { file: ../a.go, lines: \"1\", what: x }\n")
	_, err := h.r.Next(context.Background(), Opts{})
	rj := rejected(t, err, "case_invalid")
	lines := strings.Split(strings.TrimSpace(rj.Reason), "\n")
	var malo, peor int
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "malo: "):
			malo++
		case strings.HasPrefix(l, "peor: "):
			peor++
		default:
			t.Errorf("línea inesperada %q", l)
		}
	}
	if malo != 1 || peor != 2 {
		t.Errorf("una línea por problema: malo=%d peor=%d\n%s", malo, peor, rj.Reason)
	}
	if h.exists(".bflow/cache/evals") || len(h.g.adds) != 0 {
		t.Error("sin corrida ni worktrees")
	}
	// Un caso malo que no se selecciona no estorba.
	if st, err := h.r.Next(context.Background(), Opts{Cases: []string{"bueno"}}); err != nil || st.Spawn == nil {
		t.Errorf("--case bueno: %v", err)
	}
}

func TestNextRepeatsPendingSpawn(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.validCase("beta", "")
	first := h.next(Opts{})
	h.now = h.now.Add(time.Minute)
	again := h.next(Opts{})
	if again.Spawn == nil || again.Spawn.Case != "alfa" || again.Spawn.Report != first.Spawn.Report {
		t.Fatalf("mismo spawn: %+v", again.Spawn)
	}
	for k, v := range first.Spawn.Args {
		if again.Spawn.Args[k] != v {
			t.Errorf("arg %s cambió: %q → %q", k, v, again.Spawn.Args[k])
		}
	}
	if len(h.g.adds) != 1 || len(h.g.removes) != 0 {
		t.Errorf("no recrea ni prepara otro: adds %v removes %v", h.g.adds, h.g.removes)
	}
}

func TestNextOpenRunRejectsFlags(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.next(Opts{})
	for _, o := range []Opts{{Craft: "v1"}, {Cases: []string{"alfa"}}, {Craft: "v1", Cases: []string{"alfa"}}} {
		_, err := h.r.Next(context.Background(), o)
		rj := rejected(t, err, "eval_open")
		if !strings.Contains(rj.Reason, runName) || !strings.Contains(rj.Reason, "--abort") {
			t.Errorf("eval_open nombra la corrida y cómo cancelarla: %q", rj.Reason)
		}
	}
	if len(h.g.adds) != 1 {
		t.Error("el rechazo no toca la corrida")
	}
}

func TestReportScoresAndRemovesWorktree(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.validCase("beta", "")
	sp := h.next(Opts{}).Spawn
	wtDir := sp.Args["worktree"]
	abs := filepath.ToSlash(wtDir) + "/internal/a.go:11"
	h.answer(sp, "# Review\n- "+abs+" falta validar\n- internal/zzz.go:5 ruido\n\n## Docs\n- docs/x.md:3 ignorado\n")
	h.now = h.now.Add(90 * time.Second)

	res := h.report("alfa", "REJECTED", "ver internal/q.go:1")
	if res.Case != "alfa" || res.Verdict != "REJECTED" || res.Note != "ver internal/q.go:1" {
		t.Errorf("resultado: %+v", res)
	}
	if len(res.Caught) != 1 || len(res.Missed) != 0 {
		t.Errorf("atrapó el defecto con ruta absoluta del worktree: %+v", res.Score)
	}
	fp := slices.Clone(res.FalsePositives)
	slices.Sort(fp)
	if !slices.Equal(fp, []string{"internal/q.go:1", "internal/zzz.go:5"}) {
		t.Errorf("falsos positivos (la nota cuenta, ## Docs no): %v", res.FalsePositives)
	}
	if !res.Reported.Equal(h.now) || !res.Spawned.Equal(h.now.Add(-90*time.Second)) {
		t.Errorf("horas: spawn %v report %v", res.Spawned, res.Reported)
	}
	if !slices.Equal(h.g.removes, []string{wtDir}) || len(h.g.live) != 0 {
		t.Errorf("quita el worktree: %v", h.g.removes)
	}
	raw := h.read(cacheRel("run.json"))
	if !strings.Contains(raw, "REJECTED") || !strings.Contains(raw, "ver internal/q.go:1") {
		t.Errorf("run.json guarda verdict y note: %s", raw)
	}
	// No aplica las reglas del review-map del flujo: basta que exista, sin formato.
	h.answer(h.next(Opts{}).Spawn, "texto libre sin secciones\n")
	if _, err := h.r.Report(context.Background(), "beta", "APPROVED", ""); err != nil {
		t.Errorf("review-map sin formato: %v", err)
	}
}

func TestReportKeepsResultWhenRemoveFails(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	sp := h.next(Opts{}).Spawn
	h.answer(sp, "internal/a.go:10\n")
	h.g.removeErr = errors.New("ocupado")
	res, err := h.r.Report(context.Background(), "alfa", "REJECTED", "")
	if err != nil || len(res.Caught) != 1 {
		t.Fatalf("el resultado se guarda aunque falle quitar el worktree: %v %+v", err, res)
	}
	h.g.removeErr = nil
	done := h.next(Opts{})
	if done.Result == nil {
		t.Fatalf("cierra la corrida: %+v", done)
	}
	if n := countOf(h.g.removes, sp.Args["worktree"]); n < 2 {
		t.Errorf("reintenta quitarlo al cerrar: %v", h.g.removes)
	}
	if len(h.g.live) != 0 {
		t.Errorf("no queda ningún worktree: %v", h.g.live)
	}
}

func countOf(xs []string, s string) int {
	n := 0
	for _, x := range xs {
		if x == s {
			n++
		}
	}
	return n
}

func TestReportRejections(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.validCase("beta", "")
	ctx := context.Background()

	_, err := h.r.Report(ctx, "alfa", "APPROVED", "")
	_ = rejected(t, err, "eval_not_running")

	sp := h.next(Opts{}).Spawn
	_, err = h.r.Report(ctx, "alfa", "TAL_VEZ", "")
	_ = rejected(t, err, "bad_verdict")
	_, err = h.r.Report(ctx, "alfa", "approved", "")
	_ = rejected(t, err, "bad_verdict")
	_, err = h.r.Report(ctx, "beta", "APPROVED", "")
	_ = rejected(t, err, "case_not_pending")
	_, err = h.r.Report(ctx, "nada", "APPROVED", "")
	_ = rejected(t, err, "case_not_pending")
	_, err = h.r.Report(ctx, "alfa", "APPROVED", "")
	_ = rejected(t, err, "review_map_missing")
	if len(h.g.removes) != 0 {
		t.Error("un rechazo no quita el worktree")
	}

	// Tras escribir el review-map, el mismo caso sigue pendiente y se acepta.
	h.answer(sp, "ok\n")
	h.report("alfa", "APPROVED", "")
	_, err = h.r.Report(ctx, "alfa", "APPROVED", "")
	_ = rejected(t, err, "case_not_pending")
}

func writeCalls(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func call(ts time.Time, agent, model, msg string, in, out, cw, cr int, final bool) string {
	b, _ := json.Marshal(map[string]any{"ts": ts.UTC().Format(time.RFC3339Nano), "run": "claude:r-" + msg, "tool": "claude",
		"agent": agent, "model": model, "msg": msg, "input": in, "output": out, "cache_write": cw, "cache_read": cr, "final": final})
	return string(b)
}

func TestTokensFromCallsWindow(t *testing.T) {
	dir := t.TempDir()
	from := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	writeCalls(t, filepath.Join(dir, "tasks", "GH-1", "calls.jsonl"),
		call(from, "eval-reviewer", "m-a", "m1", 10, 5, 2, 100, false),                        // borde inicial
		call(from.Add(time.Minute), "eval-reviewer", "m-a", "m2", 20, 6, 3, 200, false),       // dentro
		call(from.Add(2*time.Minute), "eval-reviewer", "m-b", "m3", 30, 7, 4, 300, false),     // otro modelo
		call(from.Add(-time.Second), "eval-reviewer", "m-a", "m4", 900, 900, 900, 900, false), // antes
		call(to.Add(time.Second), "eval-reviewer", "m-a", "m5", 900, 900, 900, 900, false),    // después
		call(from.Add(time.Minute), "reviewer", "m-a", "m6", 900, 900, 900, 900, false),       // otro agente
		call(from.Add(time.Minute), "eval-reviewer", "m-a", "m7", 900, 900, 900, 900, true),   // cifra final
	)
	writeCalls(t, filepath.Join(dir, "metrics", "calls.jsonl"),
		call(to, "eval-reviewer", "m-a", "m8", 40, 8, 5, 400, false), // borde final
	)
	u, model, ok := Tokens(dir, from, to)
	if !ok || model != "m-a" {
		t.Fatalf("ok=%v model=%q", ok, model)
	}
	if u.Input != 100 || u.Output != 26 || u.CacheWrite != 14 || u.CacheRead != 1000 || u.Calls != 4 {
		t.Errorf("suma de las llamadas no finales del agente en la ventana: %+v", u)
	}

	if u, model, ok := Tokens(dir, to.Add(time.Hour), to.Add(2*time.Hour)); ok || model != "" || u.Total() != 0 {
		t.Errorf("sin llamadas: %+v %q %v", u, model, ok)
	}
	if _, _, ok := Tokens(t.TempDir(), from, to); ok {
		t.Error("sin archivos de llamadas")
	}
}

func TestNextWritesResult(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.validCase("beta", "")
	t0 := h.now
	h.write(".bflow/tasks/GH-9/calls.jsonl", call(t0.Add(10*time.Second), "eval-reviewer", "m-a", "x1", 10, 5, 2, 100, false)+"\n")

	sp := h.next(Opts{}).Spawn
	h.answer(sp, "internal/a.go:11 falta\ninternal/ruido.go:3 ruido\n")
	h.now = t0.Add(60 * time.Second)
	h.report("alfa", "REJECTED", "")

	sp = h.next(Opts{}).Spawn
	if sp == nil || sp.Case != "beta" {
		t.Fatalf("siguiente caso: %+v", sp)
	}
	h.answer(sp, "todo bien\n")
	h.now = t0.Add(90 * time.Second)
	h.report("beta", "APPROVED", "")

	done := h.next(Opts{})
	if done.Spawn != nil || done.Result == nil {
		t.Fatalf("cierre: %+v", done)
	}
	if want := ".bflow/evals/" + runName + ".json"; done.Path != want {
		t.Errorf("ruta del resultado: %q, quería %q", done.Path, want)
	}
	if h.exists(cacheRel()) {
		t.Error("la carpeta de la corrida se borra")
	}
	var onDisk map[string]any
	if err := json.Unmarshal([]byte(h.read(done.Path)), &onDisk); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(treeCraft))
	craft := onDisk["craft"].(map[string]any)
	totals := onDisk["totals"].(map[string]any)
	if onDisk["kind"] != "reviewer" || onDisk["run"] != runName || onDisk["version"] != "v-test" || onDisk["model"] == "" ||
		craft["ref"] != "" || craft["sha256"] != hex.EncodeToString(sum[:]) {
		t.Errorf("encabezado: %v", onDisk)
	}
	if totals["defects"] != 2.0 || totals["caught"] != 1.0 || totals["missed"] != 1.0 || totals["false_positives"] != 1.0 || totals["seconds"] != 90.0 {
		t.Errorf("totales: %v", totals)
	}
	if tok := totals["tokens"].(map[string]any); tok["input"] != 10.0 || tok["output"] != 5.0 {
		t.Errorf("tokens totales: %v", tok)
	}
	cases := onDisk["cases"].([]any)
	if len(cases) != 2 {
		t.Fatalf("casos: %v", cases)
	}
	a, b := cases[0].(map[string]any), cases[1].(map[string]any)
	if a["case"] != "alfa" || a["tokens_available"] != true || a["model"] != "m-a" || a["seconds"] != 60.0 {
		t.Errorf("alfa: %v", a)
	}
	if b["case"] != "beta" || b["tokens_available"] != false || b["model"] != "m-config" || b["seconds"] != 30.0 {
		t.Errorf("beta sin llamadas usa el modelo configurado: %v", b)
	}
	if _, ok := onDisk["craft"].(map[string]any)["text"]; ok || strings.Contains(h.read(done.Path), "revisa con cuidado") || strings.Contains(h.read(done.Path), filepath.ToSlash(h.root)) {
		t.Error("el resultado no guarda el texto del oficio ni rutas absolutas")
	}
	if done.Result.Totals.Defects != 2 || done.Result.Totals.Caught != 1 || done.Result.Run != runName {
		t.Errorf("Step.Result: %+v", done.Result.Totals)
	}

	// Sin corrida abierta y sin flags, vuelve a empezar desde cero.
	h.now = h.now.Add(time.Hour)
	if again := h.next(Opts{}); again.Spawn == nil || again.Spawn.Case != "alfa" {
		t.Errorf("nueva corrida: %+v", again)
	}
}

func TestAbortCleansUp(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.validCase("beta", "")
	sp := h.next(Opts{}).Spawn

	st := h.next(Opts{Abort: true})
	if st.Spawn != nil || st.Result != nil {
		t.Errorf("abort no lanza ni cierra: %+v", st)
	}
	if !slices.Equal(h.g.removes, []string{sp.Args["worktree"]}) || len(h.g.live) != 0 {
		t.Errorf("quita el worktree: %v", h.g.removes)
	}
	if h.exists(cacheRel()) {
		t.Error("borra la carpeta de la corrida")
	}
	if h.exists(".bflow/evals/" + runName + ".json") {
		t.Error("abort no escribe resultado")
	}
	before := len(h.g.removes)
	if _, err := h.r.Next(context.Background(), Opts{Abort: true}); err != nil || len(h.g.removes) != before {
		t.Errorf("sin corrida abierta no hace nada: %v", err)
	}
}

func TestPrepareFailureCleansUp(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.validCase("beta", "")
	h.g.failAdd = "/beta/"
	sp := h.next(Opts{}).Spawn
	h.answer(sp, "x\n")
	h.report("alfa", "APPROVED", "")

	_, err := h.r.Next(context.Background(), Opts{})
	var rj *Rejection
	if err == nil || errors.As(err, &rj) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("el error de git sube tal cual: %v", err)
	}
	if h.exists(cacheRel()) || len(h.g.live) != 0 {
		t.Errorf("limpia la corrida y sus worktrees: live %v", h.g.live)
	}
	if h.exists(".bflow/evals/" + runName + ".json") {
		t.Error("una corrida fallida no escribe resultado")
	}

	// Falla en el primer caso al arrancar: tampoco queda nada.
	h2 := newHarness(t)
	h2.validCase("alfa", "")
	h2.g.failAdd = "/alfa/"
	if _, err := h2.r.Next(context.Background(), Opts{}); err == nil {
		t.Fatal("debe fallar")
	}
	if h2.exists(".bflow/cache/evals/" + runName) {
		t.Error("el arranque fallido no deja corrida")
	}
}

func TestGitignoreExceptions(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.write(".bflow/.gitignore", "*\n")
	h.next(Opts{})
	lines := strings.Split(strings.TrimSpace(h.read(".bflow/.gitignore")), "\n")
	if lines[0] != "*" || countOf(lines, "!evals/") != 1 || countOf(lines, "!evals/**") != 1 {
		t.Errorf(".gitignore: %q", lines)
	}
	h.next(Opts{}) // repetir no duplica
	lines = strings.Split(strings.TrimSpace(h.read(".bflow/.gitignore")), "\n")
	if countOf(lines, "!evals/") != 1 || countOf(lines, "!evals/**") != 1 || len(lines) != 3 {
		t.Errorf("idempotente: %q", lines)
	}
	if st := h.next(Opts{}); len(st.Warnings) != 0 {
		t.Errorf("git no ignora: sin avisos: %v", st.Warnings)
	}
	if len(h.g.checked) == 0 || !strings.Contains(h.g.checked[0], ".bflow/evals/reviewer") {
		t.Errorf("comprueba si git ignora .bflow/evals/reviewer/: %v", h.g.checked)
	}

	// Si git sigue ignorando la carpeta, avisa qué cambiar en el .gitignore raíz.
	h2 := newHarness(t)
	h2.validCase("alfa", "")
	h2.g.ignored = true
	st := h2.next(Opts{})
	if len(st.Warnings) != 1 || !strings.Contains(st.Warnings[0], ".gitignore") || !strings.Contains(st.Warnings[0], "!.bflow/evals/") {
		t.Errorf("aviso: %v", st.Warnings)
	}
	if !strings.Contains(h2.read(".bflow/.gitignore"), "!evals/**") {
		t.Error("sin .gitignore previo también lo crea")
	}
}
