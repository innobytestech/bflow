package archtest

import (
	"strings"
	"testing"
)

// La regla del núcleo agnóstico: ningún paquete bajo internal/ importa un
// adaptador, salvo los propios adaptadores. El único lugar que conoce a la vez
// el núcleo y los adaptadores es cmd/bflow (la raíz de composición).
func TestCoreNeverImportsAdapters(t *testing.T) {
	pkgs, err := List("innobytes.tech/bflow/...")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatal("go list no devolvió paquetes")
	}
	for _, v := range Violations(pkgs, Module) {
		if v.Via == "" {
			t.Errorf("%s importa el adaptador %s", v.Pkg, v.Adapter)
		} else {
			t.Errorf("%s alcanza el adaptador %s vía %s", v.Pkg, v.Adapter, v.Via)
		}
	}
}

// El detector tiene que atrapar también importaciones indirectas.
func TestViolationsDetectsDirectAndTransitive(t *testing.T) {
	m := "example.com/m"
	pkgs := []Package{
		{ImportPath: m + "/internal/flow", Imports: []string{"fmt"}, Deps: []string{"fmt"}},
		{ImportPath: m + "/internal/engine", Imports: []string{m + "/internal/helper"},
			Deps: []string{m + "/internal/helper", m + "/internal/adapters/tracker/plane"}},
		{ImportPath: m + "/internal/store", Imports: []string{m + "/internal/adapters/secrets/keyring"},
			Deps: []string{m + "/internal/adapters/secrets/keyring"}},
		{ImportPath: m + "/internal/adapters/tracker/plane", Imports: []string{m + "/internal/adapters/tracker/local"},
			Deps: []string{m + "/internal/adapters/tracker/local"}},
		{ImportPath: m + "/cmd/bflow", Imports: []string{m + "/internal/adapters/tracker/plane"},
			Deps: []string{m + "/internal/adapters/tracker/plane"}},
	}
	got := Violations(pkgs, m)
	if len(got) != 2 {
		t.Fatalf("esperaba 2 violaciones (engine transitiva, store directa), got %d: %+v", len(got), got)
	}
	var names []string
	for _, v := range got {
		names = append(names, v.Pkg)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "internal/engine") || !strings.Contains(joined, "internal/store") {
		t.Errorf("violaciones inesperadas: %v", names)
	}
}
