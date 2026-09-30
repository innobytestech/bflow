package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker"
	"innobytes.tech/bflow/internal/vcs"
)

// prSections son los artefactos de .bflow/tasks/<ID>/ que van a la
// descripción del PR, en orden. Así salen del repo y quedan donde se revisan.
var prSections = []struct{ title, file string }{
	{"Review-map", "reports/review-map.md"},
	{"Walkthrough", "walkthrough.md"},
	{"Decisiones en vuelo", "decisions.md"},
}

// PRBody arma la descripción del PR.
func (e *Engine) PRBody(rec store.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** · %s\n", rec.Flow.ID, rec.Title)
	if rec.URL != "" {
		fmt.Fprintf(&b, "\nTarea: %s\n", rec.URL)
	}
	if brief, err := e.specSection(rec, "brief"); err == nil && brief != "" {
		fmt.Fprintf(&b, "\n## Brief\n\n%s\n", brief)
	}
	for _, s := range prSections {
		body, err := e.Store.ReadFile(rec.Flow.ID, s.file)
		if err != nil || strings.TrimSpace(string(body)) == "" {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", s.title, strings.TrimSpace(demoteHeadings(string(body))))
	}
	if body := e.changelog(rec.Flow); body != "" {
		fmt.Fprintf(&b, "\n## Contrato para consumidores\n\n%s\n", demoteHeadings(body))
	}
	if l, ok := e.Tracker.(tracker.PRLinker); ok {
		if ref := l.CloseRef(rec.Flow.ID); ref != "" {
			fmt.Fprintf(&b, "\n%s\n", ref)
		}
	}
	b.WriteString("\n---\n")
	if slices.Contains(e.flowCfg().Lanes[rec.Flow.Lane], flow.Spec) {
		fmt.Fprintf(&b, "Spec: `%s` · ", flow.SpecPath(rec.Flow.ID, rec.Flow.Slug))
	}
	b.WriteString("generado por bflow\n")
	return b.String()
}

// changelog lee el changelog para consumidores de la tarea, versionado en el
// repo. Las tareas empezadas antes lo tienen en .bflow/tasks/<ID>/.
func (e *Engine) changelog(s flow.State) string {
	body, err := os.ReadFile(filepath.Join(e.Cfg.Root, filepath.FromSlash(e.flowCfg().ChangelogPath(s.ID, s.Slug))))
	if err != nil {
		body, _ = e.Store.ReadFile(s.ID, "consumer-changelog.md")
	}
	return strings.TrimSpace(string(body))
}

// demoteHeadings baja dos niveles los encabezados de un artefacto para que
// queden dentro de su sección del PR.
func demoteHeadings(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "#") {
			lines[i] = "##" + l
		}
	}
	return strings.Join(lines, "\n")
}

// openPR publica la rama y abre (o actualiza) el PR. Es idempotente: si ya
// existe un PR para la rama, solo actualiza su descripción.
func (e *Engine) openPR(ctx context.Context, rec *store.Record) ([]string, error) {
	if rec.Branch == "" {
		rec.Branch = e.BranchName(rec.Flow)
	}
	if e.Git == nil {
		return []string{"sin git: no se hizo push ni se abrió el PR"}, nil
	}
	warns := e.commitSpec(ctx, rec.Flow, "actualiza la spec") // tareas marcadas, decisiones en vuelo
	if files := e.uncommitted(ctx, rec.Flow); len(files) > 0 {
		return nil, uncommittedRejection(files, "aprueba otra vez para abrir el PR")
	}
	if err := e.Git.Push(ctx, rec.Branch); err != nil {
		return nil, fmt.Errorf("push de %s: %w", rec.Branch, err)
	}
	if e.Host == nil {
		return append(warns, "push hecho; sin host remoto configurado: abre el PR a mano (vcs.host)"), nil
	}
	base := e.Cfg.VCS.BaseBranch
	body := e.PRBody(*rec)
	if pr, err := e.Host.FindPR(ctx, rec.Branch); err == nil {
		if err := e.Host.UpdatePRBody(ctx, pr.Number, body); err != nil {
			return nil, fmt.Errorf("actualizar PR #%d: %w", pr.Number, err)
		}
		rec.PR = &store.PR{Number: pr.Number, URL: pr.URL}
		return append(warns, fmt.Sprintf("PR #%d ya existía; descripción actualizada: %s", pr.Number, pr.URL)), nil
	} else if !errors.Is(err, vcs.ErrNoPR) {
		if degradable(err) {
			return append(warns, e.degradePR(rec, base, err)...), nil
		}
		return nil, fmt.Errorf("buscar PR: %w", err)
	}
	pr, err := e.Host.OpenPR(ctx, vcs.PRSpec{Base: base, Head: rec.Branch, Title: fmt.Sprintf("%s · %s", rec.Flow.ID, rec.Title), Body: body})
	if degradable(err) {
		return append(warns, e.degradePR(rec, base, err)...), nil
	}
	if err != nil {
		return nil, fmt.Errorf("abrir PR: %w", err)
	}
	rec.PR = &store.PR{Number: pr.Number, URL: pr.URL}
	return append(warns, fmt.Sprintf("PR #%d abierto: %s", pr.Number, pr.URL)), nil
}

// degradable: sin token o con un token que no ve el repo, el PR se deja listo
// para abrirlo a mano en lugar de fallar.
func degradable(err error) bool {
	return errors.Is(err, vcs.ErrNoCredentials) || errors.Is(err, vcs.ErrNoAccess)
}

func (e *Engine) degradePR(rec *store.Record, base string, cause error) []string {
	url := e.Host.CompareURL(base, rec.Branch)
	rec.PR = &store.PR{URL: url}
	if err := e.Store.WriteFile(rec.Flow.ID, "pr-body.md", []byte(e.PRBody(*rec))); err != nil {
		return []string{"no se pudo guardar pr-body.md: " + err.Error()}
	}
	return []string{fmt.Sprintf("%v: crea el PR aquí: %s (descripción lista en .bflow/tasks/%s/pr-body.md; bflow connect github para abrirlo solo)", cause, url, rec.Flow.ID)}
}

// uncommitted son los cambios de código sin commitear: no entrarían al PR.
// No cuentan .bflow/ (git la ignora) ni, en los carriles con spec, la spec y
// el changelog: bflow los commitea solo.
func (e *Engine) uncommitted(ctx context.Context, s flow.State) []string {
	if e.Git == nil {
		return nil
	}
	dirty, err := e.Git.Dirty(ctx, e.Cfg.Check.CodePaths)
	if err != nil {
		return nil
	}
	var own []string
	if slices.Contains(e.flowCfg().Lanes[s.Lane], flow.Spec) {
		own = e.specFiles(s)
	}
	var out []string
	for _, f := range dirty {
		f = strings.ReplaceAll(f, `\`, "/")
		if !slices.Contains(own, f) && !strings.HasPrefix(f, ".bflow/") {
			out = append(out, f)
		}
	}
	return out
}

func uncommittedRejection(files []string, when string) *flow.Rejection {
	return &flow.Rejection{Code: "uncommitted", Reason: "hay cambios de código sin commitear que no entrarían al PR:\n" + strings.Join(files, "\n") +
		"\nCommitéalos (o descarta los que no van) y " + when + "."}
}
