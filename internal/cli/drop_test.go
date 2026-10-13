package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/tracker/trackertest"
)

// R10: drop sin ID falla con usage y no toca nada.
func TestDropRequiresID(t *testing.T) {
	code, out, _ := run(t, "drop", "--note", "motivo", "--json")
	if code != output.ExitError || !strings.Contains(out, `"usage"`) {
		t.Errorf("exit=%d out=%s", code, out)
	}
	code, out, _ = run(t, "drop", "--json")
	if code != output.ExitError || !strings.Contains(out, `"usage"`) {
		t.Errorf("sin ID ni nota: exit=%d out=%s", code, out)
	}
}

// R4: el statusline no muestra duración de una tarea retirada.
func TestStatuslineDroppedHasNoDuration(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	sc := engine.StatusCache{ID: "GH-1", Phase: flow.Dropped, Since: now.Add(-3 * time.Hour)}
	if got := StatuslineText(sc, now); got != "GH-1 · dropped" {
		t.Errorf("statusline: %q", got)
	}
	sc.Phase = flow.Implementing
	if got := StatuslineText(sc, now); !strings.Contains(got, "3h") {
		t.Errorf("las demás fases sí muestran duración: %q", got)
	}
}

// GH-55: unblock --no-changelog exige --note.
func TestUnblockNoChangelogNeedsNote(t *testing.T) {
	ctx := context.Background()
	tr := trackertest.NewMemory()
	env := ghEnv(t, "", tr)
	os.Remove(filepath.Join(env.Dir, "bflow.yaml"))
	var out bytes.Buffer
	env.Stdout = &out
	e, err := env.Build(env.Dir)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tr.Create(ctx, "Demo", "d")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Start(ctx, task.ID, flow.Hotfix, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Block(ctx, task.ID, "x"); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"unblock", task.ID, "--no-changelog", "--json"}, env); code == output.ExitOK || !strings.Contains(out.String(), "note_required") {
		t.Errorf("sin nota: exit=%d out=%s", code, out.String())
	}
	out.Reset()
	if code := Run([]string{"unblock", task.ID, "--no-changelog", "--note", "interno", "--json"}, env); code != output.ExitOK {
		t.Errorf("con nota: exit=%d out=%s", code, out.String())
	}
}
