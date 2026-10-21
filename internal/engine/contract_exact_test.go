package engine

import (
	"os"
	"testing"

	"innobytes.tech/bflow/internal/check"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

func TestContractRedMatch(t *testing.T) {
	cases := map[string]struct {
		t1   contractT1
		line string
		want bool
	}{
		"fallo con tiempo":       {contractT1{Funcs: []string{"TestSuma"}}, "--- FAIL: TestSuma (0.00s)", true},
		"subprueba":              {contractT1{Funcs: []string{"TestSuma"}}, "--- FAIL: TestSuma/x (0.00s)", true},
		"sangría":                {contractT1{Funcs: []string{"TestSuma"}}, "    --- FAIL: TestSuma", true},
		"TestSum no es TestSuma": {contractT1{Funcs: []string{"TestSum"}}, "--- FAIL: TestSuma (0.00s)", false},
		"error de compilación":   {contractT1{Funcs: []string{"TestSuma"}}, "suma_test.go:5:2: undefined: Suma", false},
		"nombre sin FAIL":        {contractT1{Funcs: []string{"TestSuma"}}, "=== RUN   TestSuma", false},
		"ruta exacta":            {contractT1{Paths: []string{"web/a.spec.ts"}}, "FAIL web/a.spec.ts > suma", true},
		"ruta absoluta":          {contractT1{Paths: []string{"web/a.spec.ts"}}, "FAIL /repo/web/a.spec.ts:3", true},
		"ruta con barra inversa": {contractT1{Paths: []string{"web/a.spec.ts"}}, `FAIL C:\repo\web\a.spec.ts:3`, true},
		"ruta ajena":             {contractT1{Paths: []string{"web/a.spec.ts"}}, "FAIL web/ba.spec.ts", false},
		"nombre base solo":       {contractT1{Paths: []string{"web/a.spec.ts"}}, "FAIL a.spec.ts", false},
		"segunda aparición":      {contractT1{Paths: []string{"a_test.py"}}, "x/ba_test.py y luego a_test.py:1", true},
	}
	for name, c := range cases {
		if got := c.t1.failed(c.line); got != c.want {
			t.Errorf("%s: failed(%q) = %v, want %v", name, c.line, got, c.want)
		}
	}
	// a_test.go no coincide con ba_test.go: en .go solo cuentan las líneas --- FAIL.
	if (contractT1{Funcs: []string{"TestA"}}).failed("ba_test.go:3: boom") {
		t.Error("a_test.go no debe coincidir con ba_test.go")
	}
	if !buildFailed([]string{"ok x", "FAIL innobytes/internal [build failed]"}) || !buildFailed([]string{"FAIL p [setup failed]"}) || buildFailed([]string{"--- FAIL: TestSuma"}) {
		t.Error("buildFailed debe reconocer [build failed] y [setup failed]")
	}
}

// Un error de compilación que nombra el archivo T1 no es un T1 en rojo.
func TestContractBuildFailedOpensGate(t *testing.T) {
	res := &check.Result{SHA: "abc", Result: "FAIL", Steps: steps("pass", "fail", "skip",
		"internal/suma_test.go:5:2: undefined: Suma", "FAIL innobytes.tech/bflow/internal [build failed]")}
	v, id := contractEnv(t, contractYAML, &fakeContract{res: res, fresh: true})
	o, err := contractReady(v, id)
	if err != nil {
		t.Fatalf("sin rechazo: %v", err)
	}
	if o.Phase != flow.Contract || o.Gate != string(flow.GateContract) {
		t.Fatalf("debe abrir la gate contract: %+v", o)
	}
	if len(v.e.Frozen(id)) != 0 {
		t.Errorf("no se congela: %v", v.e.Frozen(id))
	}
}

// Si flow.Apply rechaza el evento no se escribe frozen-tests.json.
func TestContractNoFreezeWhenApplyFails(t *testing.T) {
	res := &check.Result{SHA: "abc", Result: "FAIL", Steps: steps("pass", "fail", "skip", "--- FAIL: TestSuma (0.00s)")}
	v, id := contractEnv(t, contractYAML, &fakeContract{res: res, fresh: true})
	if _, err := v.e.Store.Update(id, func(r *store.Record, _ bool) error {
		r.Flow.Gate = &flow.PendingGate{Name: flow.GateContract}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := contractReady(v, id)
	if rj := rejectionOf(t, err); rj.Code != "gate_pending" {
		t.Fatalf("%s %q", rj.Code, rj.Reason)
	}
	p, perr := v.e.Store.Path(id, FrozenFile)
	if perr != nil {
		t.Fatal(perr)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("frozen-tests.json no debe existir: %v", err)
	}
}

func TestRestoreFrozenPrevState(t *testing.T) {
	v, id := contractEnv(t, contractYAML, &fakeContract{})
	p, err := v.e.Store.Path(id, FrozenFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.e.saveFrozen(id, map[string]string{"nuevo_test.go": "h"}); err != nil {
		t.Fatal(err)
	}
	v.e.restoreFrozen(id, nil)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("sin estado previo se borra: %v", err)
	}
	prev := map[string]string{"a_test.go": "h1"}
	if err := v.e.saveFrozen(id, map[string]string{"b_test.go": "h2"}); err != nil {
		t.Fatal(err)
	}
	v.e.restoreFrozen(id, prev)
	got := v.e.readFrozen(id)
	if len(got) != 1 || got["a_test.go"] != "h1" {
		t.Errorf("debe volver al estado previo: %v", got)
	}
}

// Con repo que puede validar el contrato no se anuncia gate; sin poder, sí.
func TestFlowCfgContractGate(t *testing.T) {
	v, _ := contractEnv(t, contractYAML, &fakeContract{})
	if v.e.flowCfg().ContractGate {
		t.Error("puede validar: sin gate")
	}
	v.e.Contract = nil
	if !v.e.flowCfg().ContractGate {
		t.Error("sin Contract: gate contract")
	}
}
