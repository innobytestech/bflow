package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/vcs"
)

func TestFrozenHashes(t *testing.T) {
	v := newEnv(t, "")
	root := v.e.Cfg.Root
	os.MkdirAll(filepath.Join(root, "internal"), 0o755)
	write := func(rel, s string) { os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(s), 0o644) }
	write("internal/a_test.go", "package a\n")

	if err := v.e.writeFrozen("T-1", []string{"internal/a_test.go", "internal/borrada_test.go"}); err != nil {
		t.Fatal(err)
	}
	if got := v.e.Frozen("T-1"); !slices.Equal(got, []string{"internal/a_test.go", "internal/borrada_test.go"}) {
		t.Errorf("Frozen: %v", got)
	}
	if c := v.e.FrozenChanged("T-1"); len(c) != 0 {
		t.Errorf("sin cambios: %v", c)
	}
	write("internal/a_test.go", "package a\r\n") // solo finales de línea
	if c := v.e.FrozenChanged("T-1"); len(c) != 0 {
		t.Errorf("CRLF no es un cambio: %v", c)
	}
	write("internal/a_test.go", "package b\n")
	write("internal/borrada_test.go", "package a\n") // no existía al congelar
	if c := v.e.FrozenChanged("T-1"); !slices.Equal(c, []string{"internal/a_test.go", "internal/borrada_test.go"}) {
		t.Errorf("cambios: %v", c)
	}

	// Una persona deja cambiar una prueba una vez: guard la suelta, el cambio no
	// cuenta y se vuelve a congelar con su contenido nuevo.
	if _, err := v.e.Store.Update("T-1", func(r *store.Record, _ bool) error { r.Flow = flow.State{ID: "T-1"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := v.e.AllowFrozen("T-1", "internal/zz_test.go"); err == nil {
		t.Error("solo se permite una prueba congelada")
	}
	_ = v.e.writeFrozen("T-1", []string{"internal/a_test.go"})
	if err := v.e.AllowFrozen("T-1", `internal\a_test.go`); err != nil {
		t.Fatal(err)
	}
	if got := v.e.Frozen("T-1"); len(got) != 0 {
		t.Errorf("la permitida no está congelada para guard: %v", got)
	}
	write("internal/a_test.go", "package c\n")
	if c := v.e.FrozenChanged("T-1"); len(c) != 0 {
		t.Errorf("el cambio permitido no cuenta: %v", c)
	}
	v.e.refreezeAllowed("T-1")
	if got := v.e.Frozen("T-1"); !slices.Equal(got, []string{"internal/a_test.go"}) {
		t.Errorf("vuelve a congelarse: %v", got)
	}
	write("internal/a_test.go", "package d\n")
	if c := v.e.FrozenChanged("T-1"); !slices.Equal(c, []string{"internal/a_test.go"}) {
		t.Errorf("un segundo cambio ya no está permitido: %v", c)
	}

	// Formato anterior: solo rutas. Se siguen leyendo; sin hash no hay con qué comparar.
	v.e.Store.WriteFile("T-2", FrozenFile, []byte(`["internal/a_test.go"]`))
	if got := v.e.Frozen("T-2"); !slices.Equal(got, []string{"internal/a_test.go"}) {
		t.Errorf("formato anterior: %v", got)
	}
	if c := v.e.FrozenChanged("T-2"); len(c) != 0 {
		t.Errorf("formato anterior no marca cambios: %v", c)
	}
}

// toAllowDecision lleva una tarea a implementing con una decisión aprobada que
// pide freeze --allow de a_test.go, congelada.
func toAllowDecision(t *testing.T, v *env, id, option string) {
	t.Helper()
	ctx := context.Background()
	m := mustT(t)
	toDecision(t, v, id)
	root := v.e.Cfg.Root
	_ = os.MkdirAll(filepath.Join(root, "internal"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "internal", "a_test.go"), []byte("package a\n"), 0o644)
	_ = v.e.writeFrozen(id, []string{"internal/a_test.go"})
	_, err := v.e.Store.Update(id, func(r *store.Record, _ bool) error {
		r.Flow.Gate.Options = []string{option, "B"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m(v.e.Approve(ctx, id, ApproveOpts{Choice: 1}))
}

func TestDecisionHoldsUntilFreezeAllow(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	toAllowDecision(t, v, id, "Aprobar `bflow freeze --allow internal/a_test.go`")

	rec, _ := v.e.Store.Load(id)
	n := v.e.withDisplay(ctx, rec, flow.NextFor(v.e.flowCfg(), rec.Flow))
	if n.Action != "ask" || n.Gate != "freeze_allow" || len(n.Options) != 2 {
		t.Fatalf("debe retener: %+v", n)
	}
	st, err := v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Next.Gate != "freeze_allow" {
		t.Errorf("status: %+v", st.Next)
	}
	if err := v.e.AllowFrozen(id, "internal/a_test.go"); err != nil {
		t.Fatal(err)
	}
	st, err = v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Next.Action != "spawn" || st.Next.Agents[0].Agent != "implementer" {
		t.Errorf("tras freeze --allow se relanza: %+v", st.Next)
	}
}

func TestDecisionAllowUnknownFileNoHold(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	toAllowDecision(t, v, id, "Aprobar `bflow freeze --allow internal/otra_test.go`")
	st, err := v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Next.Action != "spawn" {
		t.Errorf("archivo no congelado: %+v", st.Next)
	}
	id2 := v.task(t, "Demo 2")
	toAllowDecision(t, v, id2, "Aprobar sin mencionar el comando")
	st, err = v.e.Status(ctx, id2)
	if err != nil {
		t.Fatal(err)
	}
	if st.Next.Action != "spawn" {
		t.Errorf("sin comando: %+v", st.Next)
	}
}

// ---- GH-86: copias de las pruebas congeladas y restauración ----

const (
	fa = "internal/a_test.go"
	fb = "internal/b_test.go"
)

// frozenEnv deja una tarea T-1 con las pruebas dadas (ruta → contenido) congeladas;
// una ruta con contenido nil no existe al congelar.
func frozenEnv(t *testing.T, files map[string][]byte) *env {
	t.Helper()
	v := newEnv(t, "")
	if _, err := v.e.Store.Update("T-1", func(r *store.Record, _ bool) error {
		r.Flow = flow.State{ID: "T-1", Phase: flow.Implementing}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for f, b := range files {
		names = append(names, f)
		if b != nil {
			put(t, v, f, b)
		}
	}
	if err := v.e.writeFrozen("T-1", names); err != nil {
		t.Fatal(err)
	}
	return v
}

func put(t *testing.T, v *env, rel string, b []byte) {
	t.Helper()
	p := filepath.Join(v.e.Cfg.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func disk(t *testing.T, v *env, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(v.e.Cfg.Root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	return b
}

// snapshots son los nombres de archivo en frozen-tests/ de la tarea.
func snapshots(t *testing.T, v *env) []string {
	t.Helper()
	p, err := v.e.Store.Path("T-1", FrozenSnapshots)
	if err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(p)
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

func snapshotBytes(t *testing.T, v *env, hash string) []byte {
	t.Helper()
	b, err := v.e.Store.ReadFile("T-1", FrozenSnapshots+"/"+hash)
	if err != nil {
		t.Fatalf("falta la copia %s: %v", hash, err)
	}
	return b
}

// R12: al congelar se guardan los bytes de cada prueba que existe.
func TestFrozenSnapshotSaved(t *testing.T) {
	a := []byte("package a\r\nfunc TestA() {}\r\n")
	v := frozenEnv(t, map[string][]byte{fa: a, fb: []byte("package b\n"), "internal/nueva_test.go": nil})
	ha, hb := v.e.testHash(fa), v.e.testHash(fb)
	want := []string{ha, hb}
	slices.Sort(want)
	if got := snapshots(t, v); !slices.Equal(got, want) {
		t.Fatalf("una copia por prueba existente (la ausente no tiene): %v, quería %v", got, want)
	}
	if !bytes.Equal(snapshotBytes(t, v, ha), a) {
		t.Error("la copia lleva los bytes tal cual, CRLF incluido")
	}
}

// R12: re-congelar actualiza las copias y poda las que ya nadie referencia.
func TestFrozenSnapshotRefreeze(t *testing.T) {
	v := frozenEnv(t, map[string][]byte{fa: []byte("uno\n"), fb: []byte("b\n")})
	h1, hb := v.e.testHash(fa), v.e.testHash(fb)

	put(t, v, fa, []byte("dos\n"))
	if _, err := v.e.Refreeze("T-1"); err != nil {
		t.Fatal(err)
	}
	h2 := v.e.testHash(fa)
	want := []string{h2, hb}
	slices.Sort(want)
	if got := snapshots(t, v); !slices.Equal(got, want) {
		t.Fatalf("Refreeze guarda la copia nueva y poda la vieja (%s): %v, quería %v", h1, got, want)
	}

	// freeze --allow suelta la prueba: su copia sobra, la de las demás se queda.
	if err := v.e.AllowFrozen("T-1", fa); err != nil {
		t.Fatal(err)
	}
	if got := snapshots(t, v); !slices.Equal(got, []string{hb}) {
		t.Fatalf("AllowFrozen no crea copias y conserva las demás: %v", got)
	}
	put(t, v, fa, []byte("tres\n"))
	v.e.refreezeAllowed("T-1")
	h3 := v.e.testHash(fa)
	want = []string{h3, hb}
	slices.Sort(want)
	if got := snapshots(t, v); !slices.Equal(got, want) {
		t.Fatalf("al DONE se vuelve a congelar con su copia nueva: %v, quería %v", got, want)
	}
	if !bytes.Equal(snapshotBytes(t, v, h3), []byte("tres\n")) {
		t.Error("la copia es el contenido de ahora")
	}
}

// R13, R15, R17: restaura con los bytes guardados y deja el evento.
func TestRestoreFrozen(t *testing.T) {
	orig := []byte("package a\r\nfunc TestA() {}\r\n")
	v := frozenEnv(t, map[string][]byte{fa: orig, fb: []byte("b\n")})
	if r := v.e.RestoreFrozen("T-1"); !r.Empty() {
		t.Fatalf("sin cambios no hay nada que restaurar: %+v", r)
	}
	put(t, v, fa, []byte("package a\nfunc TestA() { t.Skip() }\n"))
	r := v.e.RestoreFrozen("T-1")
	if !slices.Equal(r.Restored, []string{fa}) || len(r.Deleted) != 0 || len(r.Missing) != 0 {
		t.Fatalf("restaura la que cambió: %+v", r)
	}
	if got := disk(t, v, fa); !bytes.Equal(got, orig) {
		t.Errorf("los bytes vuelven tal cual (CRLF): %q", got)
	}
	if c := v.e.FrozenChanged("T-1"); len(c) != 0 {
		t.Errorf("tras restaurar no hay cambios: %v", c)
	}
	log, err := v.e.Store.Log("T-1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, en := range log {
		if en.Event == "frozen_restore" {
			found = true
			if b, _ := json.Marshal(en.Data); !strings.Contains(string(b), fa) {
				t.Errorf("el evento lleva los archivos: %s", b)
			}
		}
	}
	if !found {
		t.Error("falta el evento frozen_restore")
	}
	if r := v.e.RestoreFrozen("T-1"); !r.Empty() {
		t.Errorf("segunda vez: nada que hacer: %+v", r)
	}
}

// R13: una prueba que no existía al congelar y apareció se borra.
func TestRestoreFrozenAbsent(t *testing.T) {
	v := frozenEnv(t, map[string][]byte{fa: []byte("a\n"), "internal/nueva_test.go": nil})
	put(t, v, "internal/nueva_test.go", []byte("package x\n"))
	r := v.e.RestoreFrozen("T-1")
	if !slices.Equal(r.Deleted, []string{"internal/nueva_test.go"}) || len(r.Restored) != 0 {
		t.Fatalf("se borra: %+v", r)
	}
	if disk(t, v, "internal/nueva_test.go") != nil {
		t.Error("el archivo sigue ahí")
	}
}

// R13: una prueba con allow o sin hash (formato anterior) no se toca.
func TestRestoreFrozenAllowSkipped(t *testing.T) {
	v := frozenEnv(t, map[string][]byte{fa: []byte("a\n"), fb: []byte("b\n")})
	if err := v.e.AllowFrozen("T-1", fa); err != nil {
		t.Fatal(err)
	}
	put(t, v, fa, []byte("cambio permitido\n"))
	if r := v.e.RestoreFrozen("T-1"); !r.Empty() {
		t.Fatalf("la permitida no se restaura: %+v", r)
	}
	if string(disk(t, v, fa)) != "cambio permitido\n" {
		t.Error("el cambio permitido se perdió")
	}
	if err := v.e.Store.WriteFile("T-1", FrozenFile, []byte(`["`+fb+`"]`)); err != nil {
		t.Fatal(err)
	}
	put(t, v, fb, []byte("otro\n"))
	if r := v.e.RestoreFrozen("T-1"); !r.Empty() {
		t.Errorf("sin hash no hay con qué comparar: %+v", r)
	}
}

// R14: una tarea congelada antes de este cambio no tiene copias: avisa, no escribe.
func TestRestoreFrozenNoSnapshot(t *testing.T) {
	v := frozenEnv(t, map[string][]byte{fa: []byte("a\n")})
	p, _ := v.e.Store.Path("T-1", FrozenSnapshots)
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	put(t, v, fa, []byte("cambiada\n"))
	r := v.e.RestoreFrozen("T-1")
	if !slices.Equal(r.Missing, []string{fa}) || len(r.Restored) != 0 || len(r.Deleted) != 0 {
		t.Fatalf("sin copia: %+v", r)
	}
	if string(disk(t, v, fa)) != "cambiada\n" {
		t.Error("sin copia no se escribe nada")
	}
}

// R14: una copia cuyo hash no es el guardado no se usa.
func TestRestoreFrozenBadSnapshot(t *testing.T) {
	v := frozenEnv(t, map[string][]byte{fa: []byte("a\n")})
	h := v.e.testHash(fa)
	if err := v.e.Store.WriteFile("T-1", FrozenSnapshots+"/"+h, []byte("contenido alterado\n")); err != nil {
		t.Fatal(err)
	}
	put(t, v, fa, []byte("cambiada\n"))
	r := v.e.RestoreFrozen("T-1")
	if !slices.Equal(r.Missing, []string{fa}) || len(r.Restored) != 0 {
		t.Fatalf("copia que no valida: %+v", r)
	}
	if string(disk(t, v, fa)) != "cambiada\n" {
		t.Error("una copia alterada no se escribe")
	}
}

// Seguridad: un hash guardado que no es de 64 caracteres hex no arma rutas (../).
func TestRestoreFrozenBadHashName(t *testing.T) {
	v := frozenEnv(t, map[string][]byte{fa: []byte("a\n")})
	taskDir, _ := v.e.Store.Path("T-1", ".")
	outside := filepath.Join(taskDir, "..", "secreto")
	if err := os.WriteFile(outside, []byte("no me toques\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../secreto", "../../x", "abc", strings.Repeat("g", 64), strings.Repeat("A", 64)} {
		m, _ := json.Marshal(map[string]string{fa: bad})
		if err := v.e.Store.WriteFile("T-1", FrozenFile, m); err != nil {
			t.Fatal(err)
		}
		put(t, v, fa, []byte("cambiada\n"))
		r := v.e.RestoreFrozen("T-1")
		if len(r.Restored) != 0 || len(r.Deleted) != 0 {
			t.Errorf("%q: no restaura ni borra: %+v", bad, r)
		}
		if string(disk(t, v, fa)) != "cambiada\n" {
			t.Errorf("%q: la prueba no se toca", bad)
		}
		if b, _ := os.ReadFile(outside); string(b) != "no me toques\n" {
			t.Errorf("%q: se escribió fuera de la carpeta", bad)
		}
	}
}

// Un DONE rechazado después del check no gasta el freeze --allow; el aceptado sí.
func TestRejectedDoneKeepsFreezeAllow(t *testing.T) {
	v, id := narrativeImpl(t, "", vcs.AddedLine{Path: "a.go", Line: 5, Text: "// T3: según R4"})
	put(t, v, fa, []byte("uno\n"))
	if err := v.e.writeFrozen(id, []string{fa}); err != nil {
		t.Fatal(err)
	}
	if err := v.e.AllowFrozen(id, fa); err != nil {
		t.Fatal(err)
	}
	put(t, v, fa, []byte("dos\n"))
	rejection(t, implDone(v, id), "narrative_comment")
	if got := v.e.Frozen(id); len(got) != 0 {
		t.Fatalf("la prueba debe seguir permitida tras un DONE rechazado: %v", got)
	}
	v.e.Git = &fakeGit{branch: "dev"}
	if err := implDone(v, id); err != nil {
		t.Fatal(err)
	}
	if got := v.e.Frozen(id); !slices.Equal(got, []string{fa}) {
		t.Errorf("el DONE aceptado vuelve a congelar: %v", got)
	}
}
