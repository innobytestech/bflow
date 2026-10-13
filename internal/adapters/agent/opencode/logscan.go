package opencode

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/agents"
)

const (
	logTailMax   = 1 << 20 // solo se lee la cola de cada archivo
	fieldMax     = 160
	providerDays = 7
)

// LogDir es la carpeta de logs de OpenCode (también en Windows).
func LogDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "opencode", "log")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode", "log")
}

var (
	tsRe    = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z?`)
	majorRe = regexp.MustCompile(`^\d+`)
)

type logLine struct {
	text string
	ts   time.Time
}

// DoctorLog lee la cola del log de OpenCode (solo lectura) y devuelve lo que
// doctor debe mostrar: versión de 2.x, plugin que no cargó y errores del proveedor.
func (Agent) DoctorLog(root string, now time.Time) []agents.LogFinding {
	dir := LogDir()
	v2 := readTail(filepath.Join(dir, "opencode.log"))
	var v1path string
	var v1mt time.Time
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if e.IsDir() || e.Name() == "opencode.log" || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			if fi, err := e.Info(); err == nil && fi.ModTime().After(v1mt) {
				v1mt, v1path = fi.ModTime(), filepath.Join(dir, e.Name())
			}
		}
	}
	var v1 []logLine
	if v1path != "" {
		v1 = readTail(v1path)
	}
	if v2 == nil && v1 == nil {
		return []agents.LogFinding{{Area: "log", Level: "ok", Msg: "opencode: no hay log en " + dir}}
	}
	var out []agents.LogFinding

	// Versión 2.x: el último "cli starting".
	version := ""
	for _, l := range v2 {
		if strings.Contains(l.text, `message="cli starting"`) {
			if v := logfmt(l.text)["version"]; v != "" {
				version = clip(v)
			}
		}
	}
	if version != "" {
		msg := fmt.Sprintf("opencode 2.x (opencode.log): versión %s, usa .opencode/plugins/bflow2/", version)
		level := "ok"
		if m := majorRe.FindString(version); m != "" {
			if n, _ := strconv.Atoi(m); n > 2 {
				level, msg = "warn", msg+"; formato de plugin no probado en esa versión"
			}
		}
		out = append(out, agents.LogFinding{Area: "agente", Level: level, Msg: msg})
	}

	all := append(append([]logLine{}, v2...), v1...)
	since := pluginMtime(root)
	pdir := normPath(filepath.Join(root, ".opencode", "plugins"))
	failed := map[string]string{} // target relativo al repo → cause
	for _, l := range all {
		if !strings.Contains(l.text, "failed to load plugin") {
			continue
		}
		f := logfmt(l.text)
		target, cause := f["target"], f["cause"]
		if target == "" || !strings.HasPrefix(normPath(target), pdir+string(filepath.Separator)) {
			continue
		}
		if !l.ts.After(since) || strings.Contains(cause, "must export a default definition") {
			continue
		}
		rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(strings.ReplaceAll(target, `\\`, `\`)))
		if err != nil {
			rel = target
		}
		failed[filepath.ToSlash(rel)] = clip(cause)
	}
	order := make([]string, 0, len(failed))
	for rel := range failed {
		order = append(order, rel)
	}
	sort.Strings(order)
	for _, rel := range order {
		out = append(out, agents.LogFinding{Area: "log", Level: "warn", Msg: fmt.Sprintf(
			"opencode no cargó %s: %s; corre bflow render; si sigue, revisa el log en %s", clip(rel), failed[rel], dir)})
	}
	if len(order) == 0 {
		out = append(out, agents.LogFinding{Area: "log", Level: "ok", Msg: "opencode: sin fallos de carga del plugin en " + dir})
	}

	// Errores del proveedor en los últimos 7 días.
	limit := now.Add(-providerDays * 24 * time.Hour)
	home, _ := os.UserHomeDir()
	ams := agentModels(root, home)
	changed := modelsMtime(root, home)
	var cert, region bool
	var last time.Time
	named := map[string]bool{}
	for _, l := range all {
		if l.ts.Before(limit) {
			continue
		}
		if strings.Contains(l.text, "requires Global regions") && l.ts.After(changed) {
			region = true
			if l.ts.After(last) {
				last = l.ts
			}
			for _, am := range ams {
				if namesModel(l.text, am.Model) {
					named[am.Model] = true
				}
			}
		}
		cert = cert || strings.Contains(l.text, "self signed certificate")
	}
	if region {
		out = append(out, regionFindings(ams, named, last, filepath.Join(userConfigDir(home), userConfig))...)
	}
	if cert {
		out = append(out, agents.LogFinding{Area: "proveedor", Level: "warn", Msg: "certificado autofirmado (antivirus o proxy con inspección HTTPS): define NODE_EXTRA_CA_CERTS con su CA"})
	}
	return out
}

func userConfigDir(home string) string {
	if d, err := configDir(home); err == nil {
		return d
	}
	return "~/.config/opencode"
}

// namesModel dice si la línea nombra el modelo (id completo, o su parte tras el proveedor).
func namesModel(line, model string) bool {
	f := logfmt(line)
	short := model
	if i := strings.Index(model, "/"); i >= 0 {
		short = model[i+1:]
	}
	if id := f["modelID"]; id != "" && (id == short || id == model || f["providerID"]+"/"+id == model) {
		return true
	}
	if id := f["model"]; id != "" && (id == short || id == model) {
		return true
	}
	return containsID(line, model) || containsID(line, short)
}

// containsID busca id en s sin que forme parte de un identificador más largo.
func containsID(s, id string) bool {
	if id == "" {
		return false
	}
	isID := func(b byte) bool {
		return b == '-' || b == '.' || b == '_' || b == '/' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	for off := 0; ; {
		i := strings.Index(s[off:], id)
		if i < 0 {
			return false
		}
		st, en := off+i, off+i+len(id)
		if (st == 0 || !isID(s[st-1])) && (en == len(s) || !isID(s[en])) {
			return true
		}
		off = st + 1
	}
}

func regionFindings(ams []AgentModel, named map[string]bool, last time.Time, ocPath string) []agents.LogFinding {
	marked, probable := []AgentModel{}, false
	for _, am := range ams {
		if named[am.Model] {
			marked = append(marked, am)
		}
	}
	if len(named) == 0 {
		for _, am := range ams {
			if strings.HasPrefix(am.Model, "opencode-go/") {
				marked = append(marked, am)
			}
		}
		probable = len(marked) > 0
	}
	exits := "activa Global en Privacy de tu workspace de OpenCode"
	if len(marked) == 0 {
		return []agents.LogFinding{{Area: "proveedor", Level: "warn", Msg: fmt.Sprintf(
			"el proveedor rechazó modelos por región (último: %s): %s o cambia de modelo: agent.<nombre>.model en %s (solo tú) o models.opencode en bflow.yaml y bflow render (equipo)",
			last.UTC().Format("2006-01-02 15:04 UTC"), exits, ocPath)}}
	}
	var groups []string
	idx := map[string]int{}
	var byModel [][]string
	var order []string
	personal, repo := false, false
	for _, am := range marked {
		i, ok := idx[am.Model]
		if !ok {
			i = len(order)
			idx[am.Model] = i
			order = append(order, am.Model)
			byModel = append(byModel, nil)
		}
		byModel[i] = append(byModel[i], am.Agent)
		personal = personal || am.Source == "personal"
		repo = repo || am.Source == "repo"
	}
	for i, m := range order {
		src := map[string]bool{}
		for _, am := range marked {
			if am.Model == m {
				src[am.Source] = true
			}
		}
		var ss []string
		for _, k := range []string{"personal", "repo"} {
			if src[k] {
				ss = append(ss, k)
			}
		}
		groups = append(groups, fmt.Sprintf("%s → %s (%s)", clip(strings.Join(byModel[i], ", ")), clip(m), strings.Join(ss, ", ")))
	}
	var how []string
	if personal {
		how = append(how, fmt.Sprintf("agent.<nombre>.model en %s (solo tú)", ocPath))
	}
	if repo {
		how = append(how, "models.opencode en bflow.yaml y bflow render (equipo)")
	}
	note := ""
	if probable {
		note = " (probable: el log no nombra el modelo)"
	}
	return []agents.LogFinding{
		{Area: "proveedor", Level: "warn", Msg: fmt.Sprintf("el proveedor rechazó modelos por región (último: %s)%s: %s; %s o cambia de modelo: %s",
			last.UTC().Format("2006-01-02 15:04 UTC"), note, strings.Join(groups, "; "), exits, strings.Join(how, " o "))},
		{Area: "modelos", Level: "warn", Msg: fmt.Sprintf("opencode: %d agentes con un modelo rechazado por región; ver proveedor", len(marked))},
	}
}

// pluginMtime es el mtime más reciente de los archivos del plugin en el repo.
func pluginMtime(root string) time.Time {
	var m time.Time
	for _, p := range PluginFiles {
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil && fi.ModTime().After(m) {
			m = fi.ModTime()
		}
	}
	return m
}

func normPath(p string) string {
	p = filepath.Clean(strings.ReplaceAll(p, `\\`, `\`))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func clip(s string) string {
	if r := []rune(s); len(r) > fieldMax {
		return string(r[:fieldMax])
	}
	return s
}

// readTail lee como máximo el último 1 MiB; nil si no se puede leer.
func readTail(path string) []logLine {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	off := int64(0)
	if fi.Size() > logTailMax {
		off = fi.Size() - logTailMax
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	if off > 0 {
		lines = lines[1:] // la primera puede venir cortada
	}
	out := []logLine{}
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" {
			continue
		}
		ts := fi.ModTime()
		if m := tsRe.FindString(ln); m != "" {
			if t, ok := parseTS(m); ok {
				ts = t
			}
		}
		out = append(out, logLine{text: ln, ts: ts})
	}
	return out
}

// parseTS: con Z es UTC; sin zona es hora local.
func parseTS(s string) (time.Time, bool) {
	if strings.HasSuffix(s, "Z") {
		t, err := time.Parse(time.RFC3339Nano, s)
		return t, err == nil
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05.999999999", s, time.Local)
	return t, err == nil
}

// logfmt saca los pares clave=valor de una línea; ignora lo que no entiende.
func logfmt(line string) map[string]string {
	m := map[string]string{}
	i := 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		j := i
		for j < len(line) && line[j] != '=' && line[j] != ' ' {
			j++
		}
		if j >= len(line) || line[j] != '=' {
			i = j
			continue
		}
		key := line[i:j]
		j++
		var val string
		if j < len(line) && line[j] == '"' {
			k := j + 1
			var sb strings.Builder
			for k < len(line) && line[k] != '"' {
				if line[k] == '\\' && k+1 < len(line) && (line[k+1] == '\\' || line[k+1] == '"') {
					k++
				}
				sb.WriteByte(line[k])
				k++
			}
			val, j = sb.String(), k+1
		} else {
			k := j
			for k < len(line) && line[k] != ' ' {
				k++
			}
			val, j = line[j:k], k
		}
		if _, ok := m[key]; !ok {
			m[key] = val
		}
		i = j
	}
	return m
}
