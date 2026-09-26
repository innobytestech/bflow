// Package envcheck verifica que el entorno local esté listo (port de
// env-check.mjs): API arriba, puertos abiertos y variables que apuntan a donde deben.
package envcheck

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/config"
)

// Check es una verificación.
type Check struct {
	Label string `json:"label"`
	OK    bool   `json:"ok"`
	Why   string `json:"why,omitempty"`
}

// Run corre las verificaciones configuradas.
func Run(ctx context.Context, cfg config.Env, getenv func(string) string) []Check {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 2500 * time.Millisecond
	}
	var out []Check
	client := &http.Client{Timeout: timeout}
	for _, p := range cfg.HealthPaths {
		c := Check{Label: "GET " + p}
		req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(cfg.APIURL, "/")+p, nil)
		resp, err := client.Do(req)
		switch {
		case err != nil:
			c.Why = "sin respuesta: " + err.Error()
		case resp.StatusCode >= 400:
			c.Why = fmt.Sprintf("HTTP %d", resp.StatusCode)
		default:
			c.OK = true
		}
		if resp != nil {
			resp.Body.Close()
		}
		out = append(out, c)
	}
	for _, t := range cfg.TCP {
		name, addr, found := strings.Cut(t, "=")
		if !found {
			name, addr = t, t
		}
		c := Check{Label: name}
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			c.Why = addr + " cerrado"
		} else {
			conn.Close()
			c.OK = true
		}
		out = append(out, c)
	}
	for v, frag := range cfg.RequireEnv {
		c := Check{Label: v}
		val := getenv(v)
		switch {
		case val == "":
			c.Why = "no está definida"
		case !strings.Contains(val, frag):
			c.Why = fmt.Sprintf("debe contener %q", frag)
		default:
			c.OK = true
		}
		out = append(out, c)
	}
	return out
}

// Failed cuenta las verificaciones que fallaron.
func Failed(cs []Check) int {
	n := 0
	for _, c := range cs {
		if !c.OK {
			n++
		}
	}
	return n
}

// Text resume el resultado. Con quiet y todo bien, una sola línea.
func Text(cs []Check, quiet bool) string {
	n := Failed(cs)
	if n == 0 && quiet {
		return "✅ entorno OK"
	}
	var b strings.Builder
	for _, c := range cs {
		if c.OK && quiet {
			continue
		}
		mark := "✅"
		if !c.OK {
			mark = "❌"
		}
		fmt.Fprintf(&b, "%s %s", mark, c.Label)
		if c.Why != "" {
			fmt.Fprintf(&b, " (%s)", c.Why)
		}
		b.WriteString("\n")
	}
	if n > 0 {
		fmt.Fprintf(&b, "ENV FAIL: %d de %d verificaciones", n, len(cs))
	} else {
		b.WriteString("✅ entorno OK")
	}
	return strings.TrimRight(b.String(), "\n")
}
