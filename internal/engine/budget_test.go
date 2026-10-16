package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/store"
)

const budgetYAML = "flow:\n  budget: { implementing: 200000 }\n"

// addTokens registra un evento tokens de la tarea id en la fase p.
func addTokens(t *testing.T, v *env, id string, p flow.Phase, newTokens int64) {
	t.Helper()
	if err := v.e.Store.Append(store.Entry{TS: v.now, ID: id, Event: "tokens", Agent: "implementer",
		Data: map[string]any{"phase": string(p), "input": float64(newTokens), "output": float64(0), "cache_read": float64(0), "cache_write": float64(0)}}); err != nil {
		t.Fatal(err)
	}
}

// startImplementing arranca una tarea hotfix: entra directo a implementing.
func startImplementing(t *testing.T, v *env) string {
	t.Helper()
	ctx := context.Background()
	id := v.task(t, "corrige algo")
	mustT(t)(v.e.Start(ctx, id, flow.Hotfix, "", ""))
	return id
}

// R5, R6, R15: pasado el presupuesto, el next es una pregunta de presupuesto
// antes que cualquier spawn, sin cortar al agente en curso.
func TestNextAsksBudgetFirst(t *testing.T) {
	v := newEnv(t, budgetYAML)
	ctx := context.Background()
	id := startImplementing(t, v)
	addTokens(t, v, id, flow.Implementing, 412_000)

	view, err := v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Next.Action != output.ActionAsk || view.Next.Gate != "budget" {
		t.Fatalf("next debía preguntar por presupuesto: %+v", view.Next)
	}
	if !strings.Contains(view.Next.Display, "implementing") {
		t.Errorf("display debía nombrar la fase: %q", view.Next.Display)
	}
}

// R8: al aprobar, no se vuelve a preguntar hasta el siguiente múltiplo.
func TestBudgetAsksAgainAtNextMultiple(t *testing.T) {
	v := newEnv(t, budgetYAML)
	ctx := context.Background()
	id := startImplementing(t, v)
	addTokens(t, v, id, flow.Implementing, 412_000)

	view, err := v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Next.Gate != "budget" {
		t.Fatalf("esperaba gate budget: %+v", view.Next)
	}
	mustT(t)(v.e.Approve(ctx, id, ApproveOpts{Gate: "budget", Choice: 2}))

	view, err = v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Next.Gate == "budget" {
		t.Fatalf("tras aprobar 2x no debía preguntar otra vez sin pasar de 3x: %+v", view.Next)
	}

	addTokens(t, v, id, flow.Implementing, 250_000) // total 662k, 3.3x
	view, err = v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Next.Action != output.ActionAsk || view.Next.Gate != "budget" {
		t.Fatalf("al llegar a 3x debía preguntar otra vez: %+v", view.Next)
	}
}

// R9, R10: rechazar bloquea la tarea; al desbloquear, no se pregunta otra vez
// hasta el siguiente múltiplo.
func TestBudgetRejectUnblockNoReask(t *testing.T) {
	v := newEnv(t, budgetYAML)
	ctx := context.Background()
	id := startImplementing(t, v)
	addTokens(t, v, id, flow.Implementing, 412_000)

	mustT(t)(v.e.Reject(ctx, id, RejectOpts{Gate: "budget", Note: "a revisar con el equipo"}))

	view, err := v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Phase != flow.Blocked {
		t.Fatalf("rechazar presupuesto debía bloquear: %+v", view)
	}

	mustT(t)(v.e.Unblock(ctx, id, "", false))
	view, err = v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Next.Gate == "budget" {
		t.Errorf("tras desbloquear no debía preguntar otra vez hasta el siguiente múltiplo: %+v", view.Next)
	}
}

// R12: aprobar la pregunta de presupuesto escribe una línea en decisions.md.
func TestBudgetDecisionsLine(t *testing.T) {
	v := newEnv(t, budgetYAML)
	ctx := context.Background()
	id := startImplementing(t, v)
	addTokens(t, v, id, flow.Implementing, 412_000)

	mustT(t)(v.e.Approve(ctx, id, ApproveOpts{Gate: "budget", Choice: 2}))

	b, err := v.e.Store.ReadFile(id, "decisions.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "presupuesto") || !strings.Contains(string(b), "implementing") {
		t.Errorf("decisions.md no registró la decisión de presupuesto: %s", b)
	}
}

// R13: approve/reject de presupuesto llevan gate "budget" en log.jsonl, aunque
// no haya otro gate pendiente.
func TestBudgetEntryGate(t *testing.T) {
	v := newEnv(t, budgetYAML)
	ctx := context.Background()
	id := startImplementing(t, v)
	addTokens(t, v, id, flow.Implementing, 412_000)

	mustT(t)(v.e.Approve(ctx, id, ApproveOpts{Gate: "budget", Choice: 2}))

	log, err := v.e.Store.Log(id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range log {
		if e.Event == "approve" && e.Gate == "budget" {
			found = true
		}
	}
	if !found {
		t.Errorf("log.jsonl no trae el approve con gate budget: %+v", log)
	}
}

// R3: sin flow.budget no se lee el log para calcular next (un log.jsonl roto
// no debe tumbar bflow status).
func TestNoBudgetNoLogRead(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := startImplementing(t, v)
	if err := os.WriteFile(filepath.Join(v.e.Store.Dir(), "log.jsonl"), []byte("{esto no es json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	view, err := v.e.Status(ctx, id)
	if err != nil {
		t.Fatalf("sin presupuesto, un log.jsonl roto no debía afectar status: %v", err)
	}
	if view.Next.Gate == "budget" {
		t.Errorf("sin flow.budget no debía preguntar: %+v", view.Next)
	}
}
