// Command dist prepara una publicación de bflow en dist/. El workflow de
// release lo corre en tres pasos para poder firmar los .exe entre compilar y
// empaquetar:
//
//	go run ./scripts/dist build v0.1.0    # dist/bin/<os>_<arch>/bflow[.exe]
//	go run ./scripts/dist package v0.1.0  # dist/bflow_0.1.0_<os>_<arch>.{zip,tar.gz} y checksums.txt
//	go run ./scripts/dist notes v0.1.0    # dist/notes.md, la sección de CHANGELOG.md
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"innobytes.tech/bflow/internal/release"
)

func main() {
	if len(os.Args) != 3 {
		fatal(fmt.Errorf("uso: go run ./scripts/dist build|package|notes vX.Y.Z"))
	}
	step, version := os.Args[1], os.Args[2]
	if !release.Published(version) {
		fatal(fmt.Errorf("versión %q: se esperaba vX.Y.Z", version))
	}
	var err error
	switch step {
	case "build":
		err = build(version)
	case "package":
		err = pack(version)
	case "notes":
		err = notes(version)
	default:
		err = fmt.Errorf("paso desconocido %q", step)
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "dist:", err)
	os.Exit(1)
}

func binPath(t release.Target) string {
	return filepath.Join("dist", "bin", t.OS+"_"+t.Arch, release.BinaryName(t.OS))
}

func build(version string) error {
	for _, t := range release.Targets {
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version="+version, "-o", binPath(t), "./cmd/bflow")
		cmd.Env = append(os.Environ(), "GOOS="+t.OS, "GOARCH="+t.Arch, "CGO_ENABLED=0")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		fmt.Println("build", t.OS+"/"+t.Arch)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s/%s: %w", t.OS, t.Arch, err)
		}
	}
	return nil
}

func pack(version string) error {
	sums := map[string]string{}
	var order []string
	for _, t := range release.Targets {
		name := release.AssetName(version, t.OS, t.Arch)
		dst := filepath.Join("dist", name)
		if err := release.Archive(dst, binPath(t), t.OS, "LICENSE", "NOTICE", "README.md", "CHANGELOG.md"); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		b, err := os.ReadFile(dst)
		if err != nil {
			return err
		}
		sums[name] = release.SHA256(b)
		order = append(order, name)
		fmt.Println("package", name)
	}
	return os.WriteFile(filepath.Join("dist", release.Checksums), []byte(release.FormatChecksums(sums, order)), 0o644)
}

func notes(version string) error {
	b, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		return err
	}
	n, err := release.Notes(string(b), version)
	if err != nil {
		return err
	}
	if err := os.MkdirAll("dist", 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("dist", "notes.md"), []byte(n), 0o644)
}
