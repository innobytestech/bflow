package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/metrics"
)

const cacheDir = ".bflow/cache/evals"

const runStamp = "2006-01-02-150405"

// storedCase es el caso tal como se leyó al abrir la corrida: ediciones
// posteriores de case.yaml no la afectan.
type storedCase struct {
	Name    string   `json:"name"`
	Base    string   `json:"base"` // SHA
	Head    string   `json:"head"` // SHA
	Spec    string   `json:"spec,omitempty"`
	Defects []Defect `json:"defects"`
}

type storedResult struct {
	CaseResult
	TokensDone bool `json:"tokens_done"`
}

// runState es run.json de la corrida abierta.
type runState struct {
	Run      string         `json:"run"`
	Craft    string         `json:"craft_ref"`
	CraftSHA string         `json:"craft_sha256"`
	Cases    []storedCase   `json:"cases"`
	Results  []storedResult `json:"results"`
	Launched bool           `json:"launched"` // el caso len(Results) está lanzado y sin reporte
	Spawned  time.Time      `json:"spawned"`
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) root() string {
	if abs, err := filepath.Abs(r.Root); err == nil {
		return abs
	}
	return r.Root
}

func (r *Runner) cachePath() string { return filepath.Join(r.root(), filepath.FromSlash(cacheDir)) }

func (r *Runner) runDir(run string) string { return filepath.Join(r.cachePath(), run) }

// openRun busca la corrida abierta: la carpeta con run.json (la más reciente).
func (r *Runner) openRun() (string, bool) {
	ents, _ := os.ReadDir(r.cachePath())
	for i := len(ents) - 1; i >= 0; i-- {
		if !ents[i].IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(r.runDir(ents[i].Name()), "run.json")); err == nil {
			return ents[i].Name(), true
		}
	}
	return "", false
}

// Open dice qué corrida está abierta ("" si ninguna).
func (r *Runner) Open() string {
	run, _ := r.openRun()
	return run
}

func (r *Runner) load(run string) (*runState, error) {
	raw, err := os.ReadFile(filepath.Join(r.runDir(run), "run.json"))
	if err != nil {
		return nil, err
	}
	var st runState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("run.json de %s ilegible (%v): cancela con bflow eval reviewer --abort", run, err)
	}
	return &st, nil
}

func (r *Runner) save(st *runState) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.runDir(st.Run), "run.json"), raw, 0o644)
}

// cleanup quita los worktrees que queden de la corrida y borra su carpeta.
// Devuelve los worktrees que git no pudo quitar.
func (r *Runner) cleanup(ctx context.Context, run string) []string {
	dir := r.runDir(run)
	var failed []string
	subs, _ := os.ReadDir(dir)
	for _, s := range subs {
		wt := filepath.Join(dir, s.Name(), "wt")
		if _, err := os.Stat(wt); err != nil {
			continue
		}
		if err := r.Git.WorktreeRemove(ctx, wt); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", s.Name(), err))
		}
	}
	// Solo se borra dentro de .bflow/cache/evals/.
	if rel, err := filepath.Rel(r.cachePath(), dir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		_ = os.RemoveAll(dir)
	}
	return failed
}

func (r *Runner) spawnOf(st *runState, i int) *Spawn {
	c := st.Cases[i]
	dir := filepath.Join(r.runDir(st.Run), c.Name)
	args := map[string]string{
		"id":       c.Name,
		"base":     c.Base,
		"worktree": filepath.Join(dir, "wt"),
		"out":      filepath.Join(dir, "out"),
		"craft":    filepath.Join(r.runDir(st.Run), "craft.md"),
	}
	if c.Spec != "" {
		args["spec"] = c.Spec
	}
	return &Spawn{Case: c.Name, Args: args, Report: "bflow eval reviewer report " + c.Name + " --verdict APPROVED|REJECTED"}
}

// prepare arma el worktree y la carpeta out del caso i y lo marca lanzado.
func (r *Runner) prepare(ctx context.Context, st *runState, i int) error {
	sp := r.spawnOf(st, i)
	if err := r.Git.WorktreeAdd(ctx, sp.Args["worktree"], st.Cases[i].Head); err != nil {
		return err
	}
	if err := os.MkdirAll(sp.Args["out"], 0o755); err != nil {
		return err
	}
	st.Launched = true
	st.Spawned = r.now()
	return r.save(st)
}

// fail limpia la corrida y devuelve el error original.
func (r *Runner) fail(ctx context.Context, run string, err error) error {
	r.cleanup(ctx, run)
	return err
}

func (r *Runner) readCraft(ctx context.Context, ref string) ([]byte, error) {
	if ref == "" {
		b, err := os.ReadFile(filepath.Join(r.root(), filepath.FromSlash(CraftPath)))
		if err != nil {
			return nil, &Rejection{"craft_missing", CraftPath + " no existe en el árbol"}
		}
		return b, nil
	}
	if strings.HasPrefix(ref, "-") {
		return nil, &Rejection{"craft_ref_unknown", fmt.Sprintf("%q no es un commit", ref)}
	}
	if _, err := r.Git.ResolveCommit(ctx, ref); err != nil {
		return nil, &Rejection{"craft_ref_unknown", fmt.Sprintf("%q no es un commit", ref)}
	}
	b, err := r.Git.ShowFile(ctx, ref, CraftPath)
	if err != nil {
		return nil, &Rejection{"craft_missing", fmt.Sprintf("%s no existe en %s", CraftPath, ref)}
	}
	return b, nil
}

