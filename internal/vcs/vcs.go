// Package vcs define lo que el núcleo necesita de git local y del host remoto
// (GitHub, GitLab…). Las implementaciones viven en adaptadores.
package vcs

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

var (
	// ErrNoPR indica que no hay PR para la rama.
	ErrNoPR = errors.New("no hay PR para la rama")
	// ErrNoCredentials lo devuelve un Host sin token; el engine degrada a URL de compare.
	ErrNoCredentials = errors.New("sin credenciales para el host remoto")
	// ErrNoAccess lo devuelve un Host cuyo token no ve el repositorio; el
	// engine también degrada a URL de compare.
	ErrNoAccess = errors.New("el token no tiene acceso al repositorio")
)

// Git son las operaciones locales.
type Git interface {
	// CurrentBranch devuelve la rama actual ("" si HEAD está desacoplado).
	CurrentBranch(ctx context.Context) (string, error)
	// EnsureBranch crea o retoma name desde base. Devuelve "created",
	// "resumed" o "tracked" (existía en el remoto).
	EnsureBranch(ctx context.Context, name, base string) (string, error)
	// Dirty lista archivos modificados sin commitear dentro de paths.
	Dirty(ctx context.Context, paths []string) ([]string, error)
	// HeadSHA devuelve el commit actual.
	HeadSHA(ctx context.Context) (string, error)
	// ChangedSince lista archivos (dentro de paths) que cambiaron entre sha y HEAD.
	ChangedSince(ctx context.Context, sha string, paths []string) ([]string, error)
	// DiffNames lista archivos cambiados entre base y HEAD (triple punto).
	DiffNames(ctx context.Context, base string) ([]string, error)
	// DiffLines suma las líneas agregadas y borradas entre base y HEAD (triple
	// punto); los archivos binarios cuentan 0.
	DiffLines(ctx context.Context, base string) (int, error)
	// Commit hace commit solo de paths con msg, aunque haya otras cosas en
	// stage. Devuelve false si esos archivos no tenían cambios.
	Commit(ctx context.Context, paths []string, msg string) (bool, error)
	// Push publica la rama actual en el remoto.
	Push(ctx context.Context, branch string) error
	// RemoteURL devuelve la URL del remoto.
	RemoteURL(ctx context.Context) (string, error)
}

// PRSpec describe un PR a abrir.
type PRSpec struct {
	Base  string
	Head  string
	Title string
	Body  string // Markdown
}

// PR es un pull request.
type PR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"` // open | closed | merged
	Merged bool   `json:"merged"`
}

// Host son las operaciones del host remoto.
type Host interface {
	Name() string
	OpenPR(ctx context.Context, pr PRSpec) (PR, error)
	FindPR(ctx context.Context, head string) (PR, error)       // ErrNoPR si no hay
	FindMergedPR(ctx context.Context, head string) (PR, error) // ErrNoPR si no hay uno mergeado
	UpdatePRBody(ctx context.Context, number int, body string) error
	PRStatus(ctx context.Context, number int) (PR, error)
	CompareURL(base, head string) string
}

var (
	remoteURLRe = regexp.MustCompile(`^(?:https?|ssh|git)://(?:[^@/]+@)?([^/:]+)(?::\d+)?/(.+?)(?:\.git)?/?$`)
	remoteSCPRe = regexp.MustCompile(`^(?:[^@]+@)?([^:/]+):(.+?)(?:\.git)?/?$`)
)

// ParseRemote extrae host y owner/repo de una URL de remoto (https, ssh o scp).
func ParseRemote(url string) (host, repo string, ok bool) {
	url = strings.TrimSpace(url)
	if m := remoteURLRe.FindStringSubmatch(url); m != nil {
		return m[1], m[2], true
	}
	if strings.Contains(url, "://") || strings.HasPrefix(url, "/") || (len(url) > 1 && url[1] == ':') {
		return "", "", false
	}
	if m := remoteSCPRe.FindStringSubmatch(url); m != nil {
		return m[1], m[2], true
	}
	return "", "", false
}
