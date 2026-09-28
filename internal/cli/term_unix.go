//go:build !windows

package cli

import (
	"os"
	"os/exec"
)

// disableEcho apaga el eco de la terminal con stty y devuelve cómo restaurarlo.
func disableEcho(f *os.File) func() {
	off := exec.Command("stty", "-echo")
	off.Stdin = f
	if off.Run() != nil {
		return func() {}
	}
	return func() {
		on := exec.Command("stty", "echo")
		on.Stdin = f
		_ = on.Run()
	}
}

// enableVT: las terminales Unix interpretan las secuencias de color.
func enableVT(f *os.File) bool { return f != nil }
