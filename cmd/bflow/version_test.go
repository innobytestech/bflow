package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildVersion(t *testing.T) {
	info := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Main: debug.Module{Version: v}}, true }
	}
	for _, c := range []struct{ injected, module, want string }{
		{"v0.1.0", "v0.0.9", "v0.1.0"}, // publicado: manda lo inyectado
		{"dev", "v0.1.0", "v0.1.0"},    // go install …@v0.1.0
		{"dev", "(devel)", "dev"},      // go build en el repo
		{"dev", "", "dev"},
	} {
		if got := buildVersion(c.injected, info(c.module)); got != c.want {
			t.Errorf("%s/%s: %q, want %q", c.injected, c.module, got, c.want)
		}
	}
}