// Next avanza la corrida: la abre, retoma el caso en curso, prepara el
// siguiente o cierra con el resultado.
func (r *Runner) Next(ctx context.Context, o Opts) (Step, error) {
	run, open := r.openRun()
	if o.Abort {
		if open {
			if failed := r.cleanup(ctx, run); len(failed) > 0 {
				return Step{Warnings: []string{"no se pudo quitar el worktree de " + strings.Join(failed, "; ")}}, nil
			}
		}
		return Step{}, nil
	}
	if open {
		return r.resume(ctx, run, o)
	}
	return r.start(ctx, o)
}

func (r *Runner) start(ctx context.Context, o Opts) (Step, error) {
	cases, skipped, err := LoadCases(r.root(), o.Cases)
	if err != nil {
		return Step{}, err
	}
	var problems []string
	for _, c := range cases {
		problems = append(problems, c.Problems(ctx, r.Git)...)
	}
	if len(problems) > 0 {
		return Step{}, &Rejection{"case_invalid", strings.Join(problems, "\n")}
	}
	craft, err := r.readCraft(ctx, o.Craft)
	if err != nil {
		return Step{}, err
	}
	sum := sha256.Sum256(craft)
	st := &runState{Run: r.now().Format(runStamp), Craft: o.Craft, CraftSHA: hex.EncodeToString(sum[:])}
	for _, c := range cases {
		sc := storedCase{Name: c.Name, Spec: c.Spec, Defects: c.Defects}
		if sc.Base, err = r.Git.ResolveCommit(ctx, c.Base); err != nil {
			return Step{}, err
		}
		if sc.Head, err = r.Git.ResolveCommit(ctx, c.Head); err != nil {
			return Step{}, err
		}
		st.Cases = append(st.Cases, sc)
	}
	if err := os.MkdirAll(r.runDir(st.Run), 0o755); err != nil {
		return Step{}, err
	}
	if err := os.WriteFile(filepath.Join(r.runDir(st.Run), "craft.md"), craft, 0o644); err != nil {
		return Step{}, r.fail(ctx, st.Run, err)
	}
	warnings, err := Versioning(ctx, r.root(), r.Git)
	if err != nil {
		return Step{}, r.fail(ctx, st.Run, err)
	}
	if err := r.prepare(ctx, st, 0); err != nil {
		return Step{}, r.fail(ctx, st.Run, err)
	}
	return Step{Spawn: r.spawnOf(st, 0), Skipped: skipped, Warnings: warnings}, nil
}

func (r *Runner) resume(ctx context.Context, run string, o Opts) (Step, error) {
	if o.Craft != "" || len(o.Cases) > 0 {
		return Step{}, &Rejection{"eval_open", fmt.Sprintf("la corrida %s sigue abierta: termínala con `bflow eval reviewer` o cancélala con `bflow eval reviewer --abort`", run)}
	}
	st, err := r.load(run)
	if err != nil {
		return Step{}, err
	}
	r.fillTokens(st)
	if err := r.save(st); err != nil {
		return Step{}, err
	}
	warnings, err := Versioning(ctx, r.root(), r.Git)
	if err != nil {
		return Step{}, err
	}
	switch {
	case st.Launched:
		return Step{Spawn: r.spawnOf(st, len(st.Results)), Warnings: warnings}, nil
	case len(st.Results) < len(st.Cases):
		if err := r.prepare(ctx, st, len(st.Results)); err != nil {
			return Step{}, r.fail(ctx, run, err)
		}
		return Step{Spawn: r.spawnOf(st, len(st.Results)), Warnings: warnings}, nil
	}
	return r.finish(ctx, st, warnings)
}

// fillTokens suma las llamadas del agente en la ventana de cada caso reportado.
func (r *Runner) fillTokens(st *runState) {
	for i := range st.Results {
		res := &st.Results[i]
		if res.TokensDone {
			continue
		}
		u, model, ok := Tokens(filepath.Join(r.root(), ".bflow"), res.Spawned, res.Reported)
		res.TokensDone, res.TokensAvailable = true, ok
		if ok {
			res.Tokens, res.Model = u, model
		} else {
			res.Tokens, res.Model = metrics.Usage{}, r.Model
		}
	}
}

