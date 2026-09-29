package engine

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Pruebas huecas en el contrato: las que se saltan siempre o no tienen cuerpo.
// Al aprobar el contrato quedan congeladas, así que después el implementer no
// puede completarlas (en la piloto de ms-sys, 25 pruebas eran t.Skip). Solo se
// marcan los saltos incondicionales: un t.Skip dentro de un if (sin BD, en
// -short) es legítimo.

var (
	goTestStart = regexp.MustCompile(`^func (Test\w*)\(t \*testing\.T\) \{\s*(\})?\s*$`)
	goSkip      = regexp.MustCompile(`^t\.(Skip|SkipNow|Skipf)\(`)
	jsSkip      = regexp.MustCompile(`\b(it|test|describe)\.(skip|todo)\(|\bx(it|test|describe)\(`)
	pySkip      = regexp.MustCompile(`^@(pytest\.mark\.skip|unittest\.skip)\b`)
	jvmSkip     = regexp.MustCompile(`^@(Disabled|Ignore)\b`)
	csSkip      = regexp.MustCompile(`\[(Fact|Theory)\([^)]*Skip\s*=`)
)

// hollowTests devuelve "archivo:línea prueba" de cada prueba hueca en files.
func hollowTests(root string, files []string) []string {
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		out = append(out, hollowIn(f, b)...)
	}
	return out
}

func hollowIn(name string, b []byte) []string {
	var out []string
	add := func(n int, what string) { out = append(out, fmt.Sprintf("%s:%d %s", name, n, what)) }
	isGo := strings.HasSuffix(name, ".go")
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<20)
	pending, pendingLine := "", 0 // prueba de Go abierta, esperando su primera sentencia
	for n := 1; sc.Scan(); n++ {
		l := strings.TrimSpace(sc.Text())
		if isGo {
			if pending != "" && l != "" && !strings.HasPrefix(l, "//") {
				switch {
				case l == "}":
					add(pendingLine, pending+" sin cuerpo")
				case goSkip.MatchString(l):
					add(pendingLine, pending+" se salta siempre")
				}
				pending = ""
			}
			if m := goTestStart.FindStringSubmatch(l); m != nil {
				if m[2] != "" {
					add(n, m[1]+" sin cuerpo")
				} else {
					pending, pendingLine = m[1], n
				}
			}
			continue
		}
		switch {
		case jsSkip.MatchString(l), pySkip.MatchString(l), jvmSkip.MatchString(l), csSkip.MatchString(l):
			add(n, l)
		}
	}
	return out
}
