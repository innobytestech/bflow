package check

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// BranchGit es lo que hace falta de git para saber qué tocó la rama.
type BranchGit interface {
	DiffNames(ctx context.Context, base string) ([]string, error)
	DiffRemoved(ctx context.Context, base string) ([]string, error)
	Dirty(ctx context.Context, paths []string) ([]string, error)
}

var (
	// configName: archivos que suelen configurar herramientas, en cualquier carpeta.
	configName = regexp.MustCompile(`(?i)^tsconfig|^(eslint|stylelint|prettier|biome|vite|vitest|jest|karma|webpack|babel|postcss|tailwind|ruff|golangci)[.-]`)
	// proseName: lo único de la raíz que no configura nada.
	proseName = regexp.MustCompile(`(?i)\.(md|txt)$|^licen[cs]e`)
	// gitOctal: un byte escapado por git en una ruta citada (\303\255).
	gitOctal = regexp.MustCompile(`\\[0-3][0-7]{2}`)
	// scriptExt: una mención con esta extensión puede ser la herramienta del paso.
	scriptExt = regexp.MustCompile(`(?i)\.(js|mjs|cjs|ts|py|sh|go|ps1|rb|pl)$`)
)

// BranchFiles son los archivos que tocó la rama: el diff contra base más lo
// que está sin commitear, como el baseline new-only, y además la ruta vieja de
// lo que borró o renombró. Falla si git cita una ruta (no se puede comparar).
func BranchFiles(ctx context.Context, g BranchGit, base string) ([]string, error) {
	if g == nil {
		return nil, errors.New("el repo no tiene git")
	}
	if base == "" {
		return nil, errors.New("no hay rama base (vcs.base_branch) contra la cual comparar")
	}
	diff, err := g.DiffNames(ctx, base)
	if err != nil {
		return nil, err
	}
	removed, err := g.DiffRemoved(ctx, base)
	if err != nil {
		return nil, err
	}
	dirty, err := g.Dirty(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, f := range slices.Concat(diff, removed, dirty) {
		if strings.HasPrefix(strings.TrimSpace(f), `"`) || gitOctal.MatchString(f) {
			return nil, errors.New("git citó la ruta " + f + " (caracteres fuera de ASCII) y no se puede comparar")
		}
		if f = strings.ReplaceAll(strings.TrimSpace(f), `\`, "/"); f != "" && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out, nil
}

// ToolingTouched devuelve un archivo tocado que puede volver rojo un archivo
// intacto sin aparecer en la salida del paso: un manifiesto de dependencias,
// un archivo oculto, una configuración conocida, cualquier archivo de la raíz
// que no sea texto, o algo dentro de la carpeta de la herramienta del paso (la
// que nombra alguno de los comandos cmds o un script de mentions, con su línea base).
func ToolingTouched(root string, cmds []string, mentions []string, touched []string) string {
	var toolDirs []string
	addDir := func(d string) {
		if d = strings.ToLower(strings.Trim(d, "/")); d != "" && d != "." && !slices.Contains(toolDirs, d) {
			toolDirs = append(toolDirs, d)
		}
	}
	for _, cmd := range cmds {
		for _, p := range strings.Fields(cmd) {
			p = strings.TrimSuffix(strings.TrimPrefix(strings.ReplaceAll(p, `\`, "/"), "./"), "/...")
			if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil && InRepo(p) {
				if st.IsDir() {
					addDir(p)
				} else {
					addDir(path.Dir(p))
				}
			}
		}
	}
	for _, m := range mentions {
		if scriptExt.MatchString(m) && RepoFile(root, m) {
			addDir(path.Dir(m))
		}
	}
	for _, t := range touched {
		lt, base := strings.ToLower(t), path.Base(t)
		top := !strings.Contains(t, "/")
		if IsDepManifest(t) || strings.HasPrefix(base, ".") || configName.MatchString(base) || (top && !proseName.MatchString(base)) ||
			slices.ContainsFunc(toolDirs, func(d string) bool { return strings.HasPrefix(lt, d+"/") }) {
			return t
		}
	}
	return ""
}

// TouchedBy devuelve el archivo tocado al que se refiere la ruta p, sin
// distinguir mayúsculas: la misma ruta, una absoluta o relativa a un
// subdirectorio que termina igual, lo contenido en un directorio tocado, o un
// import sin extensión que coincide en sus dos últimos tramos. Con dirs, también
// un directorio que lo contiene (no para menciones: el alcance de un comando,
// como «src/app», contiene casi todo).
func TouchedBy(p string, touched []string, dirs bool) string {
	p = strings.ToLower(strings.TrimSuffix(strings.ReplaceAll(p, `\`, "/"), "/"))
	if p == "" {
		return ""
	}
	tail := lastSegments(p, 2)
	for _, t := range touched {
		lt := strings.ToLower(t)
		if p == lt || strings.HasSuffix(p, "/"+lt) || strings.HasSuffix(lt, "/"+p) || strings.HasPrefix(p, lt+"/") || dirs && strings.HasPrefix(lt, p+"/") {
			return t
		}
		if tail != "" && (tail == lastSegments(lt, 2) || tail == lastSegments(strings.TrimSuffix(lt, path.Ext(lt)), 2)) {
			return t
		}
	}
	return ""
}

// SameDir devuelve un archivo tocado en la misma carpeta que f (sin distinguir
// mayúsculas): un vecino puede cambiar lo que se revisa o se prueba en f.
func SameDir(f string, touched []string) string {
	dir := strings.ToLower(path.Dir(strings.ReplaceAll(f, `\`, "/")))
	for _, t := range touched {
		if strings.ToLower(path.Dir(t)) == dir {
			return t
		}
	}
	return ""
}

// lastSegments son los últimos n tramos de p sin «.» ni «..»; "" si tiene menos.
func lastSegments(p string, n int) string {
	segs := slices.DeleteFunc(strings.Split(p, "/"), func(s string) bool { return s == "" || s == "." || s == ".." })
	if len(segs) < n {
		return ""
	}
	return strings.Join(segs[len(segs)-n:], "/")
}

// RepoFile dice si f es un archivo del repo en root (no un enlace), relativo a
// la raíz y fuera de las dependencias instaladas.
func RepoFile(root, f string) bool {
	if !InRepo(f) {
		return false
	}
	st, err := os.Lstat(filepath.Join(root, filepath.FromSlash(f)))
	return err == nil && st.Mode().IsRegular()
}

// InRepo dice si la ruta f es relativa a la raíz y queda fuera de las
// dependencias instaladas.
func InRepo(f string) bool {
	if f == "" || path.IsAbs(f) || filepath.VolumeName(filepath.FromSlash(f)) != "" {
		return false
	}
	for _, seg := range strings.Split(f, "/") {
		if seg == ".." || seg == "node_modules" || seg == ".git" {
			return false
		}
	}
	return true
}
