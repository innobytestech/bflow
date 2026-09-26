package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/secrets"
	"innobytes.tech/bflow/internal/testutil"
)

type memSecrets map[string]string

func (m memSecrets) Get(k string) (string, error) {
	v, ok := m[k]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (m memSecrets) Set(k, v string) error { m[k] = v; return nil }
func (m memSecrets) Delete(k string) error { delete(m, k); return nil }

func connectEnv(t *testing.T, stdin string, valid bool) (*Env, memSecrets, *[]ConnectOpts) {
	dir := testutil.TempDir(t)
	t.Setenv("BFLOW_CONFIG_HOME", testutil.TempDir(t))
	os.WriteFile(filepath.Join(dir, "bflow.yaml"), []byte("tracker: { adapter: plane, url: https://plane.example.com, workspace: acme-dev, project: API }\n"), 0o644)
	sec := memSecrets{}
	var seen []ConnectOpts
	env := &Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Stdin: strings.NewReader(stdin), Dir: dir, Secrets: sec,
		Connect: func(_ context.Context, service string, o ConnectOpts) (string, error) {
			seen = append(seen, o)
			if !valid {
				return "", errors.New("plane → 401: token inválido")
			}
			return "2 proyectos visibles: API, WEB", nil
		}}
	return env, sec, &seen
}

func TestConnectPlaneFromStdin(t *testing.T) {
	env, sec, seen := connectEnv(t, "secreto-123\n", true)
	code := Run([]string{"connect", "plane"}, env)
	out := env.Stdout.(*bytes.Buffer).String()
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if sec["plane:plane.example.com"] != "secreto-123" {
		t.Errorf("llavero: %v", sec)
	}
	if (*seen)[0].URL != "https://plane.example.com" || (*seen)[0].Workspace != "acme-dev" {
		t.Errorf("debe tomar url y workspace de la config: %+v", (*seen)[0])
	}
	if strings.Contains(out, "secreto-123") || strings.Contains(env.Stderr.(*bytes.Buffer).String(), "secreto-123") {
		t.Error("el token nunca se imprime")
	}
	if !strings.Contains(out, "API") {
		t.Errorf("salida: %s", out)
	}
}

func TestConnectInvalidDoesNotStore(t *testing.T) {
	env, sec, _ := connectEnv(t, "malo\n", false)
	code := Run([]string{"connect", "plane", "--json"}, env)
	if code != 1 || len(sec) != 0 {
		t.Errorf("token inválido: exit %d, guardado %v", code, sec)
	}
}

func TestConnectGithubFlagsAndDryRun(t *testing.T) {
	env, sec, _ := connectEnv(t, "", true)
	if code := Run([]string{"connect", "github", "--token", "ghp_x", "--dry-run"}, env); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(sec) != 0 {
		t.Error("--dry-run valida pero no guarda")
	}
	Run([]string{"connect", "github", "--token", "ghp_x"}, env)
	if sec["github:github.com"] != "ghp_x" {
		t.Errorf("llavero: %v", sec)
	}
}

func TestConnectUnknownService(t *testing.T) {
	env, _, _ := connectEnv(t, "", true)
	if code := Run([]string{"connect", "jira"}, env); code != 1 {
		t.Errorf("servicio desconocido: exit %d", code)
	}
}
