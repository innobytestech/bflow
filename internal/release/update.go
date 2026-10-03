package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultAPI es el repo de GitHub donde se publican las versiones.
// BFLOW_RELEASES_URL lo cambia (pruebas, espejos).
const DefaultAPI = "https://api.github.com/repos/innobytestech/bflow"

// Release es una versión publicada.
type Release struct {
	Tag        string            // v0.1.0
	Prerelease bool              // marcada como prerelease en GitHub
	Draft      bool              // borrador (no se ofrece)
	Assets     map[string]string // nombre → URL de descarga
}

// Client consulta y descarga publicaciones.
type Client struct {
	API  string
	HTTP *http.Client
}

// NewClient usa BFLOW_RELEASES_URL o el repo de bflow.
func NewClient() *Client {
	api := os.Getenv("BFLOW_RELEASES_URL")
	if api == "" {
		api = DefaultAPI
	}
	return &Client{API: strings.TrimRight(api, "/"), HTTP: &http.Client{Timeout: 2 * time.Minute}}
}

// Latest es la última versión publicada. Con pre=false usa /releases/latest
// (sin borradores ni prereleases) y, si GitHub responde 404 porque solo hay
// prereleases, la primera no borrador de la lista. Con pre=true elige la mayor
// según Compare entre las no borrador, prereleases incluidas.
func (c *Client) Latest(ctx context.Context, pre bool) (Release, error) {
	const accept = "application/vnd.github+json"
	if !pre {
		b, err := c.get(ctx, c.API+"/releases/latest", accept)
		if err == nil {
			return decodeRelease(b)
		}
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != http.StatusNotFound {
			return Release{}, err
		}
	}
	b, err := c.get(ctx, c.API+"/releases?per_page=10", accept)
	if err != nil {
		return Release{}, err
	}
	var list []json.RawMessage
	if err := json.Unmarshal(b, &list); err != nil {
		return Release{}, fmt.Errorf("respuesta de releases: %w", err)
	}
	var best Release
	found := false
	for _, raw := range list {
		rel, err := decodeRelease(raw)
		if err != nil || rel.Draft {
			continue
		}
		if !pre {
			return rel, nil
		}
		if _, err := Compare(rel.Tag, rel.Tag); err != nil {
			continue
		}
		if cmp, _ := Compare(rel.Tag, best.Tag); !found || cmp > 0 {
			best, found = rel, true
		}
	}
	if !found {
		return Release{}, errors.New("no hay versiones publicadas")
	}
	return best, nil
}

func decodeRelease(b []byte) (Release, error) {
	var r struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return Release{}, fmt.Errorf("respuesta de releases: %w", err)
	}
	rel := Release{Tag: r.Tag, Prerelease: r.Prerelease, Draft: r.Draft, Assets: map[string]string{}}
	for _, a := range r.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// HTTPError es una respuesta que no fue 200.
type HTTPError struct {
	URL    string
	Status int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("GET %s: %d %s", e.URL, e.Status, http.StatusText(e.Status))
}

// Binary descarga el binario de rel para esta plataforma y lo verifica contra
// checksums.txt.
func (c *Client) Binary(ctx context.Context, rel Release) ([]byte, error) {
	name := AssetName(rel.Tag, runtime.GOOS, runtime.GOARCH)
	url, ok := rel.Assets[name]
	if !ok {
		return nil, fmt.Errorf("%s no publica %s (%s/%s)", rel.Tag, name, runtime.GOOS, runtime.GOARCH)
	}
	sumsURL, ok := rel.Assets[Checksums]
	if !ok {
		return nil, fmt.Errorf("%s no publica %s: no se puede verificar la descarga", rel.Tag, Checksums)
	}
	sums, err := c.get(ctx, sumsURL, "")
	if err != nil {
		return nil, err
	}
	want := ParseChecksums(string(sums))[name]
	archive, err := c.get(ctx, url, "")
	if err != nil {
		return nil, err
	}
	if got := SHA256(archive); want == "" || got != want {
		return nil, fmt.Errorf("%s no coincide con %s (esperado %q, descargado %s): no se instala", name, Checksums, want, got)
	}
	return Extract(archive, name, runtime.GOOS)
}

func (c *Client) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{URL: url, Status: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// Replace cambia el ejecutable exe por bin. En Windows un ejecutable en uso no
// se puede sobrescribir pero sí renombrar: el actual queda como .old y se
// borra en la próxima actualización.
func Replace(exe string, bin []byte) error {
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	CleanOld(exe)
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return fmt.Errorf("no se puede escribir junto a %s (%w); reinstala con permisos o en una carpeta tuya", exe, err)
	}
	if runtime.GOOS == "windows" {
		if err := os.Rename(exe, exe+".old"); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, exe); err != nil {
		if runtime.GOOS == "windows" {
			_ = os.Rename(exe+".old", exe)
		}
		os.Remove(tmp)
		return err
	}
	return nil
}

// CleanOld borra el ejecutable anterior que dejó una actualización en Windows.
func CleanOld(exe string) { _ = os.Remove(exe + ".old") }
