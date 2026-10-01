package opencode

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/metrics"
)

// ul arma una línea de .bflow/cache/opencode/<sessionID>.jsonl.
func ul(id, session, parent, agent string, in, out, reasoning, cr, cw int64, completed int64) string {
	var l usageLine
	l.ID, l.SessionID, l.ParentID, l.Agent = id, session, parent, agent
	l.ProviderID, l.ModelID = "anthropic", "claude-sonnet-4"
	l.Created, l.Completed = completed-500, completed
	l.Tokens.Input, l.Tokens.Output, l.Tokens.Reasoning = in, out, reasoning
	l.Tokens.Cache.Read, l.Tokens.Cache.Write = cr, cw
	b, _ := json.Marshal(l)
	return string(b) + "\n"
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func total(ss []metrics.Sample) metrics.Usage {
	var u metrics.Usage
	for _, s := range ss {
		u.Add(s.Usage)
	}
	return u
}

func sources(srcs []metrics.TokenSource) map[string]string {
	m := map[string]string{}
	for _, s := range srcs {
		m[filepath.Base(s.Path)] = s.Agent
	}
	return m
}

// cache deja una carpeta con la sesión principal y dos hijas.
func cache(t *testing.T) string {
	dir := t.TempDir()
	appendTo(t, filepath.Join(dir, "ses_main.jsonl"), ul("m1", "ses_main", "", "build", 1, 1, 0, 0, 0, 1000))
	appendTo(t, filepath.Join(dir, "ses_kid.jsonl"), ul("k1", "ses_kid", "ses_main", "bflow-implementer", 1, 1, 0, 0, 0, 2000))
	appendTo(t, filepath.Join(dir, "ses_anon.jsonl"), ul("a1", "ses_anon", "ses_main", "", 1, 1, 0, 0, 0, 3000))
	appendTo(t, filepath.Join(dir, "notas.txt"), "no es de uso\n")
	return dir
}

func TestTokenSourcesPrincipalTodos(t *testing.T) {
	dir := cache(t)
	srcs, main := Agent{}.TokenSources([]byte(`{"sessionID":"ses_main","parentID":"","cwd":"x"}`), dir)
	want := map[string]string{"ses_main.jsonl": "", "ses_kid.jsonl": "bflow-implementer", "ses_anon.jsonl": "?"}
	if !main || !reflect.DeepEqual(sources(srcs), want) {
		t.Errorf("el idle de la principal lee todos los *.jsonl, la principal sin agente y el hijo sin agente con ?: main=%v %v", main, sources(srcs))
	}
	for _, s := range srcs {
		if filepath.Dir(s.Path) != dir {
			t.Errorf("la ruta debe estar en la carpeta de caché: %s", s.Path)
		}
	}
}

func TestTokenSourcesHijoSoloSuyo(t *testing.T) {
	dir := cache(t)
	srcs, main := Agent{}.TokenSources([]byte(`{"sessionID":"ses_kid","parentID":"ses_main","cwd":"x"}`), dir)
	if main || !reflect.DeepEqual(sources(srcs), map[string]string{"ses_kid.jsonl": "bflow-implementer"}) {
		t.Errorf("el idle de un hijo solo lee su archivo: main=%v %v", main, sources(srcs))
	}
}

func TestTokenSourcesSessionIDInvalido(t *testing.T) {
	dir := cache(t)
	if srcs, _ := (Agent{}).TokenSources([]byte(`{"sessionID":"ses_kid","parentID":"ses_main"}`), dir); len(srcs) != 1 {
		t.Errorf("un sessionID válido sí encuentra su archivo: %v", srcs)
	}
	for _, id := range []string{"../ses_main", "ses/main", `ses\main`, "ses main", "", "ses_main.jsonl"} {
		raw, _ := json.Marshal(map[string]string{"sessionID": id, "parentID": "p"})
		if srcs, _ := (Agent{}).TokenSources(raw, dir); len(srcs) != 0 {
			t.Errorf("sessionID %q no se usa como nombre de archivo: %v", id, srcs)
		}
	}
	if srcs, _ := (Agent{}).TokenSources([]byte("no es json"), dir); len(srcs) != 0 {
		t.Errorf("JSON inválido: sin fuentes: %v", srcs)
	}
}

func TestReadUsageMapeo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ses_1.jsonl")
	appendTo(t, path, ul("m1", "ses_1", "", "build", 100, 30, 5, 20, 7, 1790877056374))
	cur := &metrics.Cursor{}
	got, err := Agent{}.ReadUsage(path, cur)
	if err != nil {
		t.Fatal(err)
	}
	want := []metrics.Sample{{TS: time.UnixMilli(1790877056374), Model: "anthropic/claude-sonnet-4",
		Usage: metrics.Usage{Input: 100, Output: 35, CacheRead: 20, CacheWrite: 7, Calls: 1, MaxContext: 127}}}
	if len(got) != 1 || !got[0].TS.Equal(want[0].TS) || got[0].Model != want[0].Model || got[0].Usage != want[0].Usage {
		t.Fatalf("got %+v, want %+v (output suma reasoning; modelo proveedor/modelo; hora = completed)", got, want)
	}
	if _, ok := cur.Seen["opencode:m1"]; !ok {
		t.Errorf("el id se guarda con prefijo opencode: %v", cur.Seen)
	}
	if st, _ := os.Stat(path); cur.Offsets[path] != st.Size() {
		t.Errorf("el offset llega al final del archivo: %d de %d", cur.Offsets[path], st.Size())
	}
}

