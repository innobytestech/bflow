// Package git implementa vcs.Git llamando al binario git.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"innobytes.tech/bflow/internal/vcs"
)

// Git opera sobre el repo en Dir.
type Git struct {
	Dir       string
	Remote    string
	Protected []string // ramas a las que bflow nunca empuja
}

// New crea un Git sobre dir con remoto origin.
func New(dir string) *Git {
	return &Git{Dir: dir, Remote: "origin", Protected: []string{"main", "master"}}
}

var _ vcs.Git = (*Git)(nil)

func (g *Git) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(out.String()), nil
}

func (g *Git) ok(ctx context.Context, args ...string) bool {
	_, err := g.run(ctx, args...)
	return err == nil
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (g *Git) CurrentBranch(ctx context.Context) (string, error) {
	b, err := g.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if b == "HEAD" {
		return "", err
	}
	return b, err
}

func (g *Git) hasRemote(ctx context.Context) bool {
	return g.ok(ctx, "remote", "get-url", g.Remote)
}

// EnsureBranch sigue la lógica de branch.mjs: retoma la rama local, o la
// trackea si solo existe en el remoto, o la crea desde la base actualizada.
func (g *Git) EnsureBranch(ctx context.Context, name, base string) (string, error) {
	remote := g.hasRemote(ctx)
	if remote {
		if _, err := g.run(ctx, "fetch", g.Remote); err != nil {
			return "", err
		}
	}
	local := g.ok(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	onRemote := remote && g.ok(ctx, "show-ref", "--verify", "--quiet", "refs/remotes/"+g.Remote+"/"+name)
	switch {
	case local:
		if _, err := g.run(ctx, "checkout", name); err != nil {
			return "", err
		}
		if onRemote {
			if _, err := g.run(ctx, "pull", "--ff-only", g.Remote, name); err != nil {
				return "", err
			}
		}
		return "resumed", nil
	case onRemote:
		_, err := g.run(ctx, "checkout", "-b", name, g.Remote+"/"+name)
		return "tracked", err
	}
	start := "HEAD"
	if base != "" {
		switch {
		case remote && g.ok(ctx, "show-ref", "--verify", "--quiet", "refs/remotes/"+g.Remote+"/"+base):
			start = g.Remote + "/" + base
		case g.ok(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+base):
			start = base
		default:
			return "", fmt.Errorf("la rama base %q no existe ni local ni en %s", base, g.Remote)
		}
	}
	_, err := g.run(ctx, "checkout", "-b", name, start)
	return "created", err
}

func (g *Git) Dirty(ctx context.Context, paths []string) ([]string, error) {
	out, err := g.run(ctx, append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, l := range lines(out) {
		if len(l) > 3 {
			f := strings.TrimSpace(l[2:])
			if i := strings.Index(f, " -> "); i >= 0 {
				f = f[i+4:]
			}
			files = append(files, strings.Trim(f, `"`))
		}
	}
	return files, nil
}

func (g *Git) HeadSHA(ctx context.Context) (string, error) { return g.run(ctx, "rev-parse", "HEAD") }

func (g *Git) Commit(ctx context.Context, paths []string, msg string) (bool, error) {
	dirty, err := g.Dirty(ctx, paths)
	if err != nil || len(dirty) == 0 {
		return false, err
	}
	if _, err := g.run(ctx, append([]string{"add", "--"}, paths...)...); err != nil {
		return false, err
	}
	// Con rutas, commit toma solo esas (--only): lo que otro tenga en stage no se mezcla.
	if _, err := g.run(ctx, append([]string{"commit", "-m", msg, "--"}, paths...)...); err != nil {
		return false, err
	}
	return true, nil
}

func (g *Git) ChangedSince(ctx context.Context, sha string, paths []string) ([]string, error) {
	out, err := g.run(ctx, append([]string{"diff", "--name-only", sha, "HEAD", "--"}, paths...)...)
	return lines(out), err
}

func (g *Git) DiffNames(ctx context.Context, base string) ([]string, error) {
	out, err := g.run(ctx, "diff", "--name-only", base+"...HEAD")
	return lines(out), err
}

func (g *Git) DiffKept(ctx context.Context, base string, paths []string) ([]string, error) {
	out, err := g.run(ctx, append([]string{"diff", "--name-only", "--diff-filter=d", base + "...HEAD", "--"}, paths...)...)
	return lines(out), err
}

// Commits lista los commits sin merges de base..HEAD, del más viejo al más nuevo.
func (g *Git) Commits(ctx context.Context, base string) ([]string, error) {
	out, err := g.run(ctx, "rev-list", "--no-merges", "--topo-order", "--reverse", base+"..HEAD")
	return lines(out), err
}

// CommitKept lista los archivos que sha agrega o modifica contra su padre.
func (g *Git) CommitKept(ctx context.Context, sha string) ([]string, error) {
	out, err := g.run(ctx, "diff-tree", "--no-commit-id", "--name-only", "-r", "--root", "--diff-filter=d", sha)
	return lines(out), err
}

// MergeBase devuelve el ancestro común de HEAD con la rama principal.
func (g *Git) MergeBase(ctx context.Context, base string) (string, error) {
	var cands []string
	if base == "" {
		cands = []string{g.Remote + "/HEAD", g.Remote + "/main", g.Remote + "/master", "main", "master"}
	} else {
		cands = []string{g.Remote + "/" + base, base}
	}
	for _, c := range cands {
		if !g.ok(ctx, "rev-parse", "--verify", "--quiet", c+"^{commit}") {
			continue
		}
		if sha, err := g.run(ctx, "merge-base", "HEAD", c); err == nil && sha != "" {
			return sha, nil
		}
	}
	return "", fmt.Errorf("no hay rama principal con la que calcular el merge-base")
}

func (g *Git) Push(ctx context.Context, branch string) error {
	if slices.Contains(g.Protected, branch) {
		return fmt.Errorf("%s es una rama protegida: bflow no empuja a ella", branch)
	}
	_, err := g.run(ctx, "push", "-u", g.Remote, branch)
	return err
}

func (g *Git) RemoteURL(ctx context.Context) (string, error) {
	return g.run(ctx, "remote", "get-url", g.Remote)
}

// UserName devuelve user.name de git ("" si no está configurado).
func (g *Git) UserName(ctx context.Context) string {
	u, _ := g.run(ctx, "config", "user.name")
	return u
}

// ParseRemote se mantiene por compatibilidad; la implementación vive en vcs.
func ParseRemote(url string) (host, repo string, ok bool) { return vcs.ParseRemote(url) }

// DiffLines suma líneas agregadas y borradas entre base y HEAD; los binarios cuentan 0.
func (g *Git) DiffLines(ctx context.Context, base string) (int, error) {
	out, err := g.run(ctx, "diff", "--numstat", "--no-renames", base+"...HEAD")
	if err != nil {
		return 0, err
	}
	total := 0
	for _, l := range lines(out) {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		add, _ := strconv.Atoi(f[0]) // "-" en binarios: 0
		del, _ := strconv.Atoi(f[1])
		total += add + del
	}
	return total, nil
}

// DiffBinaries lista los binarios del diff de base...HEAD (numstat con "-" en las dos primeras columnas).
func (g *Git) DiffBinaries(ctx context.Context, base string) ([]string, error) {
	out, err := g.run(ctx, "diff", "--numstat", "--no-renames", base+"...HEAD")
	if err != nil {
		return nil, err
	}
	var bins []string
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 3)
		if len(f) == 3 && f[0] == "-" && f[1] == "-" {
			bins = append(bins, f[2])
		}
	}
	return bins, nil
}

// DiffAdded devuelve las líneas agregadas de base...HEAD bajo paths. Con
// --unified=0 cada hunk trae solo cambios, y el número de línea sale de "+c,d".
// La cabecera "+++ " solo cuenta entre "diff --git" y el primer "@@": una
// línea agregada "++ x" llega como "+++ x" y no debe vaciar la ruta del archivo.
func (g *Git) DiffAdded(ctx context.Context, base string, paths []string) ([]vcs.AddedLine, error) {
	args := []string{"-c", "core.quotepath=off", "diff", "--unified=0", "--no-color", "--no-ext-diff", "--no-renames", base + "...HEAD"}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, err := g.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var res []vcs.AddedLine
	path, line := "", 0
	// Una línea agregada "++ x" llega como "+++ x": solo es cabecera entre
	// "diff --git" y el primer "@@".
	inHeader := false
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSuffix(l, "\r")
		switch {
		case strings.HasPrefix(l, "diff --git "):
			path = ""
			inHeader = true
		case inHeader && strings.HasPrefix(l, "+++ "):
			path = ""
			if p, ok := strings.CutPrefix(l, "+++ b/"); ok {
				path = p
			}
		case strings.HasPrefix(l, "@@"):
			inHeader = false
			// @@ -a,b +c,d @@
			line = 0
			if i := strings.Index(l, "+"); i >= 0 {
				num := l[i+1:]
				if j := strings.IndexAny(num, ", "); j >= 0 {
					num = num[:j]
				}
				line, _ = strconv.Atoi(num)
			}
		case strings.HasPrefix(l, "+"):
			if path != "" {
				res = append(res, vcs.AddedLine{Path: path, Line: line, Text: l[1:]})
			}
			line++
		}
	}
	return res, nil
}

