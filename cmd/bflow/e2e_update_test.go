package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/release"
	"innobytes.tech/bflow/internal/testutil"
)

// releaseServer publica v9.9.9 con un "binario" para esta plataforma. Con
// badSum, checksums.txt no coincide con la descarga.
func releaseServer(t *testing.T, badSum bool) *httptest.Server {
	dir := testutil.TempDir(t)
	src := filepath.Join(dir, "src")
	os.WriteFile(src, []byte("bflow nuevo"), 0o755)
	name := release.AssetName("v9.9.9", runtime.GOOS, runtime.GOARCH)
	if err := release.Archive(filepath.Join(dir, name), src, runtime.GOOS); err != nil {
		t.Fatal(err)
	}
	archive, _ := os.ReadFile(filepath.Join(dir, name))
	sum := release.SHA256(archive)
	if badSum {
		sum = strings.Repeat("0", 64)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v9.9.9", "assets": []map[string]string{
				{"name": name, "browser_download_url": srv.URL + "/dl/" + name},
				{"name": release.Checksums, "browser_download_url": srv.URL + "/dl/" + release.Checksums},
			}})
		case "/dl/" + name:
			w.Write(archive)
		case "/dl/" + release.Checksums:
			fmt.Fprintf(w, "%s  %s\n", sum, name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runCopy corre una copia del binario (update la reemplaza).
func runCopy(t *testing.T, api string, args ...string) (exe string, out string, code int) {
	exe = filepath.Join(testutil.TempDir(t), filepath.Base(binary))
	b, _ := os.ReadFile(binary)
	os.WriteFile(exe, b, 0o755)
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "BFLOW_RELEASES_URL="+api)
	o, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return exe, string(o), code
}

func TestUpdate(t *testing.T) {
	srv := releaseServer(t, false)
	// Un binario compilado desde el código no se compara: pide --force.
	if _, out, code := runCopy(t, srv.URL, "update", "--check"); code != 0 || !strings.Contains(out, "no viene de una versión publicada") {
		t.Errorf("dev --check: %d %s", code, out)
	}
	exe, out, code := runCopy(t, srv.URL, "update", "--force")
	if code != 0 || !strings.Contains(out, "→ v9.9.9") {
		t.Fatalf("update --force: %d %s", code, out)
	}
	if b, _ := os.ReadFile(exe); string(b) != "bflow nuevo" {
		t.Errorf("el binario no se reemplazó: %q", b)
	}

	// Si la descarga no coincide con checksums.txt, no se toca nada.
	bad := releaseServer(t, true)
	exe, out, code = runCopy(t, bad.URL, "update", "--force")
	if code == 0 || !strings.Contains(out, "no coincide con checksums.txt") {
		t.Errorf("checksum malo: %d %s", code, out)
	}
	if b, _ := os.ReadFile(exe); string(b) == "bflow nuevo" {
		t.Error("con checksum malo no se instala")
	}
}
