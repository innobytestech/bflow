package release

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"innobytes.tech/bflow/internal/testutil"
)

func TestCompareAndPublished(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v0.1.0", "v0.1.0", 0}, {"v0.1.0", "v0.2.0", -1}, {"v0.10.0", "v0.9.3", 1}, {"v1.0.0", "v0.99.99", 1},
		{"v0.2.0-rc.1", "v0.2.0", -1}, {"v0.1.1-0.20260928221113-0a0c342abcde", "v0.1.1", -1},
	} {
		if got, err := Compare(c.a, c.b); err != nil || got != c.want {
			t.Errorf("Compare(%s, %s) = %d %v, want %d", c.a, c.b, got, err, c.want)
		}
	}
	if _, err := Compare("dev", "v0.1.0"); !errors.Is(err, ErrNotRelease) {
		t.Errorf("dev: %v", err)
	}
	for v, want := range map[string]bool{"v0.1.0": true, "v0.2.0-rc.1": true, "dev": false, "(devel)": false,
		"v0.0.0-20260928221113-0a0c342abcde": false, "v0.1.1-0.20260928221113-0a0c342abcde": false, "v0.0.0-20260929172119-c65f6740cdc9+dirty": false, "v0.1.0+dirty": true, "v0.1": false} {
		if Published(v) != want {
			t.Errorf("Published(%s) = %v", v, !want)
		}
	}
}

func TestNotes(t *testing.T) {
	cl := "# Cambios\n\n## Sin publicar\n\n- nada\n\n## v0.1.0 (2026-09-30)\n\n### Agregado\n\n- panel\n\n## v0.0.9\n\n- viejo\n"
	if n, err := Notes(cl, "v0.1.0"); err != nil || n != "### Agregado\n\n- panel\n" {
		t.Errorf("%q %v", n, err)
	}
	if _, err := Notes(cl, "v0.2.0"); err == nil {
		t.Error("sin sección no hay notas")
	}
	if n, err := Notes(cl, "v0.1.0-rc.1"); err != nil || n != "### Agregado\n\n- panel\n" {
		t.Errorf("la pre-release usa la sección de su versión: %q %v", n, err)
	}
	if _, err := Notes(cl, "v0.3.0-rc.1"); err == nil {
		t.Error("pre-release sin sección de su versión")
	}
}

func TestArchiveRoundTripAndChecksums(t *testing.T) {
	dir := testutil.TempDir(t)
	bin := filepath.Join(dir, "bflow")
	os.WriteFile(bin, []byte("binario"), 0o755)
	os.WriteFile(filepath.Join(dir, "LICENSE"), []byte("apache"), 0o644)
	sums := map[string]string{}
	var order []string
	for _, goos := range []string{"windows", "linux"} {
		name := AssetName("v0.1.0", goos, "amd64")
		dst := filepath.Join(dir, name)
		if err := Archive(dst, bin, goos, filepath.Join(dir, "LICENSE")); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(dst)
		got, err := Extract(b, name, goos)
		if err != nil || string(got) != "binario" {
			t.Errorf("%s: %q %v", name, got, err)
		}
		sums[name] = SHA256(b)
		order = append(order, name)
	}
	if order[0] != "bflow_0.1.0_windows_amd64.zip" || order[1] != "bflow_0.1.0_linux_amd64.tar.gz" {
		t.Errorf("nombres: %v", order)
	}
	parsed := ParseChecksums(FormatChecksums(sums, order))
	if len(parsed) != 2 || parsed[order[0]] != sums[order[0]] {
		t.Errorf("checksums: %v", parsed)
	}
}

func TestReplace(t *testing.T) {
	dir := testutil.TempDir(t)
	exe := filepath.Join(dir, BinaryName(runtime.GOOS))
	os.WriteFile(exe, []byte("viejo"), 0o755)
	if err := Replace(exe, []byte("nuevo")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "nuevo" {
		t.Errorf("%q", b)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Error("no deja el .new")
	}
	// La siguiente actualización borra el .old que queda en Windows.
	if err := Replace(exe, []byte("otro")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "otro" {
		t.Errorf("%q", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) > 2 {
		t.Errorf("sobran archivos: %v", entries)
	}
}
