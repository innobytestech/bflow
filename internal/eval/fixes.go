package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"innobytes.tech/bflow/internal/store"
)

var taskIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func okTaskID(id string) bool { return taskIDRe.MatchString(id) && !strings.Contains(id, "..") }

// FromFixes crea un esqueleto (draft) por cada start con fixes cuyo caso no
// exista. created son las carpetas nuevas, relativas a la raíz.
func FromFixes(root string, log []store.Entry) (created []string, warnings []string, err error) {
	created = []string{}
	found := false
	for _, e := range log {
		if e.Event != "start" {
			continue
		}
		x, _ := e.Data["fixes"].(string)
		if x == "" {
			continue
		}
		found = true
		name := strings.ToLower(x + "-fix-" + e.ID)
		if !okTaskID(x) || !okTaskID(e.ID) || !validName(name) {
			warnings = append(warnings, "se omite "+e.ID+" (fixes "+x+"): el nombre no sirve de carpeta")
			continue
		}
		rel := CasesDir + "/" + name
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if _, serr := os.Stat(filepath.Join(dir, "case.yaml")); serr == nil {
			continue
		}
		c := Case{Draft: true, Source: x, Hotfix: e.ID, Defects: []Defect{}}
		var st struct {
			StartSHA string `json:"start_sha"`
		}
		readJSON(filepath.Join(root, ".bflow", "tasks", x, "state.json"), &st)
		var ck struct {
			SHA string `json:"sha"`
		}
		readJSON(filepath.Join(root, ".bflow", "tasks", x, "check.json"), &ck)
		c.Base, c.Head = st.StartSHA, ck.SHA
		raw, merr := yaml.Marshal(c)
		if merr != nil {
			return created, warnings, merr
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return created, warnings, err
		}
		if err := os.WriteFile(filepath.Join(dir, "case.yaml"), raw, 0o644); err != nil {
			return created, warnings, err
		}
		created = append(created, rel)
	}
	if !found {
		warnings = append(warnings, "el log no tiene tareas iniciadas con --fixes: no hay esqueletos que crear")
	}
	if len(created) > 0 {
		if err := ensureGitignore(root); err != nil {
			return created, warnings, err
		}
	}
	return created, warnings, nil
}

func readJSON(path string, v any) {
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, v)
	}
}

// ensureGitignore agrega a .bflow/.gitignore las excepciones que dejan
// versionar .bflow/evals/ (casos y resultados).
func ensureGitignore(root string) error {
	path := filepath.Join(root, ".bflow", ".gitignore")
	raw, _ := os.ReadFile(path)
	text := string(raw)
	have := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add string
	for _, l := range []string{"!evals/", "!evals/**"} {
		if !have[l] {
			add += l + "\n"
		}
	}
	if add == "" {
		return nil
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text+add), 0o644)
}

// Versioning asegura el .gitignore de .bflow/ y avisa si git sigue ignorando
// los casos por el .gitignore de la raíz.
func Versioning(ctx context.Context, root string, g Git) ([]string, error) {
	if err := ensureGitignore(root); err != nil {
		return nil, err
	}
	if ign, err := g.IsIgnored(ctx, CasesDir); err == nil && ign {
		return []string{"git ignora " + CasesDir + "/: en el .gitignore de la raíz cambia `.bflow/` por `.bflow/*` y agrega `!.bflow/evals/` para poder versionar los casos y resultados"}, nil
	}
	return nil, nil
}
