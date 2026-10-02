package engine

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/review"
	"innobytes.tech/bflow/internal/store"
)

// docsReport es el reporte del documenter con las rutas de ## Docs sin cambio.
const docsReport = "reports/docs.md"

// diffBase es la rama contra la que se compara el trabajo de la tarea
// (remote/base), o "" si no hay.
func (e *Engine) diffBase() string {
	base := e.Cfg.VCS.BaseBranch
	if base != "" && e.Cfg.VCS.Remote != "" {
		base = e.Cfg.VCS.Remote + "/" + base
	}
	return base
}

// reviewMap lee el review-map de la tarea; ok es false si todavía no existe.
func (e *Engine) reviewMap(id string) (m review.Map, ok bool) {
	b, err := e.Store.ReadFile(id, artifacts["review-map"])
	if err != nil {
		return m, false
	}
	return review.ParseMap(string(b)), true
}

// qualityEntered es cuándo la tarea entró a quality por última vez; las
// lecturas anteriores son de otra ronda.
func (e *Engine) qualityEntered(id string) time.Time {
	log, err := e.Store.Log(id)
	if err != nil {
		return time.Time{}
	}
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].To == flow.Quality && log[i].From != flow.Quality {
			return log[i].TS
		}
	}
	return time.Time{}
}

// reviewCoverage mide lo que leyó el reviewer desde la última entrada a quality
// contra el diff de la rama (R6). hasMap dice si el review-map existe. Si git
// falla o no hay base, la cobertura queda sin medir.
func (e *Engine) reviewCoverage(ctx context.Context, id string) (cov review.Coverage, m review.Map, hasMap bool) {
	m, hasMap = e.reviewMap(id)
	base := e.diffBase()
	if e.Git == nil {
		return cov, m, hasMap
	}
	diff, err := e.Git.DiffNames(ctx, base)
	if err != nil {
		return cov, m, hasMap
	}
	lines, err := e.Git.DiffLines(ctx, base)
	if err != nil {
		return cov, m, hasMap
	}
	for i := range diff {
		diff[i] = strings.ReplaceAll(diff[i], `\`, "/")
	}
	var reads []review.Read
	if b, err := e.Store.ReadFile(id, review.ReadsFile); err == nil {
		since := e.qualityEntered(id)
		for _, r := range review.ParseReads(strings.NewReader(string(b))) {
			if !r.TS.Before(since) {
				reads = append(reads, r)
			}
		}
	}
	isTest := func(p string) bool { return guard.MatchesTest(e.Cfg.Guard.TestPatterns, p) }
	return review.Measure(diff, lines, reads, m.Red, isTest, nil), m, hasMap
}

// isReviewer dice si el agente del reporte es el reviewer (con o sin prefijo).
func isReviewer(agent string) bool {
	return strings.TrimPrefix(agent, flow.SubagentPrefix) == "reviewer"
}

// reviewIncomplete arma el rechazo de APPROVED cuando falta cobertura o formato
// en el review-map (R7, R8); nil si no falta nada.
func reviewIncomplete(cov review.Coverage, m review.Map, hasMap bool) *flow.Rejection {
	if !hasMap {
		return nil
	}
	var blocks []string
	if cov.Measured && len(cov.RedMissing) > 0 {
		blocks = append(blocks, "El review-map marca como 🔴 archivos del diff que no abriste:\n- "+strings.Join(cov.RedMissing, "\n- ")+
			"\nÁbrelos (Read o `git diff -- <ruta>`) antes de aprobar.")
	}
	if len(m.RedNoPath) > 0 {
		blocks = append(blocks, "Estas viñetas 🔴 no empiezan con la ruta del archivo en backticks (`ruta`: razón):\n- "+strings.Join(m.RedNoPath, "\n- "))
	}
	if len(m.QuestionTells) > 0 {
		blocks = append(blocks, "Estas opciones de `## Preguntas de producto` delatan cuál hace el código; reescríbelas neutras, sin marcas ni menciones al código:\n- "+strings.Join(m.QuestionTells, "\n- "))
	}
	if !m.HasDocs {
		blocks = append(blocks, "El review-map no tiene la sección `## Docs`: lista ahí, una por viñeta con la ruta en backticks, la documentación que este cambio debe tocar.")
	}
	if len(blocks) == 0 {
		return nil
	}
	return &flow.Rejection{Code: "review_incomplete",
		Reason: "la revisión está incompleta:\n" + strings.Join(blocks, "\n") + "\nCorrígelo y reporta APPROVED otra vez."}
}

// docsPending arma el rechazo del documenter cuando una ruta de ## Docs ni
// cambió en la rama ni consta en reports/docs.md (R15); nil si no aplica.
func (e *Engine) docsPending(ctx context.Context, id string) *flow.Rejection {
	m, ok := e.reviewMap(id)
	base := e.diffBase()
	if !ok || len(m.Docs) == 0 || e.Git == nil {
		return nil
	}
	diff, err := e.Git.DiffNames(ctx, base)
	if err != nil {
		return nil
	}
	for i := range diff {
		diff[i] = strings.ReplaceAll(diff[i], `\`, "/")
	}
	var report string
	if b, err := e.Store.ReadFile(id, docsReport); err == nil {
		report = string(b)
	}
	pending := review.DocsPending(m.Docs, diff, report)
	if len(pending) == 0 {
		return nil
	}
	return &flow.Rejection{Code: "docs_pending", Reason: "el review-map lista en `## Docs` rutas que no cambiaron en la rama ni constan en " + docsReport + ":\n- " +
		strings.Join(pending, "\n- ") + "\nActualízalas, o agrega a " + docsReport + " una línea por cada una: - `ruta`: sin cambio: <motivo>. Después reporta DONE otra vez."}
}

// coverageEntry es el evento review_coverage de una corrida del reviewer (R12).
func (e *Engine) coverageEntry(id string, cov review.Coverage, agent string, round int) store.Entry {
	data := map[string]any{}
	if b, err := json.Marshal(cov); err == nil {
		_ = json.Unmarshal(b, &data)
	}
	data["round"] = round
	return store.Entry{TS: e.now(), ID: id, Event: "review_coverage", By: e.User, Agent: strings.TrimPrefix(agent, flow.SubagentPrefix), Data: data}
}

// lastCoverage es la cobertura del último review_coverage de la tarea.
func (e *Engine) lastCoverage(id string) (review.Coverage, bool) {
	log, err := e.Store.Log(id)
	if err != nil {
		return review.Coverage{}, false
	}
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Event != "review_coverage" {
			continue
		}
		var c review.Coverage
		if b, err := json.Marshal(log[i].Data); err == nil && json.Unmarshal(b, &c) == nil {
			return c, true
		}
	}
	return review.Coverage{}, false
}
