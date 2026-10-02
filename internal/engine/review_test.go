package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/review"
	"innobytes.tech/bflow/internal/store"
)

const mapRed = "# Review-map\n\n## 🔴 Mirar primero\n- `internal/a.go`: lógica nueva\n- `internal/b.go`: seguridad\n- `gone/x.go`: ya no está\n" +
	"## 🟡 Revisar\n- `internal/c.go`\n## Docs\n- `README.md`: tabla de comandos\n"

var diff4 = []string{"internal/a.go", "internal/b.go", "internal/c.go", "README.md"}

// inQuality deja una tarea hotfix en quality con el diff dado, el review-map y
// un git falso. Las lecturas que se agreguen después caen dentro de la ventana.
func inQuality(t *testing.T, reviewMap string, diff []string, lines int) (*env, string, *fakeGit) {
	t.Helper()
	v := newEnv(t, "")
	g := &fakeGit{diff: diff, lines: lines}
	v.e.Git = g
	ctx := context.Background()
	id := v.task(t, "Demo")
	mustT(t)(v.e.Start(ctx, id, flow.Hotfix, "", ""))
	v.tick(time.Minute)
	mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV}))
	if phaseOf(t, v, id) != flow.Quality {
		t.Fatalf("debía quedar en quality")
	}
	if reviewMap != "" {
		if err := v.e.Store.WriteFile(id, "reports/review-map.md", []byte(reviewMap)); err != nil {
			t.Fatal(err)
		}
	}
	v.tick(time.Minute)
	return v, id, g
}

func addReads(t *testing.T, v *env, id string, rs ...review.Read) {
	t.Helper()
	var lines [][]byte
	for _, r := range rs {
		if r.TS.IsZero() {
			r.TS = v.now
		}
		b, _ := json.Marshal(r)
		lines = append(lines, b)
	}
	if err := v.e.Store.AppendFile(id, review.ReadsFile, lines); err != nil {
		t.Fatal(err)
	}
}

func readOf(paths ...string) review.Read {
	return review.Read{Tool: guard.Read, Paths: paths, ReadHook: true}
}

func approve(v *env, id string) (Outcome, error) {
	return v.e.Report(context.Background(), id, ReportOpts{Agent: "reviewer", Verdict: flow.Approved})
}

func rejection(t *testing.T, err error, code string) *flow.Rejection {
	t.Helper()
	var rj *flow.Rejection
	if !errors.As(err, &rj) || rj.Code != code {
		t.Fatalf("esperaba %s, hubo: %v", code, err)
	}
	return rj
}

func coverageEvents(t *testing.T, v *env, id string) []store.Entry {
	t.Helper()
	log, err := v.e.Store.Log(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Entry
	for _, en := range log {
		if en.Event == "review_coverage" {
			out = append(out, en)
		}
	}
	return out
}

// R7: APPROVED con una 🔴 del diff sin leer se rechaza y lista solo lo que falta.
func TestReviewerApprovedNeedsRedRead(t *testing.T) {
	v, id, _ := inQuality(t, mapRed, diff4, 3000)
	addReads(t, v, id, readOf("internal/a.go"))
	_, err := approve(v, id)
	rj := rejection(t, err, "review_incomplete")
	if !strings.Contains(rj.Reason, "internal/b.go") || strings.Contains(rj.Reason, "internal/a.go") || strings.Contains(rj.Reason, "gone/x.go") ||
		!strings.Contains(rj.Reason, "reporta APPROVED otra vez") {
		t.Errorf("lista b.go (falta), no a.go (leída) ni gone/x.go (fuera del diff):\n%s", rj.Reason)
	}
	if phaseOf(t, v, id) != flow.Quality {
		t.Error("no avanza")
	}
	if n := len(coverageEvents(t, v, id)); n != 0 {
		t.Errorf("un reporte rechazado no registra cobertura: %d", n)
	}

	// Con el diff sobre el tope una carpeta no cubre; la ruta exacta sí.
	addReads(t, v, id, review.Read{Tool: guard.Bash, Paths: []string{"internal"}, ReadHook: true})
	if _, err := approve(v, id); err == nil {
		t.Fatal("una carpeta no cubre con el diff sobre el tope")
	}
	addReads(t, v, id, readOf("internal/b.go", "internal/c.go"))
	o, err := approve(v, id)
	if err != nil || o.To != flow.Documenting {
		t.Fatalf("con b.go leído pasa: %+v %v", o, err)
	}
}

// R6: con menos de 1,500 líneas un git diff entero cubre todo; con más, no.
func TestReviewerApprovedWholeSmallDiff(t *testing.T) {
	whole := review.Read{Tool: guard.Bash, Whole: true, ReadHook: true}
	v, id, _ := inQuality(t, mapRed, diff4, review.WholeDiffMax-1)
	addReads(t, v, id, whole)
	if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
		t.Fatalf("diff chico leído entero: %+v %v", o, err)
	}

	v, id, _ = inQuality(t, mapRed, diff4, review.WholeDiffMax)
	addReads(t, v, id, whole)
	rj := rejection(t, func() error { _, err := approve(v, id); return err }(), "review_incomplete")
	if !strings.Contains(rj.Reason, "internal/a.go") || !strings.Contains(rj.Reason, "internal/b.go") {
		t.Errorf("con %d líneas el diff entero no cubre las 🔴:\n%s", review.WholeDiffMax, rj.Reason)
	}
}

