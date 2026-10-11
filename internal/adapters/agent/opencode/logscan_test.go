package opencode

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/agents"
)

var phRe = regexp.MustCompile(`\{\{(UTC|LOC)-(\d+)([hd])\}\}|\{\{T:([^}]*)\}\}`)

// fixture lee un testdata y resuelve fechas relativas a now y rutas dentro de root.
func fixture(t *testing.T, name, root string, now time.Time) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return phRe.ReplaceAllStringFunc(string(b), func(m string) string {
		g := phRe.FindStringSubmatch(m)
		if g[4] != "" {
			p := filepath.Join(root, filepath.FromSlash(g[4]))
			return strings.ReplaceAll(p, `\`, `\\`)
		}
		n, _ := strconv.Atoi(g[2])
		d := time.Duration(n) * time.Hour
		if g[3] == "d" {
			d *= 24
		}
		if g[1] == "UTC" {
			return now.Add(-d).UTC().Format("2006-01-02T15:04:05.000Z")
		}
		return now.Add(-d).In(time.Local).Format("2006-01-02T15:04:05")
	})
}

// logEnv prepara XDG_DATA_HOME con los logs dados (nombre → fixture) y devuelve el repo.
func logEnv(t *testing.T, now time.Time, logs map[string]string) (root string) {
	t.Helper()
	data := t.TempDir()
	root = t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	dir := filepath.Join(data, "opencode", "log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, fx := range logs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(fixture(t, fx, root, now)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func find(fs []agents.LogFinding, area, level string) []agents.LogFinding {
	var out []agents.LogFinding
	for _, f := range fs {
		if f.Area == area && f.Level == level {
			out = append(out, f)
		}
	}
	return out
}

func TestLogDirXDG(t *testing.T) {
	x := t.TempDir()
	t.Setenv("XDG_DATA_HOME", x)
	if got, want := LogDir(), filepath.Join(x, "opencode", "log"); got != want {
		t.Errorf("LogDir = %s, quiero %s", got, want)
	}
}

func TestLogDirDefault(t *testing.T) {
	h := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	if got, want := LogDir(), filepath.Join(h, ".local", "share", "opencode", "log"); got != want {
		t.Errorf("LogDir = %s, quiero %s", got, want)
	}
}

func TestDoctorLogVersion2(t *testing.T) {
	now := time.Now()
	root := logEnv(t, now, map[string]string{"opencode.log": "log_v2.txt"})
	fs := Agent{}.DoctorLog(root, now)
	a := find(fs, "agente", "ok")
	if len(a) != 1 || !strings.Contains(a[0].Msg, "versión 2.0.22") || !strings.Contains(a[0].Msg, "bflow2/") {
		t.Errorf("toma el último cli starting: %+v", fs)
	}
	if find(fs, "agente", "warn") != nil {
		t.Errorf("2.x es una versión probada: %+v", fs)
	}
}

func TestDoctorLogVersionFutura(t *testing.T) {
	now := time.Now()
	root := logEnv(t, now, nil)
	line := "timestamp=" + now.UTC().Format("2006-01-02T15:04:05.000Z") + ` level=INFO message="cli starting" version=3.1.0` + "\n"
	if err := os.WriteFile(filepath.Join(LogDir(), "opencode.log"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := find(Agent{}.DoctorLog(root, now), "agente", "warn"); len(w) != 1 || !strings.Contains(w[0].Msg, "no probado") {
		t.Errorf("major 3: warn: %+v", w)
	}
}

func TestDoctorLogPluginFallo(t *testing.T) {
	now := time.Now()
	root := logEnv(t, now, map[string]string{"opencode.log": "log_v2.txt"})
	w := find(Agent{}.DoctorLog(root, now), "log", "warn")
	if len(w) != 1 {
		t.Fatalf("un solo fallo de este repo: %+v", w)
	}
	for _, s := range []string{".opencode/plugins/bflow2", "Cannot find module", "bflow render", LogDir()} {
		if !strings.Contains(w[0].Msg, s) {
			t.Errorf("falta %q: %s", s, w[0].Msg)
		}
	}
	if strings.Contains(w[0].Msg, root) {
		t.Errorf("el target va relativo al repo: %s", w[0].Msg)
	}
}

func TestDoctorLogBflowJSEn2EsNormal(t *testing.T) {
	now := time.Now()
	root := logEnv(t, now, nil)
	line := "timestamp=" + now.Add(-time.Hour).UTC().Format("2006-01-02T15:04:05.000Z") +
		` level=WARN message="failed to load plugin" target="` +
		strings.ReplaceAll(filepath.Join(root, ".opencode", "plugins", "bflow.js"), `\`, `\\`) +
		`" cause="must export a default definition"` + "\n"
	if err := os.WriteFile(filepath.Join(LogDir(), "opencode.log"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := Agent{}.DoctorLog(root, now)
	if find(fs, "log", "warn") != nil {
		t.Errorf("fallo normal de bflow.js en 2.x no avisa: %+v", fs)
	}
}

func TestDoctorLogFalloViejo(t *testing.T) {
	now := time.Now()
	root := logEnv(t, now, map[string]string{"opencode.log": "log_v2.txt"})
	p := filepath.Join(root, ".opencode", "plugins", "bflow2", "index.js")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now.Add(-90*time.Minute), now.Add(-90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if w := find(Agent{}.DoctorLog(root, now), "log", "warn"); len(w) != 1 {
		t.Fatalf("el fallo es posterior al render: avisa: %+v", w)
	}
	if err := os.Chtimes(p, now, now); err != nil {
		t.Fatal(err)
	}
	if w := find(Agent{}.DoctorLog(root, now), "log", "warn"); w != nil {
		t.Errorf("render posterior al fallo: sin aviso: %+v", w)
	}
}

func TestDoctorLogProveedor(t *testing.T) {
	now := time.Now()
	logs := map[string]string{"opencode.log": "log_v2.txt", "2026-10-11T022342.log": "log_v1.txt"}
	root := logEnv(t, now, logs)
	p := find(Agent{}.DoctorLog(root, now), "proveedor", "warn")
	if len(p) != 2 {
		t.Fatalf("regiones Global y certificado: %+v", p)
	}
	all := p[0].Msg + "|" + p[1].Msg
	for _, s := range []string{"Global", "Privacy", "NODE_EXTRA_CA_CERTS"} {
		if !strings.Contains(all, s) {
			t.Errorf("falta %q: %s", s, all)
		}
	}
	// Con las fechas 8 días atrás ya no avisa.
	root = logEnv(t, now.Add(-8*24*time.Hour), logs)
	if p := find(Agent{}.DoctorLog(root, now), "proveedor", "warn"); p != nil {
		t.Errorf("fuera de los 7 días: %+v", p)
	}
}

func TestDoctorLogSinLog(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := Agent{}.DoctorLog(t.TempDir(), time.Now())
	if len(fs) != 1 || fs[0].Area != "log" || fs[0].Level != "ok" || !strings.Contains(fs[0].Msg, "no hay log") {
		t.Errorf("un solo log ok: %+v", fs)
	}
}

func TestDoctorLogNoFiltraPrompt(t *testing.T) {
	now := time.Now()
	root := logEnv(t, now, map[string]string{"opencode.log": "log_v2.txt", "2026-10-11T022342.log": "log_v1.txt"})
	long := strings.Repeat("y", 400)
	line := "timestamp=" + now.UTC().Format("2006-01-02T15:04:05.000Z") + ` level=WARN message="failed to load plugin" target="` +
		strings.ReplaceAll(filepath.Join(root, ".opencode", "plugins", "otro.js"), `\`, `\\`) + `" cause="` + long + `"` + "\n"
	f, err := os.OpenFile(filepath.Join(LogDir(), "opencode.log"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(line)
	f.Close()
	sawLong := false
	for _, f := range (Agent{}).DoctorLog(root, now) {
		if strings.Contains(f.Msg, "PROMPT-SECRETO") {
			t.Errorf("filtra texto de la línea: %+v", f)
		}
		if strings.Contains(f.Msg, strings.Repeat("y", 161)) {
			t.Errorf("cause sin recortar a 160: %+v", f)
		}
		sawLong = sawLong || strings.Contains(f.Msg, strings.Repeat("y", 160))
	}
	if !sawLong {
		t.Error("el cause largo debe salir recortado, no omitido")
	}
}

func TestDoctorLogSoloCola(t *testing.T) {
	now := time.Now()
	root := logEnv(t, now, nil)
	old := "timestamp=" + now.UTC().Format("2006-01-02T15:04:05.000Z") + ` level=INFO message="cli starting" version=2.0.1` + "\n"
	pad := strings.Repeat("x", 2<<20) + "\n"
	if err := os.WriteFile(filepath.Join(LogDir(), "opencode.log"), []byte(old+pad), 0o644); err != nil {
		t.Fatal(err)
	}
	if a := find(Agent{}.DoctorLog(root, now), "agente", "ok"); a != nil {
		t.Errorf("lo que queda fuera del último MiB no se lee: %+v", a)
	}
}
