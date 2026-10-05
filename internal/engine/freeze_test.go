package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
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
	os.MkdirAll(filepath.Join(root, "internal"), 0o755)
	os.WriteFile(filepath.Join(root, "internal", "a_test.go"), []byte("package a\n"), 0o644)
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
