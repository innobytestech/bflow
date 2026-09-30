// Package leader guarda el cuerpo común con el que la sesión principal conduce
// el flujo: la skill de Claude Code y el comando /bflow de OpenCode salen de
// él, cada una con su encabezado y el nombre de su herramienta de preguntas.
package leader

import (
	_ "embed"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var marker = regexp.MustCompile(`\{\{[a-z_]+\}\}`)

// body es el cuerpo común; lleva marcadores {{nombre}} que Render reemplaza.
//
//go:embed bflow.md
var body []byte

// Render concatena head y el cuerpo común y reemplaza cada {{nombre}} por
// vars[nombre]. Falla si queda un marcador sin reemplazar o sobra una variable.
func Render(head []byte, vars map[string]string) ([]byte, error) {
	out := string(head) + string(body)
	used := map[string]bool{}
	for name, v := range vars {
		k := "{{" + name + "}}"
		if strings.Contains(out, k) {
			used[name] = true
			out = strings.ReplaceAll(out, k, v)
		}
	}
	var extra []string
	for name := range vars {
		if !used[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return nil, fmt.Errorf("la variable %s no la usa ningún marcador", strings.Join(extra, ", "))
	}
	// Un valor podría traer otro marcador; se busca en el resultado completo.
	if m := marker.FindAllString(out, -1); len(m) > 0 {
		slices.Sort(m)
		return nil, fmt.Errorf("marcador sin valor: %s", strings.Join(slices.Compact(m), ", "))
	}
	return []byte(out), nil
}
