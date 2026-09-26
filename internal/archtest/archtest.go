// Package archtest verifica reglas de arquitectura sobre el grafo de imports.
package archtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Module es la ruta del módulo de bflow.
const Module = "innobytes.tech/bflow"

// Package es el subconjunto de `go list -json` que necesitamos.
type Package struct {
	ImportPath string
	Imports    []string
	Deps       []string
}

// Violation es un paquete del núcleo que alcanza un adaptador.
type Violation struct {
	Pkg     string
	Adapter string
	Via     string // import directo por el que se llega ("" si es directo)
}

// List ejecuta `go list -json` sobre el patrón dado.
func List(pattern string) ([]Package, error) {
	cmd := exec.Command("go", "list", "-json", pattern)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %v: %s", err, errb.String())
	}
	var pkgs []Package
	dec := json.NewDecoder(&out)
	for dec.More() {
		var p Package
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// Violations devuelve cada paquete bajo <module>/internal/ (que no sea un
// adaptador) cuyas dependencias, directas o transitivas, incluyen un adaptador.
func Violations(pkgs []Package, module string) []Violation {
	internal := module + "/internal/"
	adapters := module + "/internal/adapters/"
	var out []Violation
	for _, p := range pkgs {
		if !strings.HasPrefix(p.ImportPath, internal) || strings.HasPrefix(p.ImportPath+"/", adapters) {
			continue
		}
		for _, d := range p.Deps {
			if !strings.HasPrefix(d, adapters) {
				continue
			}
			v := Violation{Pkg: p.ImportPath, Adapter: d}
			if !contains(p.Imports, d) {
				v.Via = firstInternalImport(p.Imports, internal)
			}
			out = append(out, v)
			break
		}
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func firstInternalImport(imports []string, prefix string) string {
	for _, i := range imports {
		if strings.HasPrefix(i, prefix) {
			return i
		}
	}
	return ""
}