// R9: REJECTED no se bloquea ni por cobertura ni por formato, y registra la cobertura.
func TestReviewerRejectedNotBlocked(t *testing.T) {
	bad := "## 🔴 Primero\n- una viñeta sin ruta\n- `internal/b.go`: falta leerla\n"
	v, id, _ := inQuality(t, bad, diff4, 3000)
	addReads(t, v, id, readOf("internal/a.go"))
	o, err := v.e.Report(context.Background(), id, ReportOpts{Agent: "reviewer", Verdict: flow.RejectedV, Note: "falta cubrir el caso vacío"})
	if err != nil || o.To != flow.Implementing {
		t.Fatalf("REJECTED pasa: %+v %v", o, err)
	}
	if evs := coverageEvents(t, v, id); len(evs) != 1 {
		t.Errorf("también se registra: %d", len(evs))
	}
}

// R10: sin lecturas medidas no se exige cobertura; sin review-map tampoco hay qué exigir.
func TestReviewerUnmeasuredNotBlocked(t *testing.T) {
	t.Run("sin lecturas", func(t *testing.T) {
		v, id, _ := inQuality(t, mapRed, diff4, 3000)
		if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
			t.Fatalf("%+v %v", o, err)
		}
		evs := coverageEvents(t, v, id)
		if len(evs) != 1 || evs[0].Data["measured"] != false {
			t.Errorf("queda registrada como no medida: %+v", evs)
		}
	})
	t.Run("lecturas que no vienen del hook", func(t *testing.T) {
		v, id, _ := inQuality(t, mapRed, diff4, 3000)
		addReads(t, v, id, review.Read{Tool: guard.Bash, Paths: []string{"internal/c.go"}, ReadHook: false})
		if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
			t.Fatalf("%+v %v", o, err)
		}
	})
	t.Run("sin review-map", func(t *testing.T) {
		v, id, _ := inQuality(t, "", diff4, 3000)
		addReads(t, v, id, readOf("internal/c.go"))
		if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
			t.Fatalf("%+v %v", o, err)
		}
	})
	t.Run("lecturas de una ronda anterior", func(t *testing.T) {
		v, id, _ := inQuality(t, mapRed, diff4, 3000)
		// Lecturas anteriores a la entrada a quality no cuentan: la ventana las deja fuera.
		addReads(t, v, id, review.Read{TS: v.now.Add(-time.Hour), Tool: guard.Read, Paths: []string{"internal/a.go", "internal/b.go"}, ReadHook: true})
		if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
			t.Fatalf("sin lecturas en la ventana queda no medida: %+v %v", o, err)
		}
		evs := coverageEvents(t, v, id)
		if len(evs) != 1 || evs[0].Data["measured"] != false {
			t.Errorf("no medida: %+v", evs)
		}
	})
}

