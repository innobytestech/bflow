package eval

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"innobytes.tech/bflow/internal/store"
)

func startFix(id, fixes string) store.Entry {
	d := map[string]any{"lane": "hotfix"}
	if fixes != "" {
		d["fixes"] = fixes
	}
	return store.Entry{TS: time.Now(), ID: id, Event: "start", Data: d}
}

func TestFromFixesSkeletons(t *testing.T) {
	h := newHarness(t)
	h.write(".bflow/tasks/GH-159/state.json", `{"schema":1,"start_sha":"aaaa111"}`)
	h.write(".bflow/tasks/GH-159/check.json", `{"sha":"bbbb222","result":"PASS"}`)
	log := []store.Entry{
		startFix("GH-160", "GH-159"),
		startFix("GH-161", ""),          // un start sin fixes no cuenta
		{ID: "GH-159", Event: "report"}, // otro evento
		startFix("GH-170", "GH-300"),    // X sin state ni check: base y head vacíos
	}
	created, warnings, err := FromFixes(h.root, log)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(created)
	if len(created) != 2 || !strings.HasSuffix(created[0], "gh-159-fix-gh-160") || !strings.HasSuffix(created[1], "gh-300-fix-gh-170") {
		t.Fatalf("creados: %v (avisos %v)", created, warnings)
	}
	raw, err := os.ReadFile(filepath.Join(h.root, ".bflow", "evals", "reviewer", "gh-159-fix-gh-160", "case.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var c Case
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if !c.Draft || c.Source != "GH-159" || c.Hotfix != "GH-160" || c.Base != "aaaa111" || c.Head != "bbbb222" || len(c.Defects) != 0 {
		t.Errorf("esqueleto: %+v\n%s", c, raw)
	}
	if !strings.Contains(string(raw), "defects: []") {
		t.Errorf("defects explícito y vacío:\n%s", raw)
	}
	empty, _ := os.ReadFile(filepath.Join(h.root, ".bflow", "evals", "reviewer", "gh-300-fix-gh-170", "case.yaml"))
	var e Case
	if err := yaml.Unmarshal(empty, &e); err != nil || !e.Draft || e.Base != "" || e.Head != "" || e.Source != "GH-300" {
		t.Errorf("sin datos de X: %+v %v", e, err)
	}
	// Los esqueletos no se pueden correr: son borradores.
	if _, _, err := LoadCases(h.root, nil); err == nil {
		t.Errorf("borradores omitidos: %v", err)
	}
}

func TestFromFixesKeepsExisting(t *testing.T) {
	h := newHarness(t)
	h.write(".bflow/tasks/GH-159/state.json", `{"start_sha":"aaaa111"}`)
	const mine = "source: GH-159\nhotfix: GH-160\nbase: mio\nhead: mio\ndefects:\n  - { file: a.go, lines: \"1\", what: curado }\n"
	h.caseYAML("gh-159-fix-gh-160", mine)
	created, _, err := FromFixes(h.root, []store.Entry{startFix("GH-160", "GH-159")})
	if err != nil || len(created) != 0 {
		t.Errorf("no recrea: %v %v", created, err)
	}
	if got := h.read(".bflow/evals/reviewer/gh-159-fix-gh-160/case.yaml"); got != mine {
		t.Errorf("no sobrescribe el caso curado:\n%s", got)
	}
	// Repetir es idempotente.
	if c2, _, _ := FromFixes(h.root, []store.Entry{startFix("GH-170", "GH-159")}); len(c2) != 1 {
		t.Fatalf("uno nuevo: %v", c2)
	}
	if c3, _, _ := FromFixes(h.root, []store.Entry{startFix("GH-170", "GH-159")}); len(c3) != 0 {
		t.Errorf("segunda vez: %v", c3)
	}
}

func TestFromFixesNoEvents(t *testing.T) {
	h := newHarness(t)
	created, warnings, err := FromFixes(h.root, []store.Entry{startFix("GH-161", ""), {ID: "GH-1", Event: "approve"}})
	if err != nil {
		t.Fatal(err)
	}
	if created == nil || len(created) != 0 {
		t.Errorf("created es lista vacía, no nil (sale como []): %#v", created)
	}
	if len(warnings) == 0 {
		t.Error("sin eventos avisa")
	}
	if h.exists(".bflow/evals") {
		t.Error("sin eventos no crea nada")
	}
}
