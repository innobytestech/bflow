package secrets

import (
	"errors"
	"testing"
)

type memStore map[string]string

func (m memStore) Get(k string) (string, error) {
	v, ok := m[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (m memStore) Set(k, v string) error { m[k] = v; return nil }
func (m memStore) Delete(k string) error { delete(m, k); return nil }

func TestResolveOrder(t *testing.T) {
	env := map[string]string{}
	r := Resolver{Store: memStore{"plane:plane.example.com": "del-llavero"}, Getenv: func(k string) string { return env[k] }}

	v, src, err := r.Get(Key("plane", "https://plane.example.com/"), "BFLOW_PLANE_TOKEN")
	if err != nil || v != "del-llavero" || src != FromKeyring {
		t.Fatalf("llavero: %q %q %v", v, src, err)
	}

	env["BFLOW_PLANE_TOKEN"] = "de-env"
	v, src, _ = r.Get(Key("plane", "https://plane.example.com"), "BFLOW_PLANE_TOKEN")
	if v != "de-env" || src != FromEnv {
		t.Errorf("la variable de entorno debe ganar (CI): %q %q", v, src)
	}

	_, _, err = r.Get(Key("github", "github.com"), "GH_TOKEN", "GITHUB_TOKEN")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("sin secreto: %v", err)
	}
	env["GITHUB_TOKEN"] = "gh"
	if v, _, _ := r.Get(Key("github", "github.com"), "GH_TOKEN", "GITHUB_TOKEN"); v != "gh" {
		t.Errorf("segunda variable: %q", v)
	}
}

func TestResolveWithoutStore(t *testing.T) {
	r := Resolver{Getenv: func(string) string { return "" }}
	if _, _, err := r.Get("x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("sin llavero disponible debe ser ErrNotFound: %v", err)
	}
}

func TestKeyNormalizesHost(t *testing.T) {
	for _, in := range []string{"https://Plane.Example.com/", "plane.example.com", "http://plane.example.com/api/v1"} {
		if got := Key("plane", in); got != "plane:plane.example.com" {
			t.Errorf("Key(%q) = %q", in, got)
		}
	}
}
