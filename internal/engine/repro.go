package engine

import (
	"context"
	"fmt"
	"strings"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/store"
)

// reproMissing devuelve el rechazo repro_test_missing si la rama del hotfix no
// trae un commit de solo pruebas cuyo check falló en check.test_step.
func (e *Engine) reproMissing(ctx context.Context, rec store.Record) *flow.Rejection {
	id := rec.Flow.ID
	step := e.Cfg.Check.TestStep
	if step == "" {
		step = "test"
	}
	miss := func(format string, a ...any) *flow.Rejection {
		return &flow.Rejection{Code: "repro_test_missing", Reason: fmt.Sprintf(format, a...)}
	}
	known := false
	for _, st := range e.Cfg.Check.Steps {
		if st.Name == step {
			known = true
		}
	}
	if !known {
		return miss("check.test_step es %q y no es ningún paso de check.steps: corrígelo en bflow.yaml para que el DONE pueda verificar la prueba de reproducción.", step)
	}
	base := e.diffBase()
	if base == "" {
		base = rec.StartSHA
	}
	if base == "" {
		return miss("no se puede saber qué commits son de la tarea: configura vcs.base_branch en bflow.yaml.")
	}
	commits, err := e.Git.Commits(ctx, base)
	if err != nil {
		return miss("no se pudieron listar los commits de la tarea: %v", err)
	}
	head, err := e.Git.HeadSHA(ctx)
	if err != nil {
		return miss("no se pudo leer HEAD: %v", err)
	}
	var cands []string
	for _, sha := range commits {
		if sha == head { // HEAD no cuenta; si es un merge ni siquiera viene en la lista
			continue
		}
		files, err := e.Git.CommitKept(ctx, sha)
		if err != nil {
			return miss("no se pudieron leer los archivos del commit %s: %v", short(sha), err)
		}
		if len(files) == 0 {
			continue
		}
		onlyTests := true
		for _, f := range files {
			if !guard.MatchesTest(e.Cfg.Guard.TestPatterns, f) {
				onlyTests = false
				break
			}
		}
		if onlyTests {
			cands = append(cands, sha)
		}
	}
	if len(cands) == 0 {
		return miss("en un hotfix, antes de la corrección hace falta un commit con solo la prueba que reproduce el defecto, y no hay commit de solo pruebas en el rango de la tarea. Haz commit de la prueba sin tocar código, corre `bflow check %s` (debe fallar en %s), y luego la corrección. Si no se puede probar (docs, config, carreras), reporta DONE con --no-repro \"<motivo>\".", id, step)
	}
	var unchecked []string
	for _, sha := range cands {
		found, status, err := e.Checks.StepAt(ctx, id, sha, step)
		if err != nil {
			return miss("no se pudo leer el check del commit %s: %v", short(sha), err)
		}
		if !found {
			unchecked = append(unchecked, short(sha))
			continue
		}
		if status == "fail" {
			return nil
		}
	}
	if len(unchecked) == len(cands) {
		return miss("hay commit(s) de solo pruebas (%s) pero ninguno tiene check guardado. Corre `git switch --detach <sha> && bflow check %s && git switch -` en cada uno hasta ver el fallo del paso %s.", strings.Join(unchecked, ", "), id, step)
	}
	if len(unchecked) > 0 {
		return miss("el paso %s no falló en los commits de solo pruebas con check, y %s no tienen check guardado. Corre `git switch --detach <sha> && bflow check %s && git switch -` en cada uno hasta ver el fallo del paso %s.", step, strings.Join(unchecked, ", "), id, step)
	}
	return miss("hay commit(s) de solo pruebas pero el paso %s no falló en ninguno: la prueba no reproduce el defecto. Corrígela, haz commit y corre `bflow check %s`.", step, id)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
