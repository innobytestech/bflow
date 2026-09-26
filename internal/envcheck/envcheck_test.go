package envcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/config"
)

func TestRun(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ready" {
			w.WriteHeader(503)
		}
	}))
	defer api.Close()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedAddr := closed.Addr().String()
	closed.Close()

	cfg := config.Env{APIURL: api.URL, HealthPaths: []string{"/api/health", "/api/ready"}, TimeoutMS: 500,
		TCP:        []string{"postgres=" + ln.Addr().String(), "redis=" + closedAddr},
		RequireEnv: map[string]string{"TEST_POSTGRES_DSN": ":5433/", "FALTA": "x"}}
	env := map[string]string{"TEST_POSTGRES_DSN": "postgres://u@127.0.0.1:5432/db"}
	res := Run(context.Background(), cfg, func(k string) string { return env[k] })
	got := map[string]bool{}
	for _, c := range res {
		got[c.Label] = c.OK
	}
	want := map[string]bool{"GET /api/health": true, "GET /api/ready": false, "postgres": true, "redis": false,
		"TEST_POSTGRES_DSN": false, "FALTA": false}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	text := Text(res, false)
	if !strings.Contains(text, "ENV FAIL: 4 de 6") || !strings.Contains(text, ":5433/") {
		t.Errorf("texto:\n%s", text)
	}
	if Text(nil, true) != "✅ entorno OK" {
		t.Errorf("quiet sin checks: %q", Text(nil, true))
	}
}
