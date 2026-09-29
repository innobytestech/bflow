package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndLoadProfiles(t *testing.T) {
	t.Setenv("BFLOW_CONFIG_HOME", t.TempDir())
	if err := SaveProfile("acme", map[string]any{"tracker": map[string]any{"adapter": "plane", "url": "https://p", "workspace": "w"}, "vcs": map[string]any{"base_branch": "dev"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveProfile("acme", map[string]any{"agent": "claude"}); err != nil { // se combina, no reemplaza
		t.Fatal(err)
	}
	if err := SaveProfile("otro", map[string]any{"vcs": map[string]any{"base_branch": "main"}}); err != nil {
		t.Fatal(err)
	}
	ps, err := LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	p := ps["acme"]
	if p.Tracker.URL != "https://p" || p.VCS.BaseBranch != "dev" || p.Agent != "claude" || ps["otro"].VCS.BaseBranch != "main" {
		t.Errorf("perfiles: %+v", ps)
	}
	if err := SaveProfile("x", map[string]any{"tracker": map[string]any{"token": "s"}}); err == nil {
		t.Error("un perfil no guarda secretos")
	}
}

func TestSaveUI(t *testing.T) {
	t.Setenv("BFLOW_CONFIG_HOME", t.TempDir())
	SaveProfile("acme", map[string]any{"agent": "claude"})
	if d, err := UIDecided(); err != nil || d {
		t.Fatalf("sin elegir: %v %v", d, err)
	}
	// "nada" también es una decisión: no se vuelve a preguntar.
	if err := SaveUI(map[string]any{"watch": false, "web": false}); err != nil {
		t.Fatal(err)
	}
	if d, _ := UIDecided(); !d {
		t.Error("ui.watch en false cuenta como decidido")
	}
	SaveUI(map[string]any{"watch": true, "web": true})
	c, err := Load(t.TempDir())
	if err != nil || !c.UI.Watch || !c.UI.Web {
		t.Errorf("ui: %+v %v", c.UI, err)
	}
	if ps, _ := LoadProfiles(); ps["acme"].Agent != "claude" {
		t.Error("guardar ui conserva los perfiles")
	}
}

func TestSetRepoProfileKeepsComments(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, RepoFile)
	os.WriteFile(p, []byte("# comentario importante\nstack: go # el stack\ncheck:\n  steps:\n    - { name: vet, run: \"go vet ./...\" }\n"), 0o644)
	if err := SetRepoProfile(dir, "acme"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, "profile: acme") || !strings.Contains(s, "# comentario importante") || !strings.Contains(s, "# el stack") {
		t.Errorf("resultado:\n%s", s)
	}
	SetRepoProfile(dir, "otro")
	b, _ = os.ReadFile(p)
	if strings.Count(string(b), "profile:") != 1 || !strings.Contains(string(b), "profile: otro") {
		t.Errorf("reemplazo:\n%s", b)
	}
	empty := t.TempDir()
	if err := SetRepoProfile(empty, "acme"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(empty, RepoFile)); strings.TrimSpace(string(b)) != "profile: acme" {
		t.Errorf("sin archivo: %q", b)
	}
}
