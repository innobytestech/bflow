package engine

import (
	"context"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

func TestStartRecordsModels(t *testing.T) {
	v := newEnv(t, "agents:\n  reviewer: { model: sonnet }\n")
	on := true
	v.e.Cfg.Flow.Scout = &on
	id := v.task(t, "con modelos")
	if _, err := v.e.Start(context.Background(), id, flow.Light, "", ""); err != nil {
		t.Fatal(err)
	}
	log, err := v.e.Store.Log(id)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	for _, en := range log {
		if en.Event == "start" {
			data = en.Data
		}
	}
	models, _ := data["models"].(map[string]any)
	want := map[string]string{"spec-author": "opus", "reviewer": "sonnet", "implementer": "sonnet", "documenter": "haiku", "scout": "haiku"}
	if len(models) != len(want) {
		t.Fatalf("data.models del start = %v, quiero %v", data["models"], want)
	}
	for a, m := range want {
		if models[a] != m {
			t.Errorf("modelo de %s = %v, quiero %q (todo: %v)", a, models[a], m, models)
		}
	}
}
