package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/store"
)

func init() {
	Register(&Command{Name: "stats", Summary: "tiempo por fase (agente, humano, bloqueada), iteraciones y tokens: stats [ID]", Run: runStats})
	Register(&Command{Name: "statusline", Summary: "una línea para la barra de estado (lee una caché: milisegundos)", Run: runStatusline})
	Register(&Command{Name: "watch", Summary: "panel en la terminal que se refresca solo: watch [--interval 5s] [--once]",
		Setup: func(fs *flag.FlagSet) {
			fs.Duration("interval", 5*time.Second, "cada cuánto refrescar")
			fs.Bool("once", false, "dibujar una vez y salir")
		},
		Run: runWatch})
	Register(&Command{Name: "hook tokens", Summary: "hook Stop/SubagentStop: suma los tokens nuevos del transcript a la fase activa", Run: runHookTokens})
}

func runStats(c *Ctx) output.Envelope {
	e, err := engineFor(c)
	if err != nil {
		return output.Fail("config", err)
	}
	log, err := e.Store.Log("")
	if err != nil {
		return fail(err)
	}
	now := time.Now()
	if len(c.Args) > 0 {
		id := strings.ToUpper(c.Args[0])
		st := metrics.Compute(id, log, now)
		if st.Phase == "" {
			return output.Fail("not_found", fmt.Errorf("%s no tiene historial en .bflow/log.jsonl", id))
		}
		env := output.OK("stats", map[string]any{"stats": st}, nil)
		env.Text = renderStats(st)
		return env
	}
	recs, err := e.Store.List()
	if err != nil {
		return fail(err)
	}
	var all []metrics.TaskStats
	var b strings.Builder
	for _, r := range recs {
		st := metrics.Compute(r.Flow.ID, log, now)
		all = append(all, st)
		fmt.Fprintf(&b, "%s\n", statsLine(st))
	}
	env := output.OK("stats", map[string]any{"tasks": all}, nil)
	env.Text = strings.TrimRight(b.String(), "\n")
	if env.Text == "" {
		env.Text = "sin historial todavía"
	}
	return env
}

func dur(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	return metrics.Duration(d)
}

func statsLine(st metrics.TaskStats) string {
	l := fmt.Sprintf("%s · %s · %s (agente %s · humano %s", st.ID, st.Phase, dur(st.Total), dur(st.Agent), dur(st.Human))
	if st.Blocked > 0 {
		l += " · bloqueada " + dur(st.Blocked)
	}
	l += ")"
	var it []string
	for _, x := range []struct {
		n    int
		name string
	}{{st.Rejections, "rechazo(s)"}, {st.Rounds, "ronda(s)"}, {st.Decisions, "decisión(es)"}, {st.Splits, "división(es)"},
		{len(st.Hotfixes), "hotfix(es)"}, {st.Refused + st.Guarded + st.Nudged, "fricción"}} {
		if x.n > 0 {
			it = append(it, fmt.Sprintf("%d %s", x.n, x.name))
		}
	}
	if len(it) > 0 {
		l += " · " + strings.Join(it, ", ")
	}
	if st.TokensAvailable {
		l += " · " + metrics.Tokens(st.Tokens.New(), st.Tokens.CacheRead)
	}
	return l
}

func renderStats(st metrics.TaskStats) string {
	var b strings.Builder
	b.WriteString(statsLine(st) + "\n")
	row := "  %-13s %8s %8s %8s %8s %8s\n"
	fmt.Fprintf(&b, row, "fase", "agente", "humano", "bloq.", "nuevos", "caché")
	for _, p := range st.Phases {
		fmt.Fprintf(&b, row, p.Phase, dur(p.Agent), dur(p.Human), dur(p.Blocked), tok(p.Tokens.New()), tok(p.Tokens.CacheRead))
	}
	if len(st.RejectionsByGate) > 0 {
		var gs []string
		for _, g := range slices.Sorted(maps.Keys(st.RejectionsByGate)) {
			gs = append(gs, fmt.Sprintf("%s %d", g, st.RejectionsByGate[g]))
		}
		b.WriteString("  rechazos por gate: " + strings.Join(gs, " · ") + "\n")
	}
	if len(st.Hotfixes) > 0 {
		b.WriteString("  hotfixes: " + strings.Join(st.Hotfixes, ", ") + "\n")
	}
	if st.Refused+st.Guarded+st.Nudged > 0 {
		fmt.Fprintf(&b, "  fricción: %d pedido(s) rechazado(s) por el flujo · %d bloqueo(s) de guard · %d fin(es) sin reporte\n", st.Refused, st.Guarded, st.Nudged)
	}
	usageTable(&b, "por agente", st.Agents, map[string]string{metrics.MainSession: "sesión principal", "": "sin desglose"})
	usageTable(&b, "por modelo", st.Models, map[string]string{"": "sin modelo"})
	if st.TokensAvailable {
		t := st.Tokens
		fmt.Fprintf(&b, "  tokens: %s (entrada %s · salida %s · caché escrita %s)", metrics.Tokens(t.New(), t.CacheRead),
			metrics.Human(t.Input), metrics.Human(t.Output), metrics.Human(t.CacheWrite))
	} else {
		b.WriteString("  tokens: no disponibles (se registran con el hook de tokens del agente)")
	}
	return b.String()
}

