package engine

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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

	// Formato anterior: solo rutas. Se siguen leyendo; sin hash no hay con qué comparar.
	v.e.Store.WriteFile("T-2", FrozenFile, []byte(`["internal/a_test.go"]`))
	if got := v.e.Frozen("T-2"); !slices.Equal(got, []string{"internal/a_test.go"}) {
		t.Errorf("formato anterior: %v", got)
	}
	if c := v.e.FrozenChanged("T-2"); len(c) != 0 {
		t.Errorf("formato anterior no marca cambios: %v", c)
	}
}