func TestReadUsageMismoIDUnaVez(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ses_1.jsonl")
	appendTo(t, path, ul("m1", "ses_1", "", "build", 10, 5, 0, 100, 0, 1000))
	appendTo(t, path, ul("m1", "ses_1", "", "build", 10, 40, 0, 100, 0, 1500)) // mismo mensaje, uso final mayor
	cur := &metrics.Cursor{}
	got, err := Agent{}.ReadUsage(path, cur)
	if err != nil {
		t.Fatal(err)
	}
	if u := total(got); u.Calls != 1 || u.Output != 40 || u.Input != 10 || u.CacheRead != 100 {
		t.Fatalf("una llamada con el uso más alto visto: %+v", u)
	}
	// Otra copia igual y un id distinto con el mismo contenido en otra lectura.
	appendTo(t, path, ul("m1", "ses_1", "", "build", 10, 40, 0, 100, 0, 1500))
	appendTo(t, path, ul("m2", "ses_1", "", "build", 1, 2, 0, 0, 0, 2000))
	got, _ = Agent{}.ReadUsage(path, cur)
	if u := total(got); u.Calls != 1 || u.Input != 1 || u.Output != 2 {
		t.Errorf("solo m2 es nuevo: %+v", u)
	}
	if got, _ := (Agent{}).ReadUsage(path, cur); len(got) != 0 {
		t.Errorf("sin nada nuevo no devuelve nada: %+v", got)
	}
}

func TestReadUsageLineaIncompleta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ses_1.jsonl")
	full := ul("m1", "ses_1", "", "build", 3, 4, 0, 0, 0, 1000)
	partial := ul("m2", "ses_1", "", "build", 5, 6, 0, 0, 0, 2000)
	appendTo(t, path, full+partial[:len(partial)/2]) // se está escribiendo
	cur := &metrics.Cursor{}
	got, _ := Agent{}.ReadUsage(path, cur)
	if u := total(got); u.Calls != 1 || u.Input != 3 {
		t.Fatalf("solo la línea completa: %+v", u)
	}
	if cur.Offsets[path] != int64(len(full)) {
		t.Errorf("el offset se queda al final de la última línea completa: %d, want %d", cur.Offsets[path], len(full))
	}
	appendTo(t, path, partial[len(partial)/2:])
	got, _ = Agent{}.ReadUsage(path, cur)
	if u := total(got); u.Calls != 1 || u.Input != 5 || u.Output != 6 {
		t.Errorf("al completarse se cuenta: %+v", u)
	}
	if got, err := (Agent{}).ReadUsage(filepath.Join(t.TempDir(), "no-existe.jsonl"), &metrics.Cursor{}); err != nil || len(got) != 0 {
		t.Errorf("un archivo que no existe no es error: %v %v", got, err)
	}
}