// R8: el formato del review-map se exige aunque la cobertura no esté medida.
func TestReviewerNeedsRedPathsAndDocs(t *testing.T) {
	noPath := "## 🔴 Primero\n- Revisar la parte de seguridad del cambio\n- `internal/a.go`: ok\n## Docs\n- `README.md`\n"
	v, id, _ := inQuality(t, noPath, diff4, 3000)
	rj := rejection(t, func() error { _, err := approve(v, id); return err }(), "review_incomplete")
	if !strings.Contains(rj.Reason, "Revisar la parte de seguridad") || !strings.Contains(rj.Reason, "ruta") {
		t.Errorf("dice qué viñeta no empieza con ruta en backticks:\n%s", rj.Reason)
	}
	fixed := "## 🔴 Primero\n- `internal/a.go`: seguridad\n## Docs\n- `README.md`\n"
	if err := v.e.Store.WriteFile(id, "reports/review-map.md", []byte(fixed)); err != nil {
		t.Fatal(err)
	}
	if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
		t.Fatalf("corregido pasa: %+v %v", o, err)
	}

	noDocs := "## 🔴 Primero\n- `internal/a.go`: seguridad\n## 🟡 Luego\n- `internal/c.go`\n"
	v, id, _ = inQuality(t, noDocs, diff4, 3000)
	rj = rejection(t, func() error { _, err := approve(v, id); return err }(), "review_incomplete")
	if !strings.Contains(rj.Reason, "## Docs") || !strings.Contains(rj.Reason, "reporta APPROVED otra vez") {
		t.Errorf("pide la sección ## Docs:\n%s", rj.Reason)
	}
	// Los dos problemas juntos se dicen juntos.
	both := "## 🔴 Primero\n- sin ruta\n"
	v, id, _ = inQuality(t, both, diff4, 3000)
	rj = rejection(t, func() error { _, err := approve(v, id); return err }(), "review_incomplete")
	if !strings.Contains(rj.Reason, "sin ruta") || !strings.Contains(rj.Reason, "## Docs") {
		t.Errorf("un bloque por problema:\n%s", rj.Reason)
	}
}

// R12: al aceptar el reporte queda review_coverage en el log.
func TestReviewCoverageLogged(t *testing.T) {
	v, id, _ := inQuality(t, mapRed, diff4, 3000)
	addReads(t, v, id, readOf("internal/a.go", "internal/b.go", "README.md"))
	if _, err := v.e.Report(context.Background(), id, ReportOpts{Agent: "reviewer", Verdict: flow.RejectedV, Note: "falta c.go"}); err != nil {
		t.Fatal(err)
	}
	evs := coverageEvents(t, v, id)
	if len(evs) != 1 {
		t.Fatalf("un evento: %+v", evs)
	}
	b, _ := json.Marshal(evs[0].Data)
	var c review.Coverage
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if !c.Measured || c.Total != 4 || c.Read != 3 || !slices.Equal(c.Unread, []string{"internal/c.go"}) || !slices.Equal(c.RedOutside, []string{"gone/x.go"}) {
		t.Errorf("cobertura: %+v", c)
	}
	if evs[0].Data["round"] != float64(0) || evs[0].Agent != "reviewer" || evs[0].By == "" {
		t.Errorf("ronda, agente y autor: %+v", evs[0])
	}
}

// qualityToWalkthrough lleva la tarea de quality hasta el gate del recorrido.
func qualityToWalkthrough(t *testing.T, v *env, id string) Outcome {
	t.Helper()
	ctx := context.Background()
	if _, err := approve(v, id); err != nil {
		t.Fatal(err)
	}
	o := mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "documenter", Verdict: flow.DoneV}))
	if o.Next.Gate == "questions" {
		o = mustT(t)(v.e.Approve(ctx, id, ApproveOpts{Note: "sí"}))
	}
	if o.Next.Gate != "walkthrough" {
		t.Fatalf("esperaba el gate walkthrough: %+v", o.Next)
	}
	return o
}

