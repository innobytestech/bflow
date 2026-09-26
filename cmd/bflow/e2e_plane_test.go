package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// planeFake es un Plane mínimo: un proyecto, los estados del BE y una tarea.
func planeFake(t *testing.T) (*httptest.Server, *sync.Map) {
	patched := &sync.Map{}
	states := []map[string]any{{"id": "s1", "name": "Backlog", "group": "backlog"}, {"id": "s2", "name": "Discovery", "group": "unstarted"}}
	item := map[string]any{"id": "wi-1", "sequence_id": 120, "name": "Validación por campo", "description_stripped": "x", "state": "s1", "start_date": nil, "target_date": "2026-10-01", "updated_at": "2026-09-20T10:00:00Z"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "key" {
			w.WriteHeader(401)
			return
		}
		p := r.URL.Path
		page := func(rows any) { json.NewEncoder(w).Encode(map[string]any{"results": rows, "next_page_results": false}) }
		switch {
		case strings.HasSuffix(p, "/projects/"):
			page([]map[string]any{{"id": "p-uuid", "identifier": "API", "name": "acme-api"}})
		case strings.HasSuffix(p, "/states/"):
			page(states)
		case strings.HasSuffix(p, "/work-items/API-120/"):
			json.NewEncoder(w).Encode(item)
		case r.Method == "PATCH":
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			patched.Store(p, in["state"])
			w.Write([]byte("{}"))
		case strings.HasSuffix(p, "/work-items/"):
			page([]map[string]any{item})
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"detail":"Not found."}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, patched
}

func TestPlaneWiring(t *testing.T) {
	srv, patched := planeFake(t)
	r := newRepo(t)
	t.Setenv("BFLOW_NO_KEYRING", "1")
	t.Setenv("BFLOW_PLANE_TOKEN", "key")
	os.WriteFile(filepath.Join(r.dir, "bflow.yaml"), []byte("tracker: { adapter: plane, url: "+srv.URL+", workspace: acme-dev, project: API }\n"), 0o644)

	st := r.ok("status", "API-120")
	task := st.Data["task"].(map[string]any)
	if task["title"] != "Validación por campo" || task["phase"] != "backlog" || !strings.Contains(task["url"].(string), "/acme-dev/browse/API-120/") {
		t.Fatalf("status: %v", task)
	}
	r.ok("start", "API-120", "--lane", "full")
	if v, _ := patched.Load("/api/v1/workspaces/acme-dev/projects/p-uuid/work-items/wi-1/"); v != "s2" {
		t.Errorf("start debe mover la tarea a Discovery en Plane: %v", v)
	}
	plan := r.ok("tracker", "setup", "--dry-run")
	if created, _ := plan.Data["created"].([]any); len(created) != 10 {
		t.Errorf("faltan 10 estados en el fake: %v", plan.Data)
	}
	if _, err := os.Stat(filepath.Join(r.dir, ".bflow", "cache", "plane.json")); err != nil {
		t.Error("la resolución del proyecto debe quedar en caché")
	}
	t.Setenv("BFLOW_PLANE_TOKEN", "")
	// status de una tarea empezada sale de .bflow sin llamar a Plane; panel sí consulta.
	if st := r.ok("status", "API-120"); st.Data["task"].(map[string]any)["phase"] != "discovery" {
		t.Errorf("status offline: %v", st.Data)
	}
	env := r.ok("panel")
	if !strings.Contains(fmt.Sprint(env.Data["warnings"]), "bflow connect plane") {
		t.Errorf("panel sin token debe avisar: %v", env.Data)
	}
}
