package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRememberRepo(t *testing.T) {
	t.Setenv("BFLOW_CONFIG_HOME", t.TempDir())
	a, b, gone := t.TempDir(), t.TempDir(), t.TempDir()
	for _, d := range []string{a, b, gone} {
		os.WriteFile(filepath.Join(d, RepoFile), []byte("stack: go\n"), 0o644)
	}
	for _, d := range []string{a, b, a, gone} {
		if err := RememberRepo(d); err != nil {
			t.Fatal(err)
		}
	}
	os.Remove(filepath.Join(gone, RepoFile))
	got := KnownRepos()
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("sin repetidos y sin los que ya no tienen bflow.yaml: %v", got)
	}
}
