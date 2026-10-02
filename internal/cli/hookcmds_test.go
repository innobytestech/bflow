package cli

import (
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/output"
)

func TestCompactNext(t *testing.T) {
	n := output.Next{Action: output.ActionAsk, Gate: "spec", Display: "mucho texto", Question: "¿a < b & c?",
		Options: []output.Option{{ID: "approve", Label: "Aprobar", Command: "bflow approve X"}}, Clear: true}
	got := compactNext(n)
	if got == "" || strings.Contains(got, "\n") {
		t.Fatalf("una línea no vacía: %q", got)
	}
	if strings.Contains(got, "display") || strings.Contains(got, "mucho texto") {
		t.Errorf("sin display: %s", got)
	}
	if !strings.Contains(got, `"action":"ask"`) || !strings.Contains(got, `"gate":"spec"`) || !strings.Contains(got, `"options"`) {
		t.Errorf("conserva action, gate y options: %s", got)
	}
	if !strings.Contains(got, "a < b & c") {
		t.Errorf("no escapa HTML: %s", got)
	}
	if n.Display == "" {
		t.Error("no muta el original")
	}
}
