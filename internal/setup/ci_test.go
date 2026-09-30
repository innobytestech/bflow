package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Como en BE-05: CI corre la integración con -tags y el check no.
const workflow = `name: ci
on: [pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: go mod download
      - name: pruebas
        run: |
          go vet ./...
          go test -race -tags integration ./... && govulncheck ./...
  lint:
    runs-on: ubuntu-latest
    steps:
      - run: golangci-lint run # sin tags
`

func TestCIGaps(t *testing.T) {
	dir := t.TempDir()
	wf := filepath.Join(dir, ".github", "workflows")
	os.MkdirAll(wf, 0o755)
	os.WriteFile(filepath.Join(wf, "ci.yml"), []byte(workflow), 0o644)

	steps := []string{"go vet ./...", "go test -count=1 {race} ./...", "govulncheck ./..."}
	n, gaps := CIGaps(dir, steps)
	want := []string{
		"CI corre `golangci-lint run` y ningún paso del check lo hace",
		"CI corre `go test -race -tags integration ./...` y el check no usa -tags integration",
	}
	if n != 4 || !reflect.DeepEqual(gaps, want) {
		t.Errorf("n=%d gaps=%q", n, gaps)
	}

	steps[1] = "go test -count=1 -tags=integration,e2e ./..."
	steps = append(steps, "golangci-lint run --new-from-rev={base}")
	if _, gaps := CIGaps(dir, steps); len(gaps) != 0 {
		t.Errorf("con tags e integración cubiertas no hay huecos: %q", gaps)
	}
	if n, gaps := CIGaps(t.TempDir(), steps); n != 0 || gaps != nil {
		t.Errorf("sin workflows no hay nada que comparar: %d %q", n, gaps)
	}
}
