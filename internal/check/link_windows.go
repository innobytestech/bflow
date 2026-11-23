package check

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// linkDir crea en link un junction a la carpeta target: no pide permisos de
// administrador ni el modo de desarrollador, a diferencia de un enlace simbólico.
func linkDir(target, link string) error {
	if strings.ContainsAny(target+link, `"%^!&`) {
		return errors.New("la ruta lleva caracteres que cmd.exe interpreta")
	}
	cmd := exec.Command(cmdExe())
	// Con /S, cmd quita solo las comillas externas y deja las de cada ruta.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `/S /C "mklink /J "` + link + `" "` + target + `""`}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mklink /J: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cmdExe es el cmd.exe del sistema, no el primero que aparezca en el PATH.
func cmdExe() string {
	if c := os.Getenv("ComSpec"); c != "" && filepath.IsAbs(c) {
		return c
	}
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
}

// runAlive dice si la corrida de dir sigue viva: mientras vive tiene abierto
// su archivo pid, y Windows no deja borrar un archivo abierto. Así no hace falta
// abrir otro proceso (OpenProcess), que los antivirus toman por sospechoso.
func runAlive(dir string) bool {
	err := os.Remove(filepath.Join(dir, "pid"))
	return err != nil && !errors.Is(err, os.ErrNotExist)
}
