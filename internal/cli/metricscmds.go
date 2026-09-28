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
		l += " · " + metrics.Human(st.Tokens.Total()) + " tok"
	}
	return l
}

func renderStats(st metrics.TaskStats) string {
	var b strings.Builder
	b.WriteString(statsLine(st) + "\n")
	fmt.Fprintf(&b, "  %-13s %8s %8s %8s %8s\n", "fase", "agente", "humano", "bloq.", "tokens")
	for _, p := range st.Phases {
		tok := "-"
		if p.Tokens.Total() > 0 {
			tok = metrics.Human(p.Tokens.Total())
		}
		fmt.Fprintf(&b, "  %-13s %8s %8s %8s %8s\n", p.Phase, dur(p.Agent), dur(p.Human), dur(p.Blocked), tok)
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
	if _, unknown := st.Models[""]; len(st.Models) > 1 || (len(st.Models) == 1 && !unknown) {
		var ms []string
		for _, m := range slices.Sorted(maps.Keys(st.Models)) {
			name := m
			if name == "" {
				name = "sin modelo"
			}
			ms = append(ms, name+" "+metrics.Human(st.Models[m].Total()))
		}
		b.WriteString("  por modelo: " + strings.Join(ms, " · ") + "\n")
	}
	if st.TokensAvailable {
		t := st.Tokens
		fmt.Fprintf(&b, "  tokens: %s (entrada %s · salida %s · caché leída %s · caché escrita %s)", metrics.Human(t.Total()),
			metrics.Human(t.Input), metrics.Human(t.Output), metrics.Human(t.CacheRead), metrics.Human(t.CacheWrite))
	} else {
		b.WriteString("  tokens: no disponibles (se registran con el hook de tokens del agente)")
	}
	return b.String()
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

// StatuslineText arma la línea: API-171 · implementing · 1h42m · ronda 1 · 184k tok
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
	if sc.Tokens > 0 {
		parts = append(parts, metrics.Human(sc.Tokens)+" tok")
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
		next := v.Next.Action
		if v.Gate != "" {
			next = "decidir " + v.Gate
		} else if len(v.Next.Agents) > 0 {
			var as []string
			for _, a := range v.Next.Agents {
				as = append(as, a.Agent)
			}
			next = "agentes: " + strings.Join(as, ", ")
		}
		fmt.Fprintf(&b, "    siguiente: %s\n", next)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// runHookTokens suma los tokens nuevos del transcript y los atribuye a la
// fase de la tarea activa. Nunca falla ni imprime: es un hook.
func runHookTokens(c *Ctx) output.Envelope {
	quiet := output.Envelope{OK: true, Code: "tokens", Quiet: true}
	raw, _ := io.ReadAll(c.Stdin)
	if c.Agent == nil {
		return quiet
	}
	path := c.Agent.TranscriptPath(raw)
	if path == "" {
		return quiet
	}
	e, err := engineFor(c)
	if err != nil {
		return quiet
	}
	curPath := filepath.Join(e.Store.Dir(), "cache", "tokens-cursor.json")
	_ = os.MkdirAll(filepath.Dir(curPath), 0o755)
	unlock, err := store.LockFile(curPath+".lock", 5*time.Second, time.Minute)
	if err != nil {
		return quiet
	}
	defer unlock()
	var cur metrics.Cursor
	if b, err := os.ReadFile(curPath); err == nil {
		_ = json.Unmarshal(b, &cur)
	}
	byModel, err := c.Agent.ReadUsage(path, &cur)
	if err != nil {
		return quiet
	}
	var u metrics.Usage
	for _, m := range byModel {
		u.Add(m)
	}
	if len(cur.Seen) > 5000 { // los offsets evitan releer; los ids viejos ya no hacen falta
		cur.Seen = nil
	}
	if b, err := json.Marshal(cur); err == nil {
		_ = store.WriteAtomic(curPath, b)
	}
	if u.Total() == 0 {
		return quiet
	}
	id, err := e.Active(context.Background())
	if err != nil {
		return quiet
	}
	rec, err := e.Store.Load(id)
	if err != nil {
		return quiet
	}
	now := time.Now()
	var entries []store.Entry
	for _, model := range slices.Sorted(maps.Keys(byModel)) {
		m := byModel[model]
		d := map[string]any{"phase": string(rec.Flow.Phase), "tool": c.Agent.Name(),
			"input": m.Input, "output": m.Output, "cache_read": m.CacheRead, "cache_write": m.CacheWrite}
		if model != "" {
			d["model"] = model
		}
		entries = append(entries, store.Entry{TS: now, ID: id, Event: "tokens", By: e.User, Data: d})
	}
	_ = e.Store.Append(entries...)
	e.AddTokens(id, u.Total())
	quiet.Data = map[string]any{"id": id, "tokens": u}
	return quiet
}
