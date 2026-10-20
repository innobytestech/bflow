package engine

import (
	"os"
	"path/filepath"
	"testing"

	"innobytes.tech/bflow/internal/check"
	"innobytes.tech/bflow/internal/flow"
)

// Si no se pueden congelar las pruebas, el contrato no avanza sin ellas: queda en la gate contract.
func TestContractFreezeFailureOpensGate(t *testing.T) {
	res := &check.Result{SHA: "abc", Result: "FAIL", Steps: steps("pass", "fail", "skip", "--- FAIL: TestSuma (0.00s)")}
	v, id := contractEnv(t, contractYAML, &fakeContract{res: res, fresh: true})
	p, err := v.e.Store.Path(id, FrozenFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p, "x"), 0o755); err != nil { // un directorio donde va el archivo
		t.Fatal(err)
	}
	o, err := contractReady(v, id)
	if err != nil {
		t.Fatal(err)
	}
	if o.Phase != flow.Contract || o.Gate != string(flow.GateContract) || !hasWarning(o, "no se pudieron congelar") {
		t.Fatalf("debe abrir la gate contract con aviso: %+v %v", o, o.Warnings)
	}
}