// La captura real (testdata/events.jsonl) pasada por lo mismo que hace el plugin:
// cada message.updated de asistente terminado → una línea de uso.
func TestReadUsageCaptura(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	path := filepath.Join(t.TempDir(), "ses_cap.jsonl")
	ids := map[string]bool{}
	var wantIn, wantOut, wantCR int64
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var e struct {
			Type       string `json:"type"`
			Properties struct {
				Info struct {
					ID         string `json:"id"`
					SessionID  string `json:"sessionID"`
					Role       string `json:"role"`
					Agent      string `json:"agent"`
					ProviderID string `json:"providerID"`
					ModelID    string `json:"modelID"`
					Time       struct {
						Created   int64 `json:"created"`
						Completed int64 `json:"completed"`
					} `json:"time"`
					Tokens struct {
						Input, Output, Reasoning int64
						Cache                    struct{ Read, Write int64 }
					} `json:"tokens"`
				} `json:"info"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		i := e.Properties.Info
		if e.Type != "message.updated" || i.Role != "assistant" || i.Time.Completed == 0 {
			continue
		}
		var l usageLine
		l.ID, l.SessionID, l.Agent, l.ProviderID, l.ModelID = i.ID, i.SessionID, i.Agent, i.ProviderID, i.ModelID
		l.Created, l.Completed = i.Time.Created, i.Time.Completed
		l.Tokens.Input, l.Tokens.Output, l.Tokens.Reasoning = i.Tokens.Input, i.Tokens.Output, i.Tokens.Reasoning
		l.Tokens.Cache.Read, l.Tokens.Cache.Write = i.Tokens.Cache.Read, i.Tokens.Cache.Write
		b, _ := json.Marshal(l)
		appendTo(t, path, string(b)+"\n")
		if !ids[i.ID] {
			ids[i.ID] = true
			wantIn += i.Tokens.Input
			wantOut += i.Tokens.Output + i.Tokens.Reasoning
			wantCR += i.Tokens.Cache.Read
		}
	}
	if len(ids) < 3 || wantIn == 0 {
		t.Fatalf("la captura debe traer mensajes de asistente con tokens: %d", len(ids))
	}
	got, err := Agent{}.ReadUsage(path, &metrics.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if u := total(got); u.Calls != int64(len(ids)) || u.Input != wantIn || u.Output != wantOut || u.CacheRead != wantCR {
		t.Errorf("sobre la captura: %+v, want %d llamadas, entrada %d, salida %d, caché %d", u, len(ids), wantIn, wantOut, wantCR)
	}
	for _, s := range got {
		if s.Model != "mock/mock" || s.TS.IsZero() {
			t.Errorf("modelo y hora de la captura: %+v", s)
		}
	}
}

func TestPruneViejosLeidos(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-8 * 24 * time.Hour)
	mk := func(name string, age time.Time, body string) string {
		p := filepath.Join(dir, name)
		appendTo(t, p, body)
		if err := os.Chtimes(p, age, age); err != nil {
			t.Fatal(err)
		}
		return p
	}
	line := ul("m", "s", "", "", 1, 1, 0, 0, 0, 1)
	viejoLeido := mk("viejo-leido.jsonl", old, line)
	viejoSinLeer := mk("viejo-sin-leer.jsonl", old, line+line)
	viejoNuncaVisto := mk("viejo-nunca-visto.jsonl", old, line)
	recienteLeido := mk("reciente-leido.jsonl", time.Now(), line)
	otro := mk("notas.txt", old, "x")
	cur := &metrics.Cursor{Offsets: map[string]int64{
		viejoLeido: int64(len(line)), viejoSinLeer: int64(len(line)), recienteLeido: int64(len(line)), otro: 1}}

	Agent{}.Prune(dir, cur, 7*24*time.Hour)

	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if exists(viejoLeido) {
		t.Errorf("leído por completo y sin cambios en 7 días: se borra")
	}
	if _, ok := cur.Offsets[viejoLeido]; ok {
		t.Errorf("y su offset también")
	}
	for name, p := range map[string]string{"viejo sin leer del todo": viejoSinLeer, "viejo que nunca se leyó": viejoNuncaVisto,
		"reciente": recienteLeido, "que no es jsonl": otro} {
		if !exists(p) {
			t.Errorf("%s no se borra", name)
		}
	}
	if _, ok := cur.Offsets[recienteLeido]; !ok {
		t.Errorf("el offset de lo que se queda se conserva")
	}
}
