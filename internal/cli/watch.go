package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/store"
)

func init() {
	Register(&Command{Name: "watch", Summary: "panel en vivo de la tarea: paso actual, siguiente, tiempos, tokens por agente y eventos: watch [--interval 2s] [--once]",
		Setup: func(fs *flag.FlagSet) {
			fs.Duration("interval", 2*time.Second, "cada cuánto refrescar")
			fs.Bool("once", false, "dibujar una vez y salir")
			fs.Bool("open", false, "abrir el panel en otra ventana o pestaña (si no hay uno abierto)")
		},
		Run: runWatch})
}

// El panel es para una persona en otra ventana: ninguna sesión lo lee, así que
// puede ser tan detallado como ayude a seguir la tarea.

const watchEvents = 8

// watchData es lo que el panel muestra, ya leído.
type watchData struct {
	Repo   string
	Active *engine.View
	Stats  metrics.TaskStats
	Events []store.Entry // de la tarea activa, en orden del log
	Others []engine.View
}

func runWatch(c *Ctx) output.Envelope {
	interval := c.Flags.Lookup("interval").Value.(flag.Getter).Get().(time.Duration)
	once := str(c.Flags, "once") == "true"
	if str(c.Flags, "open") == "true" {
		return runWatchOpen(c)
	}
	m := bannerFor(c)
	if once {
		d, err := readWatch(c)
		if err != nil {
			return fail(err)
		}
		env := output.OK("watch", map[string]any{}, nil)
		env.Text = renderWatch(m, c.Version, d, time.Now(), 0)
		return env
	}
	f, _ := c.Stdout.(*os.File)
	enableVT(f)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	defer signal.Stop(stop)
	fmt.Fprint(c.Stdout, "\x1b[?25l\x1b[2J") // sin cursor; se limpia una vez y después se repinta encima
	defer fmt.Fprint(c.Stdout, "\x1b[?25h\n")
	var storeDir string
	if e, err := engineFor(c); err == nil {
		storeDir = e.Store.Dir()
		defer os.Remove(alivePath(storeDir)) // al cerrar la ventana no corre: el latido envejece solo
	}
	for {
		frame := ""
		if d, err := readWatch(c); err != nil {
			frame = "bflow watch: " + err.Error()
		} else {
			frame = renderWatch(m, c.Version, d, time.Now(), termRows(f)-1)
		}
		if storeDir != "" {
			beat(storeDir, interval)
		}
		// Cada línea borra lo que quedaba del cuadro anterior; así no parpadea.
		fmt.Fprint(c.Stdout, "\x1b[H"+strings.ReplaceAll(frame, "\n", "\x1b[K\n")+"\x1b[K\x1b[J")
		select {
		case <-stop:
			return output.Envelope{OK: true, Code: "watch", Quiet: true}
		case <-time.After(interval):
		}
	}
}

func readWatch(c *Ctx) (watchData, error) {
	e, err := engineFor(c)
	if err != nil {
		return watchData{}, err
	}
	ctx := context.Background()
	views, err := e.Views(ctx)
	if err != nil {
		return watchData{}, err
	}
	d := watchData{Repo: filepath.Base(e.Cfg.Root)}
	active, _ := e.Active(ctx)
	for i := range views {
		if views[i].ID == active {
			d.Active = &views[i]
		} else {
			d.Others = append(d.Others, views[i])
		}
	}
	if d.Active == nil {
		return d, nil
	}
	log, err := e.Store.Log(active)
	if err != nil {
		return d, err
	}
	d.Stats = metrics.Compute(active, log, time.Now())
	for _, en := range log {
		if en.Event != "tokens" {
			d.Events = append(d.Events, en)
		}
	}
	return d, nil
}

