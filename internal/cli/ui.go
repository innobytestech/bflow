package cli

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/output"
)

// bflow ui es el panel de bflow watch en el navegador: una página local, de
// solo lectura, que pregunta el estado cada pocos segundos. Escucha solo en
// 127.0.0.1 y rechaza cualquier otro Host (una página ajena no puede leerla
// por DNS rebinding). No usa nada de internet: la página va en el binario.

//go:embed ui/index.html
var uiPage []byte

const uiDefaultPort = 7719

func init() {
	Register(&Command{Name: "ui", Summary: "el panel en el navegador: ui [--port 7719] [--no-open]",
		Setup: func(fs *flag.FlagSet) {
			fs.Int("port", uiDefaultPort, "puerto local (si está ocupado se usa otro)")
			fs.Bool("no-open", false, "no abrir el navegador")
		},
		Run: runUI})
}

// panelStep es AHORA o DESPUÉS: quién actúa y qué hace.
type panelStep struct {
	Human bool       `json:"human"`
	Text  string     `json:"text"`
	Since *time.Time `json:"since,omitempty"`
}

type panelTask struct {
	ID       string              `json:"id"`
	Title    string              `json:"title,omitempty"`
	Lane     string              `json:"lane,omitempty"`
	Round    int                 `json:"round,omitempty"`
	Phases   []map[string]string `json:"phases"`  // name, state: done|current|pending
	Waiting  bool                `json:"waiting"` // la tarea espera a la persona
	Now      *panelStep          `json:"now,omitempty"`
	Next     *panelStep          `json:"next,omitempty"`
	Time     map[string]string   `json:"time"`
	ByPhase  []map[string]string `json:"by_phase,omitempty"`
	Tokens   string              `json:"tokens,omitempty"`
	Agents   []map[string]string `json:"agents,omitempty"`
	Friction string              `json:"friction,omitempty"`
	Events   []map[string]string `json:"events,omitempty"`
}

type panelState struct {
	Repo    string              `json:"repo"`
	Version string              `json:"version"`
	Now     time.Time           `json:"now"`
	Task    *panelTask          `json:"task,omitempty"`
	Others  []map[string]string `json:"others,omitempty"`
}

// buildPanel arma lo que muestra la página, con los mismos textos que watch.
func buildPanel(version string, d watchData, now time.Time) panelState {
	ps := panelState{Repo: d.Repo, Version: version, Now: now}
	for _, o := range d.Others {
		ps.Others = append(ps.Others, map[string]string{"id": o.ID, "phase": string(o.Phase), "next": nextLabel(o)})
	}
	if d.Active == nil {
		return ps
	}
	v, st := *d.Active, d.Stats
	t := &panelTask{ID: v.ID, Title: v.Title, Lane: string(v.Lane), Round: v.Round, Waiting: v.Gate != "" || v.Blocked != ""}
	cur := slices.Index(v.Phases, v.Phase)
	for i, p := range v.Phases {
		if p == flow.Done {
			continue
		}
		state := "pending"
		switch {
		case i == cur:
			state = "current"
		case cur >= 0 && i < cur:
			state = "done"
		}
		t.Phases = append(t.Phases, map[string]string{"name": string(p), "state": state})
	}
	if s, ok := nowStep(v, d.Events); ok {
		t.Now = &panelStep{Human: s.human, Text: s.text}
		if !s.since.IsZero() {
			t.Now.Since = &s.since
		}
	}
	if s, ok := nextStep(v); ok {
		t.Next = &panelStep{Human: s.human, Text: s.text}
	}
	t.Time = map[string]string{"total": dur(st.Total), "agents": dur(st.Agent), "human": dur(st.Human)}
	if st.Blocked > 0 {
		t.Time["blocked"] = dur(st.Blocked)
	}
	for _, p := range st.Phases {
		if x := p.Agent + p.Human + p.Blocked; x > 0 {
			t.ByPhase = append(t.ByPhase, map[string]string{"phase": string(p.Phase), "time": dur(x)})
		}
	}
	if st.TokensAvailable {
		t.Tokens = metrics.Tokens(st.Tokens.New(), st.Tokens.CacheRead)
		for _, a := range byNew(st.Agents) {
			u := st.Agents[a]
			t.Agents = append(t.Agents, map[string]string{"name": agentName(a), "tokens": metrics.Tokens(u.New(), u.CacheRead)})
		}
	}
	if n := st.Refused + st.Guarded + st.Nudged; n > 0 {
		t.Friction = fmt.Sprintf("%d rechazo(s) del flujo · %d bloqueo(s) de guard · %d fin(es) sin reporte", st.Refused, st.Guarded, st.Nudged)
	}
	evs := d.Events[max(0, len(d.Events)-2*watchEvents):]
	for i := len(evs) - 1; i >= 0; i-- {
		t.Events = append(t.Events, map[string]string{"time": eventTime(evs[i].TS, now), "text": describeEvent(evs[i])})
	}
	ps.Task = t
	return ps
}

// uiHandler sirve la página y el estado. allowed son los Host aceptados.
func uiHandler(allowed []string, state func() (panelState, error)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'")
		w.Write(uiPage)
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		ps, err := state()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(ps)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(allowed, r.Host) {
			http.Error(w, "host no permitido", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// serveUI levanta la página en 127.0.0.1 (en port o, si está ocupado, en
// otro) y devuelve su dirección y cómo cerrarla.
func serveUI(c *Ctx, port int) (string, func(), error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0") // ocupado: otro puerto
	}
	if err != nil {
		return "", nil, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	allowed := []string{fmt.Sprintf("127.0.0.1:%d", addr.Port), fmt.Sprintf("localhost:%d", addr.Port)}
	srv := &http.Server{Handler: uiHandler(allowed, func() (panelState, error) {
		d, err := readWatch(c)
		return buildPanel(c.Version, d, time.Now()), err
	}), ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return fmt.Sprintf("http://127.0.0.1:%d/", addr.Port), stop, nil
}

func runUI(c *Ctx) output.Envelope {
	if _, err := engineFor(c); err != nil {
		return output.Fail("config", err)
	}
	port, _ := strconv.Atoi(str(c.Flags, "port"))
	url, stopUI, err := serveUI(c, port)
	if err != nil {
		return fail(err)
	}
	defer stopUI()
	fmt.Fprintln(c.Stdout, "bflow ui en "+url+" (Ctrl+C para cerrar)")
	if str(c.Flags, "no-open") != "true" {
		if err := openBrowser(url); err != nil {
			fmt.Fprintln(c.Stdout, "abre esa dirección en tu navegador ("+err.Error()+")")
		}
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	return output.Envelope{OK: true, Code: "ui", Quiet: true}
}

// browserCmd abre url en el navegador del sistema.
func browserCmd(goos string, getenv func(string) string, url string) ([]string, error) {
	if getenv("CI") != "" {
		return nil, fmt.Errorf("%w: CI", errNoDesktop)
	}
	switch goos {
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", url}, nil
	case "darwin":
		return []string{"open", url}, nil
	}
	if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
		return nil, fmt.Errorf("%w: no hay DISPLAY ni WAYLAND_DISPLAY", errNoDesktop)
	}
	return []string{"xdg-open", url}, nil
}

func openBrowser(url string) error {
	args, err := browserCmd(goos, os.Getenv, url)
	if err != nil {
		return err
	}
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return errors.New(strings.TrimSpace(err.Error()))
	}
	return cmd.Process.Release()
}