// R13: el display del walkthrough empieza con la cobertura del reviewer.
func TestWalkthroughShowsCoverage(t *testing.T) {
	t.Run("medida", func(t *testing.T) {
		v, id, _ := inQuality(t, mapRed, diff4, 3000)
		addReads(t, v, id, readOf("internal/a.go", "internal/b.go", "internal/c.go", "README.md"))
		o := qualityToWalkthrough(t, v, id)
		d := o.Next.Display
		if !strings.HasPrefix(d, "**Cobertura del reviewer:**\nel reviewer leyó 4 de 4 archivos del diff") {
			t.Fatalf("empieza con la cobertura:\n%s", d)
		}
		head := d[:strings.Index(d, "Tus respuestas")]
		if !strings.Contains(head, "gone/x.go") || !strings.Contains(head, "fuera del diff") {
			t.Errorf("no leídos y 🔴 fuera del diff:\n%s", head)
		}
	})
	t.Run("sin medir", func(t *testing.T) {
		v, id, _ := inQuality(t, mapRed, diff4, 3000)
		o := qualityToWalkthrough(t, v, id)
		if !strings.HasPrefix(o.Next.Display, "**Cobertura del reviewer:**\ncobertura del reviewer: no medida") {
			t.Errorf("sin medir:\n%s", o.Next.Display)
		}
	})
}

// R15: el documenter no cierra con una ruta de ## Docs sin tocar ni justificar.
func TestDocumenterDocsPending(t *testing.T) {
	docsMap := "## 🔴 Primero\n- `internal/a.go`: lógica\n## Docs\n- `README.md`: tabla\n- `docs/guia.md`: gates\n"
	ctx := context.Background()
	toDocumenting := func(t *testing.T, m string) (*env, string) {
		v, id, _ := inQuality(t, m, []string{"internal/a.go", "README.md"}, 100)
		if _, err := approve(v, id); err != nil {
			t.Fatal(err)
		}
		return v, id
	}
	done := func(v *env, id string) error {
		_, err := v.e.Report(ctx, id, ReportOpts{Agent: "documenter", Verdict: flow.DoneV})
		return err
	}

	v, id := toDocumenting(t, docsMap)
	rj := rejection(t, done(v, id), "docs_pending")
	if !strings.Contains(rj.Reason, "docs/guia.md") || strings.Contains(rj.Reason, "README.md") || !strings.Contains(rj.Reason, "sin cambio:") {
		t.Errorf("lista solo docs/guia.md y el formato esperado:\n%s", rj.Reason)
	}
	if phaseOf(t, v, id) != flow.Documenting {
		t.Error("no avanza")
	}
	if err := v.e.Store.WriteFile(id, "reports/docs.md", []byte("- `docs/guia.md`: sin cambio:\n")); err != nil {
		t.Fatal(err)
	}
	_ = rejection(t, done(v, id), "docs_pending") // motivo vacío no vale
	if err := v.e.Store.WriteFile(id, "reports/docs.md", []byte("- `docs/guia.md`: sin cambio: la guía no habla de gates nuevos\n")); err != nil {
		t.Fatal(err)
	}
	if err := done(v, id); err != nil {
		t.Fatalf("con el motivo pasa: %v", err)
	}

	// Sin review-map no aplica.
	v, id = toDocumenting(t, "")
	if err := done(v, id); err != nil {
		t.Fatalf("sin review-map no aplica: %v", err)
	}
}

// withTests deja que los *_test.go cuenten como pruebas en la cobertura.
func withTests(v *env) { v.e.Cfg.Guard.TestPatterns = []string{"*_test.go"} }

