package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/store"
)

// FrozenFile guarda las pruebas congeladas de una tarea: ruta → hash del
// contenido al congelar. Las versiones anteriores guardaban solo la lista de
// rutas; esas se siguen leyendo, sin hash que comparar.
const FrozenFile = "frozen-tests.json"

// absent es el hash de una prueba que no existía al congelar.
const absent = "absent"

// freezeTests congela las pruebas que la tarea escribió o tocó hasta aprobar
// el contrato: desde ahí el implementer hace pasar esas pruebas sin cambiarlas.
func (e *Engine) freezeTests(ctx context.Context, id string) (int, error) {
	if e.Git == nil {
		return 0, nil
	}
	frozen := e.taskTests(ctx)
	return len(frozen), e.writeFrozen(id, frozen)
}

// taskTests son las pruebas que la rama agregó o cambió, más las que están
// sin commitear.
func (e *Engine) taskTests(ctx context.Context) []string {
	if e.Git == nil {
		return nil
	}
	base := e.Cfg.VCS.BaseBranch
	if base != "" && e.Cfg.VCS.Remote != "" {
		base = e.Cfg.VCS.Remote + "/" + base
	}
	var files []string
	if base != "" {
		if d, err := e.Git.DiffNames(ctx, base); err == nil {
			files = append(files, d...)
		}
	}
	if d, err := e.Git.Dirty(ctx, nil); err == nil {
		files = append(files, d...)
	}
	var frozen []string
	for _, f := range files {
		f = strings.ReplaceAll(f, `\`, "/")
		if guard.MatchesTest(e.Cfg.Guard.TestPatterns, f) && !slices.Contains(frozen, f) {
			frozen = append(frozen, f)
		}
	}
	return frozen
}

// Refreeze vuelve a tomar el hash de las pruebas congeladas tal como están
// ahora. Es para una persona que cambió una prueba a propósito; guard impide
// que lo corra un agente.
func (e *Engine) Refreeze(id string) (int, error) {
	if _, err := e.Store.Load(id); err != nil {
		return 0, err
	}
	files := e.Frozen(id)
	if len(files) == 0 {
		return 0, errors.New(id + " no tiene pruebas congeladas")
	}
	if err := e.writeFrozen(id, files); err != nil {
		return 0, err
	}
	_ = e.Store.Append(store.Entry{TS: e.now(), ID: id, Event: "refreeze", By: e.User, Data: map[string]any{"files": len(files)}})
	return len(files), nil
}

func (e *Engine) writeFrozen(id string, files []string) error {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f] = e.testHash(f)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return e.Store.WriteFile(id, FrozenFile, b)
}

func (e *Engine) readFrozen(id string) map[string]string {
	b, err := e.Store.ReadFile(id, FrozenFile)
	if err != nil {
		return nil
	}
	m := map[string]string{}
	if json.Unmarshal(b, &m) == nil {
		return m
	}
	var old []string
	if json.Unmarshal(b, &old) != nil {
		return nil
	}
	for _, f := range old {
		m[f] = ""
	}
	return m
}

// Frozen devuelve las pruebas congeladas de una tarea.
func (e *Engine) Frozen(id string) []string {
	m := e.readFrozen(id)
	out := make([]string, 0, len(m))
	for f := range m {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

// FrozenChanged devuelve las pruebas congeladas cuyo contenido ya no es el
// del momento en que se congelaron. No depende de hooks: lo revisa el CLI al
// reportar DONE y en check --verify, sea cual sea la herramienta del agente.
func (e *Engine) FrozenChanged(id string) []string {
	var changed []string
	for f, h := range e.readFrozen(id) {
		if h != "" && e.testHash(f) != h {
			changed = append(changed, f)
		}
	}
	slices.Sort(changed)
	return changed
}

func frozenRejection(changed []string) *flow.Rejection {
	return &flow.Rejection{Code: "frozen_changed", Reason: "cambiaron pruebas congeladas al aprobar el contrato: " + strings.Join(changed, ", ") +
		". Déjalas como estaban; si una prueba está mal, reporta NEEDS_DECISION. Si una persona la cambió a propósito, que corra bflow freeze desde su terminal."}
}

// testHash normaliza los finales de línea para que un checkout con autocrlf
// no cuente como cambio.
func (e *Engine) testHash(rel string) string {
	b, err := os.ReadFile(filepath.Join(e.Cfg.Root, filepath.FromSlash(rel)))
	if err != nil {
		return absent
	}
	sum := sha256.Sum256(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
	return hex.EncodeToString(sum[:])
}
