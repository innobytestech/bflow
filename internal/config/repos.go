package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// repos.json, en la carpeta global, lista los repos donde se usó bflow. La
// página del panel los muestra todos, agrupados por perfil. No es
// configuración: bflow lo escribe solo y quitar una línea no rompe nada.

func reposPath() string { return filepath.Join(GlobalDir(), "repos.json") }

type reposFile struct {
	Repos []string `json:"repos"`
}

func readRepos() []string {
	b, err := os.ReadFile(reposPath())
	if err != nil {
		return nil
	}
	var f reposFile
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	return f.Repos
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// RememberRepo anota root en repos.json si no estaba.
func RememberRepo(root string) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	repos := readRepos()
	if slices.ContainsFunc(repos, func(r string) bool { return samePath(r, abs) }) {
		return nil
	}
	b, err := json.MarshalIndent(reposFile{Repos: append(repos, abs)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(GlobalDir(), 0o755); err != nil {
		return err
	}
	tmp := reposPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, reposPath())
}

// KnownRepos son los repos anotados que todavía tienen bflow.yaml.
func KnownRepos() []string {
	var out []string
	for _, r := range readRepos() {
		if _, err := os.Stat(filepath.Join(r, RepoFile)); err == nil {
			out = append(out, r)
		} else if !errors.Is(err, os.ErrNotExist) {
			out = append(out, r) // sin permiso u otro error: que lo diga la página
		}
	}
	return out
}