// renderWatch arma el cuadro. Con rows > 0 lo ajusta a ese alto: primero
// cambia el banner por una línea y después quita los eventos más viejos; si
// el cuadro no cabe, el repintado deja basura en la terminal.
func renderWatch(m bannerMode, version string, d watchData, now time.Time, rows int) string {
	var b, tail strings.Builder
	label := func(l, v string) { dimLabel(&b, m, l, v) }
	label("repo", d.Repo+" · "+now.Format("15:04:05"))
	var events []string
	if d.Active == nil {
		label("tarea", "ninguna en curso · bflow start <ID> --lane full|light|hotfix")
	} else {
		v, st := *d.Active, d.Stats
		task := v.ID
		if v.Lane != "" {
			task += " · " + string(v.Lane)
		}
		if v.Round > 0 {
			task += fmt.Sprintf(" · ronda %d", v.Round)
		}
		label("tarea", task)
		if v.Title != "" {
			label("", truncate(v.Title, 70))
		}
		label("ahora", highlight(m, v, currentStep(v, d.Events, now)))
		if v.Upcoming != "" {
			label("sigue", v.Upcoming)
		}
		t := dur(st.Total) + " · agente " + dur(st.Agent) + " · humano " + dur(st.Human)
		if st.Blocked > 0 {
			t += " · bloqueada " + dur(st.Blocked)
		}
		label("tiempo", t)
		var phases []string
		for _, p := range st.Phases {
			if d := p.Agent + p.Human + p.Blocked; d > 0 {
				phases = append(phases, string(p.Phase)+" "+dur(d))
			}
		}
		if len(phases) > 0 {
			label("", strings.Join(phases, " · "))
		}
		if st.TokensAvailable {
			label("tokens", metrics.Tokens(st.Tokens.New(), st.Tokens.CacheRead))
			for _, a := range byNew(st.Agents) {
				u := st.Agents[a]
				label("", fmt.Sprintf("%-18s %s", agentName(a), metrics.Tokens(u.New(), u.CacheRead)))
			}
		}
		if n := st.Refused + st.Guarded + st.Nudged; n > 0 {
			label("fricción", fmt.Sprintf("%d rechazo(s) del flujo · %d bloqueo(s) de guard · %d fin(es) sin reporte", st.Refused, st.Guarded, st.Nudged))
		}
		if len(d.Events) > 0 {
			evs := d.Events[max(0, len(d.Events)-watchEvents):]
			w := 5 // hora; con fecha si alguno no es de hoy
			if !sameDay(evs[0].TS, now) {
				w = 11
			}
			for i := len(evs) - 1; i >= 0; i-- {
				events = append(events, fmt.Sprintf("  %-*s %s", w, eventTime(evs[i].TS, now), describeEvent(evs[i])))
			}
		}
	}
	watchOthers(&tail, m, d.Others)

	head := ""
	if m != bannerOff {
		head = renderBanner(m, version) + "\n"
	}
	lines := func() int {
		n := strings.Count(head, "\n") + strings.Count(b.String(), "\n") + strings.Count(tail.String(), "\n")
		if len(events) > 0 {
			n += len(events) + 1
		}
		return n
	}
	if rows > 0 && lines() > rows && head != "" {
		head = compactBanner(m, version) + "\n\n"
	}
	for rows > 0 && lines() > rows && len(events) > 0 {
		events = events[:len(events)-1]
	}
	out := head + b.String()
	if len(events) > 0 {
		out += "\n" + strings.Join(events, "\n") + "\n"
	}
	out = strings.TrimRight(out+tail.String(), "\n")
	if all := strings.Split(out, "\n"); rows > 0 && len(all) > rows { // último recurso
		out = strings.Join(all[:rows], "\n")
	}
	return out
}

func watchOthers(b *strings.Builder, m bannerMode, others []engine.View) {
	if len(others) == 0 {
		return
	}
	b.WriteString("\n")
	for i, o := range others {
		l := ""
		if i == 0 {
			l = "otras"
		}
		dimLabel(b, m, l, o.ID+" · "+string(o.Phase)+" · "+nextLabel(o))
	}
}

