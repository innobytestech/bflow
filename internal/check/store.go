package check

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"innobytes.tech/bflow/internal/store"
)

// Save guarda el resultado en .bflow/tasks/<ID>/ (o en .bflow/ sin tarea).
func Save(st *store.Store, id string, res Result) error {
	js, err := json.Marshal(res)
	if err != nil {
		return err
	}
	if id == "" {
		if err := os.MkdirAll(st.Dir(), 0o755); err != nil {
			return err
		}
		if err := store.WriteAtomic(filepath.Join(st.Dir(), "check.json"), js); err != nil {
			return err
		}
		return store.WriteAtomic(filepath.Join(st.Dir(), "check.md"), []byte(res.Markdown()))
	}
	if err := st.WriteFile(id, "check.json", js); err != nil {
		return err
	}
	return st.WriteFile(id, "check.md", []byte(res.Markdown()))
}

// Load lee el último resultado (nil si no hay).
func Load(st *store.Store, id string) (*Result, error) {
	var b []byte
	var err error
	if id == "" {
		b, err = os.ReadFile(filepath.Join(st.Dir(), "check.json"))
	} else {
		b, err = st.ReadFile(id, "check.json")
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Result
	return &r, json.Unmarshal(b, &r)
}

// Verifier implementa engine.Verifier: DONE solo con un check vigente.
type Verifier struct {
	Store     *store.Store
	Git       GitReader
	CodePaths []string
}

func (v Verifier) Verify(ctx context.Context, id string) (bool, string, error) {
	res, err := Load(v.Store, id)
	if err != nil {
		return false, "", err
	}
	ok, detail := Verify(ctx, v.Git, v.CodePaths, res)
	return ok, detail, nil
}
