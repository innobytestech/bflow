package setup

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// verifyRe reconoce en CI los comandos que verifican el código (pruebas, lint,
// vulnerabilidades, secretos). Los de preparación (instalar, descargar,
// publicar) no se comparan. El grupo 1 es la clave que tiene que aparecer en
// algún paso del check.
var verifyRe = []*regexp.Regexp{
	regexp.MustCompile(`^(go (?:test|vet))\b`),
	regexp.MustCompile(`^(golangci-lint|govulncheck|staticcheck|gitleaks|gosec)\b`),
	regexp.MustCompile(`^((?:npm|pnpm|yarn) (?:test|run [\w:.-]+))`),
	regexp.MustCompile(`^(npx [\w@/.-]+)`),
	regexp.MustCompile(`^(?:python -m )?(pytest|ruff|mypy|flake8)\b`),
	regexp.MustCompile(`^(dotnet (?:test|build))\b`),
	regexp.MustCompile(`^((?:\./)?(?:mvnw?|gradlew?) .*?\b(?:test|verify|check)\b)`),
}

var (
	tagsRe = regexp.MustCompile(`-tags[= ]["']?([\w,]+)`)
	sepRe  = regexp.MustCompile(`&&|;`)
)

// CIGaps compara los comandos de verificación de .github/workflows con los
// pasos del check y describe los que el check no corre, incluidas las -tags
// de go test (las pruebas de integración suelen ir detrás de una). Así la
// persona lo ve antes de que el PR falle en CI.
func CIGaps(dir string, steps []string) (found int, gaps []string) {
	for _, run := range ciCommands(dir) {
		key := ""
		for _, re := range verifyRe {
			if m := re.FindStringSubmatch(run); m != nil {
				key = m[1]
				break
			}
		}
		if key == "" {
			continue
		}
		found++
		var covering []string
		for _, s := range steps {
			if strings.Contains(s, key) {
				covering = append(covering, s)
			}
		}
		if len(covering) == 0 {
			gaps = appendNew(gaps, fmt.Sprintf("CI corre `%s` y ningún paso del check lo hace", run))
			continue
		}
		for _, tag := range tags(run) {
			if !slices.ContainsFunc(covering, func(s string) bool { return slices.Contains(tags(s), tag) }) {
				gaps = appendNew(gaps, fmt.Sprintf("CI corre `%s` y el check no usa -tags %s", run, tag))
			}
		}
	}
	return found, gaps
}

func tags(cmd string) []string {
	var out []string
	for _, m := range tagsRe.FindAllStringSubmatch(cmd, -1) {
		out = append(out, strings.Split(m[1], ",")...)
	}
	return out
}

func appendNew(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// ciCommands lee los run: de los workflows de GitHub Actions, un comando por
// elemento (separa líneas, && y ;).
func ciCommands(dir string) []string {
	var files []string
	for _, pat := range []string{"*.yml", "*.yaml"} {
		m, _ := filepath.Glob(filepath.Join(dir, ".github", "workflows", pat))
		files = append(files, m...)
	}
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var wf struct {
			Jobs map[string]struct {
				Steps []struct {
					Run string `yaml:"run"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if yaml.Unmarshal(b, &wf) != nil {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
			for _, s := range wf.Jobs[name].Steps {
				for _, line := range strings.Split(strings.ReplaceAll(s.Run, "\\\n", " "), "\n") {
					for _, part := range sepRe.Split(line, -1) {
						if p := strings.Join(strings.Fields(part), " "); p != "" && !strings.HasPrefix(p, "#") {
							out = append(out, p)
						}
					}
				}
			}
		}
	}
	return out
}
