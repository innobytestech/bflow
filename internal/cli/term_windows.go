package cli

import (
	"os"
	"syscall"
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode = kernel32.NewProc("SetConsoleMode")
)

const enableEchoInput = 0x0004

// disableEcho apaga el eco de la consola y devuelve cómo restaurarlo.
func disableEcho(f *os.File) func() {
	h := syscall.Handle(f.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(h, &mode); err != nil {
		return func() {}
	}
	setConsoleMode.Call(uintptr(h), uintptr(mode&^enableEchoInput))
	return func() { setConsoleMode.Call(uintptr(h), uintptr(mode)) }
}
