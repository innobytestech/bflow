package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/check"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

type fakeContract struct {
	res    *check.Result
	fresh  bool
	detail string
}

func (f *fakeContract) Last(context.Context, string) (*check.Result, bool, string, error) {
	return f.res, f.fresh, f.detail, nil
}

const (
	t1File = "internal/suma_test.go"
	t1Src  = "package internal\n\nimport \"testing\"\n\nfunc TestSuma(t *testing.T) {\n\tif Suma(1, 2) != 3 {\n\t\tt.Fatal(\"suma\")\n\t}\n}\n\nfunc TestResta(t *testing.T) {\n\tif Resta(3, 1) != 2 {\n\t\tt.Fatal(\"resta\")\n\t}\n}\n"
)

const contractYAML = "stack: go\ncheck:\n  steps:\n    - { name: vet, run: \"go vet ./...\" }\n    - { name: test, run: \"go test ./...\" }\n    - { name: lint, run: \"lint\" }\n"

// contractEnv deja una tarea full en contract con una prueba del contrato en la rama.
func contractEnv(t *testing.T, yaml string, fc *fakeContract) (*env, string) {
	t.Helper()
	v := newEnv(t, yaml)
	v.e.Git = &fakeGit{branch: "feature/T-1", diff: []string{t1File}}
	v.e.Contract = fc
	put(t, v, t1File, []byte(t1Src))
	id := v.task(t, "Demo")
	if _, err := v.e.Store.Update(id, func(r *store.Record, _ bool) error {
		r.Flow = flow.State{ID: id, Lane: flow.Full, Phase: flow.Contract, BranchCreated: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return v, id
}

func contractReady(v *env, id string) (Outcome, error) {
	return v.e.Report(context.Background(), id, ReportOpts{Agent: "implementer", Verdict: flow.ContractReady})
}

func steps(vet, test, lint string, fails ...string) []check.StepResult {
	return []check.StepResult{
		{Name: "vet", Status: vet},
		{Name: "test", Status: test, Fails: fails},
		{Name: "lint", Status: lint},
	}
}

func TestContractStale(t *testing.T) {
	good := &check.Result{SHA: "abc", Result: "FAIL", Steps: steps("pass", "fail", "skip", "--- FAIL: TestSuma")}
	cases := map[string]*fakeContract{
		"sin check":     {res: nil, fresh: false, detail: "no hay check registrado"},
		"DIRTY":         {res: &check.Result{SHA: "DIRTY", Steps: good.Steps}, fresh: false, detail: "con código sin commitear"},
		"cambios luego": {res: good, fresh: false, detail: "cambios de código posteriores al check: internal/x.go"},
	}
	for name, fc := range cases {
		v, id := contractEnv(t, contractYAML, fc)
		_, err := contractReady(v, id)
		rj := rejectionOf(t, err)
		if rj.Code != "contract_stale" || !strings.Contains(rj.Reason, "bflow check "+id) {
			t.Errorf("%s: %s %q", name, rj.Code, rj.Reason)
		}
		if phaseOf(t, v, id) != flow.Contract {
			t.Errorf("%s: la fase no debe cambiar", name)
		}
	}
}

func TestContractBroken(t *testing.T) {
	var fails []string
	for i := 1; i <= 12; i++ {
		fails = append(fails, fmt.Sprintf("vet: error %d", i))
	}
	res := &check.Result{SHA: "abc", Result: "FAIL", Steps: []check.StepResult{
		{Name: "vet", Status: "fail", Fails: fails},
		{Name: "test", Status: "fail", Fails: []string{"--- FAIL: TestSuma"}},
	}}
	v, id := contractEnv(t, contractYAML, &fakeContract{res: res, fresh: true})
	_, err := contractReady(v, id)
	rj := rejectionOf(t, err)
	if rj.Code != "contract_broken" || !strings.Contains(rj.Reason, "vet") {
		t.Fatalf("%s %q", rj.Code, rj.Reason)
	}
	if !strings.Contains(rj.Reason, "vet: error 10") || strings.Contains(rj.Reason, "vet: error 11") {
		t.Errorf("deben ir hasta 10 líneas: %q", rj.Reason)
	}
}

func TestContractGreen(t *testing.T) {
	res := &check.Result{SHA: "abc", Result: "PASS", Steps: steps("pass", "pass", "pass")}
	v, id := contractEnv(t, contractYAML, &fakeContract{res: res, fresh: true})
	_, err := contractReady(v, id)
	rj := rejectionOf(t, err)
	if rj.Code != "contract_green" || !strings.Contains(rj.Reason, "fallar") {
		t.Fatalf("%s %q", rj.Code, rj.Reason)
	}
}

// El test_step falló, pero por algo que no es una prueba del contrato (otro paquete roto).
func TestContractGreenNoT1Names(t *testing.T) {
	res := &check.Result{SHA: "abc", Result: "FAIL", Steps: steps("pass", "fail", "skip", "--- FAIL: TestOtraCosa", "FAIL otro/paquete")}
	v, id := contractEnv(t, contractYAML, &fakeContract{res: res, fresh: true})
	_, err := contractReady(v, id)
	if rj := rejectionOf(t, err); rj.Code != "contract_green" {
		t.Fatalf("%s %q", rj.Code, rj.Reason)
	}

	// Sin Fails se busca en las líneas del Tail.
	res = &check.Result{SHA: "abc", Result: "FAIL", Steps: []check.StepResult{
		{Name: "vet", Status: "pass"},
		{Name: "test", Status: "fail", Tail: "ok otro\n--- FAIL: TestResta (0.00s)\n"},
	}}
	v, id = contractEnv(t, contractYAML, &fakeContract{res: res, fresh: true})
	if _, err := contractReady(v, id); err != nil {
		t.Errorf("el Tail nombra TestResta, debe aceptarse: %v", err)
	}
}

func TestContractTestNames(t *testing.T) {
	v := newEnv(t, "")
	put(t, v, t1File, []byte(t1Src))
	put(t, v, "web/a.spec.ts", []byte("it('x', () => {})"))
	got := contractTestNames(v.e.Cfg.Root, []string{t1File, "web/a.spec.ts", "falta_test.go"})
	for _, w := range []string{"suma_test.go", "TestSuma", "TestResta", "a.spec.ts", "falta_test.go"} {
		if !slices.Contains(got, w) {
			t.Errorf("falta %q en %v", w, got)
		}
	}
	if slices.Contains(got, "internal/suma_test.go") {
		t.Errorf("solo nombres base y funciones Test*: %v", got)
	}
}

func TestContractReadyFreezesAndAdvances(t *testing.T) {
	// vet pasa, test falla nombrando una prueba del contrato; lint (posterior) se ignora aunque falle.
	res := &check.Result{SHA: "abc", Result: "FAIL", Steps: steps("pass", "fail", "fail", "--- FAIL: TestSuma (0.00s)")}
	v, id := contractEnv(t, contractYAML, &fakeContract{res: res, fresh: true})
	o, err := contractReady(v, id)
	if err != nil {
		t.Fatalf("contrato válido: %v", err)
	}
	if o.To != flow.Implementing || o.Gate != "" || o.Next.Agents[0].Args["resume"] != "true" {
		t.Fatalf("debe avanzar sin gate y con resume: %+v next=%+v", o, o.Next)
	}
	if !slices.Contains(v.e.Frozen(id), t1File) {
		t.Errorf("pruebas sin congelar: %v", v.e.Frozen(id))
	}
	if !hasWarning(o, "1 prueba(s) congelada(s)") {
		t.Errorf("aviso de congelado: %v", o.Warnings)
	}
}

func TestContractReadyPreStepSkipCounts(t *testing.T) {
	res := &check.Result{SHA: "abc", Result: "FAIL", Steps: steps("skip", "fail", "pass", "TestResta")}
	v, id := contractEnv(t, contractYAML, &fakeContract{res: res, fresh: true})
	if o, err := contractReady(v, id); err != nil || o.To != flow.Implementing {
		t.Fatalf("un paso previo en skip cuenta como bien: %v %+v", err, o)
	}
}

// Sin forma de validar, la gate contract se abre como antes y al aprobarla se congela.
func TestContractUnverifiableOpensGate(t *testing.T) {
	good := &check.Result{SHA: "abc", Result: "FAIL", Steps: steps("pass", "fail", "pass", "TestSuma")}
	cases := map[string]string{
		"test_step ausente": contractYAML + "  test_step: integracion\n",
		"sin steps":         "stack: go\n",
	}
	for name, yaml := range cases {
		v, id := contractEnv(t, yaml, &fakeContract{res: good, fresh: true})
		if name == "sin steps" {
			v.e.Cfg.Check.Steps = nil
		}
		o, err := contractReady(v, id)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if o.Phase != flow.Contract || o.Gate != string(flow.GateContract) {
			t.Fatalf("%s: debe abrir la gate contract: %+v", name, o)
		}
		if len(v.e.Frozen(id)) != 0 {
			t.Errorf("%s: no se congela antes de aprobar", name)
		}
		o = mustT(t)(v.e.Approve(context.Background(), id, ApproveOpts{Gate: "contract"}))
		if o.To != flow.Implementing || !slices.Contains(v.e.Frozen(id), t1File) {
			t.Errorf("%s: aprobar congela: %+v %v", name, o, v.e.Frozen(id))
		}
	}
}
