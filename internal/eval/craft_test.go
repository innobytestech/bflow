package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func (h *harness) noCache() {
	h.t.Helper()
	if h.exists(".bflow/cache/evals") || len(h.g.adds) != 0 {
		h.t.Error("el rechazo no debe crear corrida ni worktrees")
	}
}

func TestCraftFromTree(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	st := h.next(Opts{})
	craft := st.Spawn.Args["craft"]
	if !filepath.IsAbs(craft) {
		t.Errorf("craft debe ser ruta absoluta: %q", craft)
	}
	if b, err := os.ReadFile(craft); err != nil || string(b) != treeCraft {
		t.Errorf("craft.md es el oficio del árbol: %q %v", b, err)
	}
	if want := filepath.Join(h.root, ".bflow", "cache", "evals", runName, "craft.md"); filepath.Clean(craft) != want {
		t.Errorf("craft en %q, quería %q", craft, want)
	}
}

func TestCraftFromRef(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.g.files["v1:"+CraftPath] = "# oficio viejo\n"
	st := h.next(Opts{Craft: "v1"})
	if b, _ := os.ReadFile(st.Spawn.Args["craft"]); string(b) != "# oficio viejo\n" {
		t.Errorf("craft.md es el del ref: %q", b)
	}
	// Cierra la corrida y revisa que el resultado guarde ref y hash del contenido.
	h.answer(st.Spawn, "internal/a.go:11 falta\n")
	if _, err := h.r.Report(context.Background(), "alfa", "REJECTED", ""); err != nil {
		t.Fatal(err)
	}
	done := h.next(Opts{})
	sum := sha256.Sum256([]byte("# oficio viejo\n"))
	if done.Result == nil || done.Result.Craft.Ref != "v1" || done.Result.Craft.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("craft del resultado: %+v", done.Result)
	}
}

func TestCraftUnknownRef(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	_, err := h.r.Next(context.Background(), Opts{Craft: "no-existe"})
	rejected(t, err, "craft_ref_unknown")
	h.noCache()
}

func TestCraftMissingAtRef(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	_, err := h.r.Next(context.Background(), Opts{Craft: "v1"}) // resuelve, pero no trae el oficio
	rejected(t, err, "craft_missing")
	h.noCache()
}

func TestCraftRefWithDash(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.g.files["--upload-pack=x:"+CraftPath] = "oficio"
	// El fake resuelve y trae este ref: el rechazo es de la regla, no de git.
	_, err := h.r.Next(context.Background(), Opts{Craft: "--upload-pack=x"})
	rejected(t, err, "craft_ref_unknown")
	h.noCache()
}
