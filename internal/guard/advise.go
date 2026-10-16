package guard

import (
	"slices"
	"strings"
)

// Advise devuelve el aviso que sugiere `bflow check --quick` cuando a es un
// Bash que corre la prueba del stack directo (GH-17), o "" si no aplica. Mira el
// ejecutable al inicio de cada segmento, no subcadenas: `echo go test` no cuenta.
func Advise(a Action) string {
	if a.Tool != Bash {
		return ""
	}
	for _, s := range Segments(a.Command) {
		if BflowSubcommand(s) != "" {
			continue
		}
		t := skipPrefix(quotedFields(s))
		if len(t) == 0 || !isTestRunner(t) {
			continue
		}
		return "`" + shorten(t) + "` sí corrió (aviso, no bloqueo). Para la próxima, `bflow check --quick <paquete>` corre las pruebas rápidas de este repo con menos salida."
	}
	return ""
}

func isTestRunner(t []string) bool {
	arg := func(i int) string {
		if i < len(t) {
			return strings.ToLower(t[i])
		}
		return ""
	}
	switch exeName(t[0]) {
	case "go", "cargo", "dotnet":
		return arg(1) == "test"
	case "npm", "pnpm", "yarn":
		return arg(1) == "test" || (arg(1) == "run" && arg(2) == "test")
	case "npx":
		return slices.Contains([]string{"vitest", "jest"}, arg(1))
	case "pytest":
		return true
	case "python", "python3", "py":
		return arg(1) == "-m" && arg(2) == "pytest"
	}
	return false
}

func shorten(t []string) string {
	s := strings.Join(t, " ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60]) + "..."
	}
	return s
}