func tok(n int64) string {
	if n <= 0 {
		return "-"
	}
	return metrics.Human(n)
}

// usageTable desglosa tokens por clave, de mayor a menor gasto nuevo. Se omite
// si solo hay registros viejos sin esa clave.
func usageTable(b *strings.Builder, title string, m map[string]metrics.Usage, names map[string]string) {
	if _, legacy := m[""]; len(m) == 0 || (len(m) == 1 && legacy) {
		return
	}
	keys := slices.SortedFunc(maps.Keys(m), func(x, y string) int {
		if d := m[y].New() - m[x].New(); d != 0 {
			return int(max(-1, min(1, d)))
		}
		return strings.Compare(x, y)
	})
	row := "  %-22s %8s %8s\n"
	fmt.Fprintf(b, row, title, "nuevos", "caché")
	for _, k := range keys {
		name := k
		if n, ok := names[k]; ok {
			name = n
		}
		fmt.Fprintf(b, row, name, tok(m[k].New()), tok(m[k].CacheRead))
	}
}

// runStatusline no carga config ni toca git ni el tracker: solo lee la caché.
func runStatusline(c *Ctx) output.Envelope {
	dir := c.Dir
	if c.Stdin != nil {
		if f, ok := c.Stdin.(*os.File); !ok || !isTerminal(f) {
			raw, _ := io.ReadAll(io.LimitReader(c.Stdin, 1<<16))
			var in struct {
				Cwd       string `json:"cwd"`
				Workspace struct {
					CurrentDir string `json:"current_dir"`
				} `json:"workspace"`
			}
			if len(raw) > 0 && json.Unmarshal(raw, &in) == nil {
				if in.Workspace.CurrentDir != "" {
					dir = in.Workspace.CurrentDir
				} else if in.Cwd != "" {
					dir = in.Cwd
				}
			}
		}
	}
	sc := engine.ReadStatusCache(config.FindRoot(dir))
	env := output.OK("statusline", map[string]any{}, nil)
	if sc == nil {
		env.Text = "bflow"
		return env
	}
	env.Data["status"] = sc
	env.Text = StatuslineText(*sc, time.Now())
	return env
}

// StatuslineText arma la línea: API-171 · implementing · 1h42m · ronda 1 · 145k nuevos · 2.9M caché
func StatuslineText(sc engine.StatusCache, now time.Time) string {
	parts := []string{sc.ID, string(sc.Phase)}
	if sc.Gate != "" {
		parts[1] += " (" + sc.Gate + ")"
	}
	if !sc.Since.IsZero() && sc.Phase != flow.Done {
		parts = append(parts, metrics.Duration(now.Sub(sc.Since)))
	}
	if sc.Round > 0 {
		parts = append(parts, fmt.Sprintf("ronda %d", sc.Round))
	}
	if sc.New+sc.Cached > 0 {
		parts = append(parts, metrics.Tokens(sc.New, sc.Cached))
	}
	return strings.Join(parts, " · ")
}

func runWatch(c *Ctx) output.Envelope {
	interval := c.Flags.Lookup("interval").Value.(flag.Getter).Get().(time.Duration)
	once := str(c.Flags, "once") == "true"
	for {
		frame, err := watchFrame(c)
		if err != nil {
			return fail(err)
		}
		if once {
			env := output.OK("watch", map[string]any{}, nil)
			env.Text = frame
			return env
		}
		fmt.Fprint(c.Stdout, "\x1b[H\x1b[2J"+frame+"\n")
		time.Sleep(interval)
	}
}

