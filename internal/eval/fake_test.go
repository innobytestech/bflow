package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	shaBase = "1111111111111111111111111111111111111111"
	shaHead = "2222222222222222222222222222222222222222"
	shaRef  = "3333333333333333333333333333333333333333"
)

// fakeGit resuelve refs de un mapa y simula worktrees con carpetas.
type fakeGit struct {
	commits   map[string]string // ref → SHA
	files     map[string]string // "<ref>:<ruta>" → contenido
	live      map[string]bool   // worktrees agregados y no quitados
	adds      []string          // "<dir>@<sha>"
	removes   []string
	resolved  []string
	failAdd   string // si el dir contiene esto, WorktreeAdd falla
	removeErr error
	ignored   bool
	checked   []string
}

func newFakeGit() *fakeGit {
	return &fakeGit{
		commits: map[string]string{"base1": shaBase, "head1": shaHead, "v1": shaRef, "--upload-pack=x": shaRef},
		files:   map[string]string{},
		live:    map[string]bool{},
	}
}

func (g *fakeGit) ResolveCommit(_ context.Context, ref string) (string, error) {
	g.resolved = append(g.resolved, ref)
	if sha, ok := g.commits[ref]; ok {
		return sha, nil
	}
	return "", errors.New("no es un commit")
}

func (g *fakeGit) ShowFile(_ context.Context, ref, path string) ([]byte, error) {
	if s, ok := g.files[ref+":"+path]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("no existe en el ref")
}

func (g *fakeGit) WorktreeAdd(_ context.Context, dir, sha string) error {
	if g.failAdd != "" && strings.Contains(filepath.ToSlash(dir), g.failAdd) {
		return errors.New("boom: no se pudo crear el worktree")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	g.live[dir] = true
	g.adds = append(g.adds, dir+"@"+sha)
	return nil
}

func (g *fakeGit) WorktreeRemove(_ context.Context, dir string) error {
	g.removes = append(g.removes, dir)
	if g.removeErr != nil {
		return g.removeErr
	}
	delete(g.live, dir)
	return os.RemoveAll(dir)
}

func (g *fakeGit) IsIgnored(_ context.Context, path string) (bool, error) {
	g.checked = append(g.checked, filepath.ToSlash(path))
	return g.ignored, nil
}

const treeCraft = "# oficio del árbol\nrevisa con cuidado\n"

type harness struct {
	t    *testing.T
	root string
	g    *fakeGit
	r    *Runner
	now  time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{t: t, root: root, g: newFakeGit(), now: time.Date(2026, 10, 19, 10, 15, 0, 0, time.Local)}
	h.write(CraftPath, treeCraft)
	h.r = &Runner{Root: root, Git: h.g, Now: func() time.Time { return h.now }, Model: "m-config", Version: "v-test"}
	return h
}

// runName es la hora local de inicio de la corrida del harness.
const runName = "2026-10-19-101500"

func (h *harness) write(rel, content string) {
	h.t.Helper()
	p := filepath.Join(h.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(h.root, filepath.FromSlash(rel)))
	return err == nil
}

func (h *harness) read(rel string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.root, filepath.FromSlash(rel)))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

func (h *harness) caseYAML(name, yaml string) {
	h.write(".bflow/evals/reviewer/"+name+"/case.yaml", yaml)
}

// validCase deja un caso con un defecto en internal/a.go:10-12.
func (h *harness) validCase(name string, extra string) {
	h.caseYAML(name, "source: GH-1\nbase: base1\nhead: head1\n"+extra+"defects:\n  - file: internal/a.go\n    lines: \"10-12\"\n    what: falta validar\n")
}

func (h *harness) next(o Opts) Step {
	h.t.Helper()
	st, err := h.r.Next(context.Background(), o)
	if err != nil {
		h.t.Fatalf("Next(%+v): %v", o, err)
	}
	return st
}

func (h *harness) report(c, verdict, note string) CaseResult {
	h.t.Helper()
	res, err := h.r.Report(context.Background(), c, verdict, note)
	if err != nil {
		h.t.Fatalf("Report(%s): %v", c, err)
	}
	return res
}

// rejected exige un *Rejection con ese código.
func rejected(t *testing.T, err error, code string) *Rejection {
	t.Helper()
	var rj *Rejection
	if !errors.As(err, &rj) || rj.Code != code {
		t.Fatalf("quería rechazo %s, llegó %v", code, err)
	}
	return rj
}

// answer escribe el review-map del caso en su carpeta out.
func (h *harness) answer(sp *Spawn, content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(sp.Args["out"], "review-map.md"), []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}