func (r *Runner) finish(ctx context.Context, st *runState, warnings []string) (Step, error) {
	res := &Result{Kind: "reviewer", Run: st.Run, Version: r.Version, Cases: []CaseResult{}}
	res.Craft.Ref, res.Craft.SHA256 = st.Craft, st.CraftSHA
	models := map[string]int{}
	for _, sr := range st.Results {
		c := sr.CaseResult
		res.Cases = append(res.Cases, c)
		res.Totals.Defects += len(c.Caught) + len(c.Missed)
		res.Totals.Caught += len(c.Caught)
		res.Totals.Missed += len(c.Missed)
		res.Totals.FalsePositives += len(c.FalsePositives)
		res.Totals.Tokens.Add(c.Tokens)
		res.Totals.Seconds += c.Seconds
		models[c.Model]++
	}
	res.Model = r.Model
	best := 0
	for m, n := range models {
		if m != "" && (n > best || (n == best && m < res.Model)) {
			res.Model, best = m, n
		}
	}
	rel := EvalsDir + "/" + st.Run + ".json"
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return Step{}, err
	}
	if err := os.MkdirAll(filepath.Join(r.root(), filepath.FromSlash(EvalsDir)), 0o755); err != nil {
		return Step{}, err
	}
	if err := os.WriteFile(filepath.Join(r.root(), filepath.FromSlash(rel)), append(raw, '\n'), 0o644); err != nil {
		return Step{}, err
	}
	if failed := r.cleanup(ctx, st.Run); len(failed) > 0 {
		warnings = append(warnings, "no se pudo quitar el worktree de "+strings.Join(failed, "; "))
	}
	return Step{Result: res, Path: rel, Warnings: warnings}, nil
}

// Report califica el caso en curso con el review-map y los .md de su out.
func (r *Runner) Report(ctx context.Context, caseName, verdict, note string) (CaseResult, error) {
	run, open := r.openRun()
	if !open {
		return CaseResult{}, &Rejection{"eval_not_running", "no hay una corrida abierta: empieza con `bflow eval reviewer`"}
	}
	if verdict != "APPROVED" && verdict != "REJECTED" {
		return CaseResult{}, &Rejection{"bad_verdict", fmt.Sprintf("el veredicto es APPROVED o REJECTED, no %q", verdict)}
	}
	st, err := r.load(run)
	if err != nil {
		return CaseResult{}, err
	}
	if !st.Launched || st.Cases[len(st.Results)].Name != caseName {
		pending := "ninguno"
		if st.Launched {
			pending = st.Cases[len(st.Results)].Name
		}
		return CaseResult{}, &Rejection{"case_not_pending", fmt.Sprintf("el caso %q no está en curso (en curso: %s)", caseName, pending)}
	}
	sp := r.spawnOf(st, len(st.Results))
	out, wt := sp.Args["out"], sp.Args["worktree"]
	if _, err := os.Stat(filepath.Join(out, "review-map.md")); err != nil {
		return CaseResult{}, &Rejection{"review_map_missing", fmt.Sprintf("falta %s: escribe el review-map en out y reporta otra vez", filepath.Join(out, "review-map.md"))}
	}
	docs := map[string]string{}
	ents, _ := os.ReadDir(out)
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(out, e.Name())); err == nil {
			docs[e.Name()] = string(b)
		}
	}
	score := ScoreCase(st.Cases[len(st.Results)].Defects, Findings(note, docs, wt))
	now := r.now()
	res := CaseResult{
		Case: caseName, Verdict: verdict, Note: trimWorktree(note, wt), Score: score,
		Spawned: st.Spawned, Reported: now, Seconds: int64(now.Sub(st.Spawned).Seconds()),
		Model: r.Model,
	}
	st.Results = append(st.Results, storedResult{CaseResult: res})
	st.Launched = false
	if err := r.save(st); err != nil {
		return CaseResult{}, err
	}
	// Si git no puede quitarlo, Next lo reintenta al cerrar la corrida.
	_ = r.Git.WorktreeRemove(ctx, wt)
	return res, nil
}

// trimWorktree quita del texto el prefijo del worktree: el resultado no guarda rutas absolutas.
func trimWorktree(s, wt string) string {
	for _, p := range []string{wt, filepath.ToSlash(wt), strings.ReplaceAll(wt, "/", `\`)} {
		s = strings.ReplaceAll(s, p+"/", "")
		s = strings.ReplaceAll(s, p+`\`, "")
	}
	return s
}

// Tokens suma las llamadas no finales del agente del eval en [from, to], de
// las tareas y de las sin tarea, con el modelo que más aparece.
func Tokens(bflowDir string, from, to time.Time) (u metrics.Usage, model string, ok bool) {
	files, _ := filepath.Glob(filepath.Join(bflowDir, "tasks", "*", metrics.CallsFile))
	files = append(files, metrics.UnassignedPath(bflowDir))
	var calls []metrics.Call
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		for _, c := range metrics.ParseCalls(fh) {
			if c.Agent == AgentName && !c.Final && !c.TS.Before(from) && !c.TS.After(to) {
				calls = append(calls, c)
			}
		}
		fh.Close()
	}
	if len(calls) == 0 {
		return metrics.Usage{}, "", false
	}
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].TS.Before(calls[j].TS) })
	count, last := map[string]int{}, map[string]int{}
	for i, c := range calls {
		if c.Model != "" {
			count[c.Model]++
			last[c.Model] = i
		}
	}
	for m, n := range count {
		if model == "" || n > count[model] || (n == count[model] && last[m] > last[model]) {
			model = m
		}
	}
	for _, run := range metrics.Runs(calls) {
		u.Add(run.Total)
	}
	return u, model, true
}
