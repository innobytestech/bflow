package guard

import "testing"

// R17: la N-ésima repetición idéntica (misma huella) en la corrida se niega.
func TestLoopObserveDeniesAtN(t *testing.T) {
	s := &LoopState{}
	a := Action{Tool: Bash, Fingerprint: "abc"}
	for i := 1; i < 3; i++ {
		n, deny := s.Observe(a, 3)
		if n != i || deny {
			t.Fatalf("repetición %d: n=%d deny=%v, want n=%d deny=false", i, n, deny, i)
		}
	}
	n, deny := s.Observe(a, 3)
	if n != 3 || !deny {
		t.Fatalf("3ra repetición debía negarse: n=%d deny=%v", n, deny)
	}
}

// R19: un Edit o Write permitido reinicia la cuenta de Bash y Read de la
// corrida (volver a correr las pruebas tras un arreglo no es un ciclo).
func TestLoopEditResetsBash(t *testing.T) {
	s := &LoopState{}
	bash := Action{Tool: Bash, Fingerprint: "go test ./..."}
	s.Observe(bash, 3)
	s.Observe(bash, 3)

	edit := Action{Tool: Edit, Fingerprint: "edit:a.go:1"}
	if n, deny := s.Observe(edit, 3); deny || n != 1 {
		t.Fatalf("el edit no debía negarse: n=%d deny=%v", n, deny)
	}

	if n, deny := s.Observe(bash, 3); deny || n != 1 {
		t.Fatalf("tras el edit, el bash debía reiniciar su cuenta: n=%d deny=%v", n, deny)
	}
}