// R1, R3, R5: caso GH-46: las 🔴 leídas pero una prueba sin abrir se rechaza; lo 🔴 pendiente no se repite.
func TestReviewerApprovedNeedsAllCode(t *testing.T) {
	m := "## 🔴 Primero\n- `internal/b.go`: lógica\n## Docs\n- `README.md`\n"
	v, id, _ := inQuality(t, m, []string{"internal/a.go", "internal/b.go", "internal/b_test.go", "README.md"}, 300)
	withTests(v)
	addReads(t, v, id, readOf("internal/a.go"))
	_, err := approve(v, id)
	rj := rejection(t, err, "review_incomplete")
	if n := strings.Count(rj.Reason, "internal/b.go"); n != 1 {
		t.Errorf("b.go (🔴 y pendiente) aparece una vez, no %d:\n%s", n, rj.Reason)
	}
	if !strings.Contains(rj.Reason, "internal/b_test.go") || !strings.Contains(rj.Reason, "no abriste") {
		t.Errorf("la prueba sin abrir se lista:\n%s", rj.Reason)
	}
	if phaseOf(t, v, id) != flow.Quality {
		t.Error("no avanza")
	}

	// Con las 🔴 leídas queda la prueba, y solo ella.
	addReads(t, v, id, readOf("internal/b.go"))
	rj = rejection(t, func() error { _, err := approve(v, id); return err }(), "review_incomplete")
	if strings.Contains(rj.Reason, "El review-map marca") || !strings.Contains(rj.Reason, "internal/b_test.go") || strings.Contains(rj.Reason, "README.md") {
		t.Errorf("solo la prueba pendiente:\n%s", rj.Reason)
	}
	addReads(t, v, id, readOf("internal/b_test.go"))
	if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
		t.Fatalf("con todo abierto pasa: %+v %v", o, err)
	}
}

// R2: con 25 pendientes se listan 20 en orden, "y 5 más" y la sugerencia con la base.
func TestReviewerPendingListCapped(t *testing.T) {
	var diff []string
	for i := 1; i <= 25; i++ {
		diff = append(diff, fmt.Sprintf("pkg/f%02d.go", i))
	}
	diff = append(diff, "README.md")
	m := "## 🔴 Primero\n- `README.md`: ok\n## Docs\n- `README.md`\n"
	reject := func(v *env, id string) string {
		t.Helper()
		addReads(t, v, id, readOf("README.md"))
		_, err := approve(v, id)
		return rejection(t, err, "review_incomplete").Reason
	}

	v, id, _ := inQuality(t, m, diff, 3000)
	base := v.e.diffBase()
	if base == "" {
		t.Fatal("la config por defecto trae base")
	}
	r := reject(v, id)
	if !strings.Contains(r, "\n- pkg/f01.go\n") || !strings.Contains(r, "\n- pkg/f20.go\n") || strings.Contains(r, "pkg/f21.go") || !strings.Contains(r, "\n- y 5 más") ||
		strings.Index(r, "pkg/f01.go") > strings.Index(r, "pkg/f02.go") {
		t.Errorf("20 en orden del diff y luego 'y 5 más':\n%s", r)
	}
	if !strings.Contains(r, "`git diff "+base+"...HEAD -- <ruta>`") || strings.Contains(r, "basta un") {
		t.Errorf("sugiere git diff por ruta, sin el diff completo (3000 líneas):\n%s", r)
	}

	v, id, _ = inQuality(t, m, diff, 1499)
	if r := reject(v, id); !strings.Contains(r, "basta un `git diff "+v.e.diffBase()+"...HEAD` completo") {
		t.Errorf("con menos de 1,500 líneas sugiere el diff completo:\n%s", r)
	}

	v, id, _ = inQuality(t, m, diff, 3000)
	v.e.Cfg.VCS.BaseBranch = ""
	if r := reject(v, id); !strings.Contains(r, "`git diff <base>...HEAD -- <ruta>`") {
		t.Errorf("sin base el texto dice <base>:\n%s", r)
	}
}

// R4, R11: docs, specs/, lockfiles y binarios sin abrir no se exigen.
func TestReviewerExemptNotRequired(t *testing.T) {
	m := "## 🔴 Primero\n- `internal/a.go`: ok\n## Docs\n- `README.md`\n"
	diff := []string{"internal/a.go", "README.md", "docs/guia.md", "specs/GH-1-x/spec.md", "go.sum", "assets/logo.png", "assets/blob"}
	v, id, g := inQuality(t, m, diff, 3000)
	g.binary = []string{"assets/logo.png", "assets/blob"}
	addReads(t, v, id, readOf("internal/a.go"))
	if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
		t.Fatalf("lo exento no se exige: %+v %v", o, err)
	}
	// Sin la lista de binarios, el que no es exento sí queda pendiente.
	v, id, g = inQuality(t, m, diff, 3000)
	g.binary = nil
	addReads(t, v, id, readOf("internal/a.go"))
	rj := rejection(t, func() error { _, err := approve(v, id); return err }(), "review_incomplete")
	if !strings.Contains(rj.Reason, "assets/blob") || strings.Contains(rj.Reason, "README.md") || strings.Contains(rj.Reason, "go.sum") || strings.Contains(rj.Reason, "specs/") {
		t.Errorf("solo lo no exento:\n%s", rj.Reason)
	}
}

