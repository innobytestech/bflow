package cli

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestBannerMode(t *testing.T) {
	cases := []struct {
		tty, json bool
		getenv    func(string) string
		want      bannerMode
	}{
		{true, false, env(), bannerColor},
		{false, false, env(), bannerOff}, // agente o pipe: nunca
		{true, true, env(), bannerOff},   // --json: nunca
		{true, false, env("NO_COLOR", "1"), bannerPlain},
		{true, false, env("TERM", "dumb"), bannerASCII},
		{true, false, env("TERM", "dumb", "NO_COLOR", "1"), bannerASCII},
	}
	for _, c := range cases {
		if got := modeFor(c.tty, c.json, c.getenv); got != c.want {
			t.Errorf("tty=%v json=%v → %v, want %v", c.tty, c.json, got, c.want)
		}
	}
}

func TestBannerRender(t *testing.T) {
	if renderBanner(bannerOff, "v1") != "" {
		t.Error("apagado no imprime nada")
	}
	for _, m := range []bannerMode{bannerASCII, bannerPlain} {
		out := renderBanner(m, "v0.1.0")
		if strings.Contains(out, "\x1b[") {
			t.Errorf("modo %v sin color no lleva secuencias ANSI", m)
		}
		if !strings.Contains(out, "bflow by innobytes") || !strings.Contains(out, "v0.1.0") {
			t.Errorf("modo %v sin la línea de versión:\n%s", m, out)
		}
		for _, l := range strings.Split(out, "\n") {
			if n := utf8.RuneCountInString(l); n > 64 {
				t.Errorf("modo %v: línea de %d columnas (máximo 64): %q", m, n, l)
			}
		}
	}
	for _, r := range renderBanner(bannerASCII, "v") {
		if r > 127 {
			t.Fatalf("ASCII lleva %q", r)
		}
	}
	if c := renderBanner(bannerColor, "v"); !strings.Contains(c, "\x1b[38;2;") || !strings.Contains(c, "\x1b[0m") {
		t.Error("color: secuencias de 24 bits y reset")
	}
}

// luminance es la luminancia relativa de WCAG.
func luminance(c [3]int) float64 {
	lin := func(v int) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

func contrast(a, b float64) float64 { return (math.Max(a, b) + 0.05) / (math.Min(a, b) + 0.05) }

// El degradado se lee en terminales oscuras y claras: cada extremo tiene al
// menos 3:1 (texto grande, WCAG) contra negro y contra blanco.
func TestBannerContrast(t *testing.T) {
	for _, c := range [][3]int{gradFrom, gradTo} {
		l := luminance(c)
		if k := contrast(l, 0); k < 3 {
			t.Errorf("%v sobre negro: %.2f", c, k)
		}
		if k := contrast(l, 1); k < 3 {
			t.Errorf("%v sobre blanco: %.2f", c, k)
		}
	}
}
