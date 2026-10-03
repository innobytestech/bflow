package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/testutil"
)

// installRels son solo prereleases, con un borrador al frente de la lista.
func installRels() []rel {
	return []rel{
		{Tag: "v0.1.0-rc.10", Prerelease: true, Draft: true},
		{Tag: "v0.1.0-rc.9", Prerelease: true},
		{Tag: "v0.1.0-rc.8", Prerelease: true},
	}
}

type installCase struct {
	name, version, wantBin, wantErr string
	badVersion                      bool
}

func installCases() []installCase {
	return []installCase{
		{name: "solo prereleases", wantBin: "bflow v0.1.0-rc.9"},
		{name: "version fijada", version: "0.1.0-rc.8", wantBin: "bflow v0.1.0-rc.8"},
		{name: "version inexistente", version: "v0.1.0-rc.7", wantErr: "no existe la versi"},
		{name: "version invalida", version: "x;rm", wantErr: "BFLOW_VERSION", badVersion: true},
	}
}

func runInstaller(t *testing.T, c installCase, name string, args []string, binName string) {
	t.Helper()
	api := ""
	if c.badVersion {
		// Una versión inválida no debe llegar a pedir nada.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("pidió %s con una versión inválida", r.URL.Path)
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)
		api = srv.URL
	} else {
		api = releaseServerOf(t, false, installRels()...).URL
	}
	dir := filepath.Join(testutil.TempDir(t), "bin")
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "BFLOW_RELEASES_URL="+api, "BFLOW_INSTALL_DIR="+dir, "BFLOW_INSTALL_NO_PATH=1", "BFLOW_VERSION="+c.version)
	out, err := cmd.CombinedOutput()
	if c.wantErr != "" {
		if err == nil || !strings.Contains(string(out), c.wantErr) {
			t.Fatalf("esperaba error %q: %v\n%s", c.wantErr, err, out)
		}
		if _, serr := os.Stat(filepath.Join(dir, binName)); serr == nil {
			t.Error("no debía instalar nada")
		}
		return
	}
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, binName)); string(b) != c.wantBin {
		t.Errorf("instaló %q, esperaba %q\n%s", b, c.wantBin, out)
	}
}

func TestInstallSh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh es para Linux y macOS")
	}
	for _, tool := range []string{"sh", "curl", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("falta %s", tool)
		}
	}
	script, _ := filepath.Abs("../../install.sh")
	for _, c := range installCases() {
		t.Run(c.name, func(t *testing.T) { runInstaller(t, c, "sh", []string{script}, "bflow") })
	}
}

func TestInstallPs1(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("install.ps1 es para Windows")
	}
	shell, err := exec.LookPath("powershell")
	if err != nil {
		if shell, err = exec.LookPath("pwsh"); err != nil {
			t.Skip("falta powershell o pwsh")
		}
	}
	script, _ := filepath.Abs("../../install.ps1")
	for _, c := range installCases() {
		t.Run(c.name, func(t *testing.T) {
			runInstaller(t, c, shell, []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script}, "bflow.exe")
		})
	}
}