// Operaciones del eval del reviewer (eval.Git).

// ResolveCommit da el SHA completo del commit al que apunta ref. Un ref con "-"
// inicial se rechaza: git lo leería como opción.
func (g *Git) ResolveCommit(ctx context.Context, ref string) (string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("%q no es un commit", ref)
	}
	sha, err := g.run(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil || sha == "" {
		return "", fmt.Errorf("%q no es un commit", ref)
	}
	return sha, nil
}

// ShowFile lee el contenido exacto de path en ref.
func (g *Git) ShowFile(ctx context.Context, ref, path string) ([]byte, error) {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return nil, fmt.Errorf("%q no es un commit", ref)
	}
	cmd := exec.CommandContext(ctx, "git", "cat-file", "blob", ref+":"+path)
	cmd.Dir = g.Dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s no existe en %s: %s", path, ref, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// WorktreeAdd crea un worktree desacoplado en sha.
func (g *Git) WorktreeAdd(ctx context.Context, dir, sha string) error {
	_, err := g.run(ctx, "worktree", "add", "--detach", "--force", dir, sha)
	return err
}

// WorktreeRemove quita el worktree con --force y poda los registros huérfanos.
func (g *Git) WorktreeRemove(ctx context.Context, dir string) error {
	_, rmErr := g.run(ctx, "worktree", "remove", "--force", dir)
	_, pruneErr := g.run(ctx, "worktree", "prune")
	if rmErr != nil {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return pruneErr // ya no estaba: basta con la poda
		}
		return rmErr
	}
	return pruneErr
}

// IsIgnored dice si git ignora path (git check-ignore -q).
func (g *Git) IsIgnored(ctx context.Context, path string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "check-ignore", "-q", "--", path)
	cmd.Dir = g.Dir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}
