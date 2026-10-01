// Package git implementa vcs.Git llamando al binario git.
package git

import (
	"bytes"
	"context"
	"fmt"
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