func watchFrame(c *Ctx) (string, error) {
	e, err := engineFor(c)
	if err != nil {
		return "", err
	}
	views, err := e.Views(context.Background())
	if err != nil {
		return "", err
	}
	log, _ := e.Store.Log("")
	now := time.Now()
	var b strings.Builder
	fmt.Fprintf(&b, "bflow · %s · %s\n\n", filepath.Base(e.Cfg.Root), now.Format("15:04:05"))
	if len(views) == 0 {
		b.WriteString("sin tareas en curso\n")
	}
	active, _ := e.Active(context.Background())
	for _, v := range views {
		mark := "  "
		if v.ID == active {
			mark = "▶ "
		}
		st := metrics.Compute(v.ID, log, now)
		fmt.Fprintf(&b, "%s%s\n", mark, statsLine(st))
		fmt.Fprintf(&b, "    siguiente: %s\n", nextLabel(v))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// nextLabel resume en pocas palabras lo que sigue en una tarea.
func nextLabel(v engine.View) string {
	if v.Gate != "" {
		return "decidir " + v.Gate
	}
	if len(v.Next.Agents) > 0 {
		var as []string
		for _, a := range v.Next.Agents {
			as = append(as, a.Agent)
		}
		return "agentes: " + strings.Join(as, ", ")
	}
	return v.Next.Action
}

// runHookTokens suma los tokens nuevos de un transcript a la tarea activa. En
// Stop lee solo la sesión principal y reparte cada respuesta en la fase en que
// ocurrió; en SubagentStop lee solo el transcript de ese agente y lo asigna a
// la fase donde trabajó. Nunca falla ni imprime: es un hook.
func runHookTokens(c *Ctx) output.Envelope {
	quiet := output.Envelope{OK: true, Code: "tokens", Quiet: true}
	raw, _ := io.ReadAll(c.Stdin)
	if c.Agent == nil {
		return quiet
	}
	path, agent := c.Agent.TokenSource(raw)
	if path == "" {
		return quiet
	}
	e, err := engineFor(c)
	if err != nil {
		return quiet
	}
	curPath := filepath.Join(e.Store.Dir(), "cache", "tokens-cursor.json")
	_ = store.EnsureDirFor(curPath)
	unlock, err := store.LockFile(curPath+".lock", 5*time.Second, time.Minute)
	if err != nil {
		return quiet
	}
	defer unlock()
	var cur metrics.Cursor
	if b, err := os.ReadFile(curPath); err == nil {
		_ = json.Unmarshal(b, &cur)
	}
	samples, err := c.Agent.ReadUsage(path, &cur)
	if err != nil {
		return quiet
	}
	if len(cur.Seen) > 5000 { // los offsets evitan releer; los ids viejos ya no hacen falta
		cur.Seen = nil
	}
	if b, err := json.Marshal(cur); err == nil {
		_ = store.WriteAtomic(curPath, b)
	}
	if len(samples) == 0 {
		return quiet
	}
	id, err := e.Active(context.Background())
	if err != nil {
		return quiet
	}
	log, err := e.Store.Log(id)
	if err != nil {
		return quiet
	}
	name := metrics.MainSession
	if agent != "" {
		name = strings.TrimPrefix(agent, flow.SubagentPrefix)
	}
	shares := metrics.Allot(log, id, strings.TrimPrefix(agent, flow.SubagentPrefix), samples)
	now := time.Now()
	var entries []store.Entry
	var u metrics.Usage
	for _, s := range shares {
		d := map[string]any{"phase": string(s.Phase), "tool": c.Agent.Name(),
			"input": s.Input, "output": s.Output, "cache_read": s.CacheRead, "cache_write": s.CacheWrite}
		if s.Model != "" {
			d["model"] = s.Model
		}
		entries = append(entries, store.Entry{TS: now, ID: id, Event: "tokens", Agent: name, By: e.User, Data: d})
		u.Add(s.Usage)
	}
	_ = e.Store.Append(entries...)
	e.AddTokens(id, u.New(), u.CacheRead)
	quiet.Data = map[string]any{"id": id, "agent": name, "tokens": u}
	return quiet
}
