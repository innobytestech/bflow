//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
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

// termRows es el alto de la terminal (0 si no se sabe).
func termRows(f *os.File) int {
	if f == nil {
		return 0
	}
	var ws struct{ Row, Col, X, Y uint16 }
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))); e != 0 {
		return 0
	}
	return int(ws.Row)
}