// R6: con un diff de 2 archivos y un git diff entero no se pide nada más.
func TestReviewerSmallDiffWholeCovers(t *testing.T) {
	m := "## 🔴 Primero\n- `internal/a.go`: ok\n## Docs\n- `README.md`\n"
	v, id, _ := inQuality(t, m, []string{"internal/a.go", "internal/a_test.go"}, 80)
	withTests(v)
	addReads(t, v, id, review.Read{Tool: guard.Bash, Whole: true, ReadHook: true})
	if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
		t.Fatalf("diff chico leído entero: %+v %v", o, err)
	}
	// Con 1500 líneas el diff entero no cubre; la 🔴 está fuera del diff, así que solo cuentan los pendientes.
	m = "## 🔴 Primero\n- `gone.go`: ya no está\n## Docs\n- `README.md`\n"
	v, id, _ = inQuality(t, m, []string{"internal/a.go", "internal/a_test.go"}, review.WholeDiffMax)
	withTests(v)
	addReads(t, v, id, review.Read{Tool: guard.Bash, Whole: true, ReadHook: true})
	_ = rejection(t, func() error { _, err := approve(v, id); return err }(), "review_incomplete")
}

// R9, R10: REJECTED no se bloquea por pendientes y guarda pending en review_coverage.
func TestReviewerPendingNotOnRejected(t *testing.T) {
	v, id, _ := inQuality(t, mapRed, diff4, 3000)
	addReads(t, v, id, readOf("internal/a.go", "internal/b.go"))
	o, err := v.e.Report(context.Background(), id, ReportOpts{Agent: "reviewer", Verdict: flow.RejectedV, Note: "falta el caso vacío"})
	if err != nil || o.To != flow.Implementing {
		t.Fatalf("REJECTED pasa con c.go sin abrir: %+v %v", o, err)
	}
	evs := coverageEvents(t, v, id)
	if len(evs) != 1 {
		t.Fatalf("un evento: %+v", evs)
	}
	if p, _ := evs[0].Data["pending"].([]any); len(p) != 1 || p[0] != "internal/c.go" {
		t.Errorf("pending guardado: %+v", evs[0].Data)
	}
	// Un evento viejo, sin pending, se lee igual.
	b, _ := json.Marshal(map[string]any{"measured": true, "total": 4, "read": 3})
	var c review.Coverage
	if err := json.Unmarshal(b, &c); err != nil || len(c.Pending) != 0 || c.Read != 3 {
		t.Errorf("evento viejo: %+v %v", c, err)
	}
}

// R8: sin cobertura medida no se rechaza por pendientes.
func TestReviewerPendingUnmeasured(t *testing.T) {
	t.Run("sin lecturas", func(t *testing.T) {
		v, id, _ := inQuality(t, mapRed, diff4, 3000)
		if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
			t.Fatalf("%+v %v", o, err)
		}
	})
	t.Run("lecturas que no vienen del hook", func(t *testing.T) {
		v, id, _ := inQuality(t, mapRed, diff4, 3000)
		addReads(t, v, id, review.Read{Tool: guard.Bash, Paths: []string{"internal/a.go"}})
		if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
			t.Fatalf("%+v %v", o, err)
		}
	})
	t.Run("sin review-map", func(t *testing.T) {
		v, id, _ := inQuality(t, "", diff4, 3000)
		addReads(t, v, id, readOf("internal/a.go"))
		if o, err := approve(v, id); err != nil || o.To != flow.Documenting {
			t.Fatalf("%+v %v", o, err)
		}
	})
}