// currentStep dice quién tiene la tarea y desde cuándo.
func currentStep(v engine.View, evs []store.Entry, now time.Time) string {
	ago := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return " · hace " + dur(max(now.Sub(t), time.Second))
	}
	phase := string(v.Phase)
	switch {
	case v.Blocked != "":
		return "bloqueada: " + v.Blocked + ago(since(v, evs))
	case v.Gate != "":
		t := v.GateSince
		if t.IsZero() {
			t = since(v, evs)
		}
		return phase + " · esperando tu decisión: gate " + v.Gate + ago(t)
	case v.Next.Action == output.ActionSpawn:
		var as []string
		for _, a := range v.Next.Agents {
			as = append(as, a.Agent)
		}
		s := phase + " · trabajando: " + strings.Join(as, ", ") + ago(since(v, evs))
		if len(v.Reported) > 0 {
			s += " · listos: " + strings.Join(v.Reported, ", ")
		}
		return s
	case v.Phase == flow.InReview:
		return phase + " · esperando el merge del PR" + ago(since(v, evs))
	}
	if v.Next.Reason != "" {
		return phase + " · " + v.Next.Reason
	}
	return phase + " · " + nextLabel(v)
}

// since es cuándo empezó el paso actual: la entrada a la fase o, si después
// se resolvió una gate sin cambiar de fase (una decisión), esa resolución.
func since(v engine.View, evs []store.Entry) time.Time {
	var t time.Time
	if v.Since != nil {
		t = *v.Since
	}
	for _, e := range evs {
		if (e.Event == "approve" || e.Event == "unblock" || e.Event == "block") && e.TS.After(t) {
			t = e.TS
		}
	}
	return t
}

// highlight resalta lo que pide a la persona actuar.
func highlight(m bannerMode, v engine.View, s string) string {
	if m != bannerColor || (v.Gate == "" && v.Blocked == "") {
		return s
	}
	return "\x1b[1;33m" + s + "\x1b[0m"
}

func sameDay(a, b time.Time) bool {
	a, b = a.Local(), b.Local()
	return a.YearDay() == b.YearDay() && a.Year() == b.Year()
}

func eventTime(ts, now time.Time) string {
	if sameDay(ts, now) {
		return ts.Local().Format("15:04")
	}
	return ts.Local().Format("02/01 15:04")
}

// describeEvent resume un evento del log en una línea.
func describeEvent(e store.Entry) string {
	arrow := ""
	if e.To != "" && e.To != e.From {
		arrow = " → " + string(e.To)
	}
	if g, ok := e.Data["gate_opened"].(string); ok {
		arrow += " · gate " + g
	}
	note := ""
	if n := strings.TrimSpace(e.Note); n != "" {
		note = ": " + truncate(n, 60)
	}
	switch e.Event {
	case "start":
		return fmt.Sprintf("inicio en carril %v", e.Data["lane"]) + arrow
	case "report":
		return e.Agent + " " + e.Verdict + arrow
	case "approve":
		return "aprobada gate " + e.Gate + arrow
	case "reject":
		return "rechazada gate " + e.Gate + note + arrow
	case "block":
		return "bloqueada" + note
	case "unblock":
		return "desbloqueada" + arrow
	case "guard":
		return fmt.Sprintf("guard bloqueó: %v", e.Data["rule"])
	case "refused":
		return fmt.Sprintf("el flujo rechazó %v (%v)", e.Data["event"], e.Data["code"])
	case "nudge":
		return e.Agent + " terminó sin reportar"
	}
	return e.Event + note + arrow
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func agentName(a string) string {
	switch a {
	case metrics.MainSession:
		return "sesión principal"
	case "":
		return "sin desglose"
	}
	return a
}

// byNew ordena las claves de mayor a menor gasto nuevo.
func byNew(m map[string]metrics.Usage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(x, y string) int {
		if d := m[y].New() - m[x].New(); d != 0 {
			return int(max(-1, min(1, d)))
		}
		return strings.Compare(x, y)
	})
	return keys
}

func runWatchOpen(c *Ctx) output.Envelope {
	e, err := engineFor(c)
	if err != nil {
		return output.Fail("config", err)
	}
	st, err := openWatch(e.Cfg.Root, e.Store.Dir())
	if err != nil {
		return output.Fail("no_terminal", fmt.Errorf("no se pudo abrir el panel: %w; córrelo a mano en otra terminal: bflow watch", err))
	}
	env := output.OK("watch", map[string]any{"watch": st}, nil)
	env.Text = "panel abierto en otra ventana"
	if st == "already" {
		env.Text = "el panel ya está abierto"
	}
	return env
}
