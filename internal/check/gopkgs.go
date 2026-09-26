package check

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// GoPackage es un paquete de Go y si sus pruebas usan una BD compartida.
type GoPackage struct {
	ImportPath string
	DB         bool
}

// GoPackages clasifica los paquetes de root como gate.mjs: un paquete usa BD si
// importa (en sus tests) algo que termina en uno de dbImports, o si alguno de
// sus _test.go coincide con dbPattern. Clasificar de más es seguro (solo más
// lento); de menos, reintroduce los bloqueos entre tests.
func GoPackages(ctx context.Context, root, dbPattern string, dbImports []string) ([]GoPackage, error) {
	const format = "{{.ImportPath}}\t{{.Dir}}\t{{join .TestGoFiles \",\"}},{{join .XTestGoFiles \",\"}}\t{{join .TestImports \",\"}},{{join .XTestImports \",\"}}"
	cmd := exec.CommandContext(ctx, "go", "list", "-f", format, "./...")
	cmd.Dir = root
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %v\n%s", err, errb.String())
	}
	var re *regexp.Regexp
	if dbPattern != "" {
		var err error
		if re, err = regexp.Compile(dbPattern); err != nil {
			return nil, err
		}
	}
	var pkgs []GoPackage
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			continue
		}
		p := GoPackage{ImportPath: parts[0]}
		for _, imp := range strings.Split(parts[3], ",") {
			for _, suf := range dbImports {
				if imp != "" && strings.HasSuffix(imp, suf) {
					p.DB = true
				}
			}
		}
		if !p.DB && re != nil {
			for _, f := range strings.Split(parts[2], ",") {
				if f == "" {
					continue
				}
				if b, err := os.ReadFile(filepath.Join(parts[1], f)); err == nil && re.Match(b) {
					p.DB = true
					break
				}
			}
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// OSExec corre comandos del sistema: argv directo o, si argv es nil, la shell
// del sistema (cmd /C en Windows, sh -c en el resto).
func OSExec(ctx context.Context, dir string, argv []string, shell string) (string, int, error) {
	var cmd *exec.Cmd
	if argv == nil {
		cmd = shellCommand(ctx, shell)
	} else {
		if len(argv) == 0 {
			return "", 1, fmt.Errorf("comando vacío")
		}
		cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
	}
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), ee.ExitCode(), nil
	}
	if err != nil {
		return out.String(), 127, err
	}
	return out.String(), 0, nil
}

// CGOAvailable dice si -race es posible: CGO_ENABLED=1 y un compilador de C.
func CGOAvailable() bool {
	out, err := exec.Command("go", "env", "CGO_ENABLED", "CC").Output()
	if err != nil {
		return false
	}
	f := strings.Fields(string(out))
	if len(f) < 2 || f[0] != "1" {
		return false
	}
	_, err = exec.LookPath(f[1])
	return err == nil
}
